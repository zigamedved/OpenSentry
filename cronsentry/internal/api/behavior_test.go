package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/zigamedved/OpenSentry/internal/db"
	"github.com/zigamedved/OpenSentry/internal/models"
)

// These tests pin the job API as it behaves today. They talk to Postgres and
// fail when a status code, JSON field, or error string changes.

func TestMain(m *testing.M) {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	root := wd
	for {
		if _, statErr := os.Stat(filepath.Join(root, "go.mod")); statErr == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			panic("go.mod not found from " + wd)
		}
		root = parent
	}
	if err := os.Chdir(root); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func TestJobLifecycleMatchesCurrentBehavior(t *testing.T) {
	srv := newBehaviorServer(t)

	empty := do(t, srv, http.MethodGet, "/api/jobs", "")
	if empty.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d", empty.StatusCode)
	}
	if strings.TrimSpace(empty.Body) != "[]" {
		t.Fatalf("empty list = %q, want []", empty.Body)
	}
	if empty.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("cors = %q", empty.Header.Get("Access-Control-Allow-Origin"))
	}

	created := do(t, srv, http.MethodPost, "/api/jobs", `{
		"name": "Nightly backup",
		"description": "Daily database backup",
		"schedule": "0 0 * * *",
		"grace_time": 15
	}`)
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d body %s", created.StatusCode, created.Body)
	}
	job := decodeJob(t, created.Body)
	if job.ID == "" || job.Name != "Nightly backup" || job.Description != "Daily database backup" {
		t.Fatalf("created job = %+v", job)
	}
	if job.Schedule != "0 0 * * *" || job.GraceTime != 15 || job.Status != models.StatusHealthy {
		t.Fatalf("schedule/status = %s %d %s", job.Schedule, job.GraceTime, job.Status)
	}
	if job.UserID != behaviorUserID {
		t.Fatalf("user_id = %q, want %q", job.UserID, behaviorUserID)
	}
	if job.NextExpect.Before(time.Now().UTC().Add(-2 * time.Second)) {
		t.Fatalf("next_expect = %s", job.NextExpect)
	}
	if job.LastPing.IsZero() {
		t.Fatal("last_ping is zero")
	}

	listed := do(t, srv, http.MethodGet, "/api/jobs", "")
	jobs := decodeJobs(t, listed.Body)
	if len(jobs) != 1 || jobs[0].ID != job.ID {
		t.Fatalf("list = %+v", jobs)
	}

	got := do(t, srv, http.MethodGet, "/api/jobs/"+job.ID, "")
	if got.StatusCode != http.StatusOK {
		t.Fatalf("get status = %d", got.StatusCode)
	}
	if decodeJob(t, got.Body).ID != job.ID {
		t.Fatal("get returned a different job")
	}

	pinged := do(t, srv, http.MethodPost, "/api/ping/"+job.ID, "")
	if pinged.StatusCode != http.StatusOK || pinged.Body != `{"status":"ok"}` {
		t.Fatalf("ping = %d %q", pinged.StatusCode, pinged.Body)
	}
	afterPing := decodeJob(t, do(t, srv, http.MethodGet, "/api/jobs/"+job.ID, "").Body)
	if afterPing.Status != models.StatusHealthy {
		t.Fatalf("status after ping = %s", afterPing.Status)
	}
	if afterPing.LastPing.Before(job.LastPing) {
		t.Fatalf("last_ping moved backwards: %s -> %s", job.LastPing, afterPing.LastPing)
	}
	if afterPing.NextExpect.IsZero() {
		t.Fatal("next_expect cleared by ping")
	}

	updated := do(t, srv, http.MethodPut, "/api/jobs/"+job.ID, `{"schedule":"0 12 * * *"}`)
	if updated.StatusCode != http.StatusOK {
		t.Fatalf("update status = %d body %s", updated.StatusCode, updated.Body)
	}
	afterUpdate := decodeJob(t, updated.Body)
	if afterUpdate.Schedule != "0 12 * * *" {
		t.Fatalf("schedule = %q", afterUpdate.Schedule)
	}
	if afterUpdate.NextExpect.Equal(afterPing.NextExpect) {
		t.Fatal("schedule change left next_expect unchanged")
	}
	if !afterUpdate.LastPing.Equal(afterPing.LastPing) {
		t.Fatalf("schedule change reset last_ping: %s -> %s", afterPing.LastPing, afterUpdate.LastPing)
	}
	if afterUpdate.Name != "Nightly backup" || afterUpdate.GraceTime != 15 {
		t.Fatalf("update changed unrelated fields: %+v", afterUpdate)
	}

	deleted := do(t, srv, http.MethodDelete, "/api/jobs/"+job.ID, "")
	if deleted.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %d", deleted.StatusCode)
	}
	missing := do(t, srv, http.MethodGet, "/api/jobs/"+job.ID, "")
	if missing.StatusCode != http.StatusNotFound || strings.TrimSpace(missing.Body) != "Job not found" {
		t.Fatalf("get after delete = %d %q", missing.StatusCode, missing.Body)
	}
}

func TestJobAPIRejectsInvalidInput(t *testing.T) {
	srv := newBehaviorServer(t)

	cases := []struct {
		name   string
		method string
		path   string
		body   string
		status int
		want   string
	}{
		{"invalid json", http.MethodPost, "/api/jobs", "{", http.StatusBadRequest, "Invalid request body"},
		{"missing name", http.MethodPost, "/api/jobs", `{"schedule":"0 0 * * *"}`, http.StatusBadRequest, "Name and schedule are required"},
		{"invalid cron", http.MethodPost, "/api/jobs", `{"name":"x","schedule":"not a cron"}`, http.StatusBadRequest, "Invalid CRON schedule provided"},
		{"unknown job", http.MethodGet, "/api/jobs/does-not-exist", "", http.StatusNotFound, "Job not found"},
		{"unknown ping", http.MethodPost, "/api/ping/does-not-exist", "", http.StatusNotFound, "Job not found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := do(t, srv, tc.method, tc.path, tc.body)
			if resp.StatusCode != tc.status || strings.TrimSpace(resp.Body) != tc.want {
				t.Fatalf("got %d %q, want %d %q", resp.StatusCode, resp.Body, tc.status, tc.want)
			}
		})
	}

	created := decodeJob(t, do(t, srv, http.MethodPost, "/api/jobs", `{"name":"x","schedule":"0 0 * * *","grace_time":5}`).Body)
	badUpdate := do(t, srv, http.MethodPut, "/api/jobs/"+created.ID, `{"schedule":"not a cron"}`)
	if badUpdate.StatusCode != http.StatusBadRequest || strings.TrimSpace(badUpdate.Body) != "Invalid CRON schedule provided" {
		t.Fatalf("bad update = %d %q", badUpdate.StatusCode, badUpdate.Body)
	}
	unchanged := decodeJob(t, do(t, srv, http.MethodGet, "/api/jobs/"+created.ID, "").Body)
	if unchanged.Schedule != "0 0 * * *" {
		t.Fatalf("invalid update changed schedule to %q", unchanged.Schedule)
	}
}

func TestHealthzReportsDatabase(t *testing.T) {
	srv := newBehaviorServer(t)
	resp := doAs(t, srv, "", http.MethodGet, "/healthz", "")
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(resp.Body) != `{"status":"ok"}` {
		t.Fatalf("healthz = %d %q", resp.StatusCode, resp.Body)
	}
	if resp.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("content-type = %q", resp.Header.Get("Content-Type"))
	}
}

func TestPreflightAllowsBrowserCalls(t *testing.T) {
	srv := newBehaviorServer(t)
	resp := do(t, srv, http.MethodOptions, "/api/jobs", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("options status = %d", resp.StatusCode)
	}
	if resp.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("origin = %q", resp.Header.Get("Access-Control-Allow-Origin"))
	}
	if !strings.Contains(resp.Header.Get("Access-Control-Allow-Methods"), "POST") {
		t.Fatalf("methods = %q", resp.Header.Get("Access-Control-Allow-Methods"))
	}
}

func TestJobDetailEventsPauseAndEdit(t *testing.T) {
	srv := newBehaviorServer(t)
	database, err := db.NewDatabase()
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	created := do(t, srv, http.MethodPost, "/api/jobs", `{
		"name": "Nightly backup",
		"description": "Daily database backup",
		"schedule": "0 0 * * *",
		"grace_time": 15
	}`)
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("create = %d %s", created.StatusCode, created.Body)
	}
	job := decodeJob(t, do(t, srv, http.MethodGet, "/api/jobs/"+decodeJob(t, created.Body).ID, "").Body)

	empty := do(t, srv, http.MethodGet, "/api/jobs/"+job.ID+"/events", "")
	if empty.StatusCode != http.StatusOK || strings.TrimSpace(empty.Body) != "[]" {
		t.Fatalf("empty events = %d %q", empty.StatusCode, empty.Body)
	}

	unauthReq, err := http.NewRequest(http.MethodGet, srv.URL+"/api/jobs/"+job.ID+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	unauthResp, err := http.DefaultClient.Do(unauthReq)
	if err != nil {
		t.Fatal(err)
	}
	unauthResp.Body.Close()
	if unauthResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated events = %d", unauthResp.StatusCode)
	}

	missing := do(t, srv, http.MethodGet, "/api/jobs/does-not-exist/events", "")
	if missing.StatusCode != http.StatusNotFound || strings.TrimSpace(missing.Body) != "Job not found" {
		t.Fatalf("unknown events = %d %q", missing.StatusCode, missing.Body)
	}

	pinged := do(t, srv, http.MethodPost, "/api/ping/"+job.ID, "")
	if pinged.StatusCode != http.StatusOK {
		t.Fatalf("ping = %d %s", pinged.StatusCode, pinged.Body)
	}
	events := decodeEvents(t, do(t, srv, http.MethodGet, "/api/jobs/"+job.ID+"/events?limit=1", "").Body)
	if len(events) != 1 || events[0].Type != models.TypePing || events[0].JobID != job.ID {
		t.Fatalf("events = %+v", events)
	}

	beforePause := decodeJob(t, do(t, srv, http.MethodGet, "/api/jobs/"+job.ID, "").Body)
	paused := do(t, srv, http.MethodPut, "/api/jobs/"+job.ID, `{"status":"paused"}`)
	if paused.StatusCode != http.StatusOK {
		t.Fatalf("pause = %d %s", paused.StatusCode, paused.Body)
	}
	afterPause := decodeJob(t, paused.Body)
	if afterPause.Status != models.StatusPaused {
		t.Fatalf("status = %s", afterPause.Status)
	}
	if !afterPause.NextExpect.Equal(beforePause.NextExpect) || !afterPause.LastPing.Equal(beforePause.LastPing) {
		t.Fatalf("pause changed timing: next %s -> %s last %s -> %s", beforePause.NextExpect, afterPause.NextExpect, beforePause.LastPing, afterPause.LastPing)
	}

	if pinged := do(t, srv, http.MethodPost, "/api/ping/"+job.ID, ""); pinged.StatusCode != http.StatusOK {
		t.Fatalf("ping while paused = %d", pinged.StatusCode)
	}
	stillPaused := decodeJob(t, do(t, srv, http.MethodGet, "/api/jobs/"+job.ID, "").Body)
	if stillPaused.Status != models.StatusPaused {
		t.Fatalf("ping resumed the job: %s", stillPaused.Status)
	}

	stale := time.Now().UTC().Add(-48 * time.Hour)
	if _, err := database.GetDB().Exec(`UPDATE jobs SET next_expect = $1 WHERE id = $2`, stale, job.ID); err != nil {
		t.Fatalf("set stale next_expect: %v", err)
	}
	resumed := do(t, srv, http.MethodPut, "/api/jobs/"+job.ID, `{"status":"healthy"}`)
	if resumed.StatusCode != http.StatusOK {
		t.Fatalf("resume = %d %s", resumed.StatusCode, resumed.Body)
	}
	afterResume := decodeJob(t, resumed.Body)
	if afterResume.Status != models.StatusHealthy {
		t.Fatalf("status = %s", afterResume.Status)
	}
	if afterResume.NextExpect.Before(time.Now().UTC().Add(-2 * time.Second)) {
		t.Fatalf("resume left next_expect in the past: %s", afterResume.NextExpect)
	}
	if afterResume.LastPing.Before(stillPaused.LastPing) {
		t.Fatal("resume moved last_ping backwards")
	}

	edited := do(t, srv, http.MethodPut, "/api/jobs/"+job.ID, `{
		"name": "Morning backup",
		"description": "",
		"schedule": "0 12 * * *",
		"grace_time": 20
	}`)
	if edited.StatusCode != http.StatusOK {
		t.Fatalf("edit = %d %s", edited.StatusCode, edited.Body)
	}
	afterEdit := decodeJob(t, edited.Body)
	if afterEdit.Name != "Morning backup" || afterEdit.Description != "" || afterEdit.Schedule != "0 12 * * *" || afterEdit.GraceTime != 20 {
		t.Fatalf("edit = %+v", afterEdit)
	}
	if afterEdit.NextExpect.Equal(afterResume.NextExpect) {
		t.Fatal("schedule edit left next_expect unchanged")
	}

	badStatus := do(t, srv, http.MethodPut, "/api/jobs/"+job.ID, `{"status":"asleep"}`)
	if badStatus.StatusCode != http.StatusBadRequest || strings.TrimSpace(badStatus.Body) != "Invalid status" {
		t.Fatalf("bad status = %d %q", badStatus.StatusCode, badStatus.Body)
	}
	if decodeJob(t, do(t, srv, http.MethodGet, "/api/jobs/"+job.ID, "").Body).Status != models.StatusHealthy {
		t.Fatal("invalid status was saved")
	}

	badGrace := do(t, srv, http.MethodPut, "/api/jobs/"+job.ID, `{"grace_time":0}`)
	if badGrace.StatusCode != http.StatusBadRequest || strings.TrimSpace(badGrace.Body) != "Grace time must be positive" {
		t.Fatalf("bad grace = %d %q", badGrace.StatusCode, badGrace.Body)
	}

	forced := do(t, srv, http.MethodPut, "/api/jobs/"+job.ID, `{"status":"missing"}`)
	if forced.StatusCode != http.StatusBadRequest || strings.TrimSpace(forced.Body) != "Invalid status" {
		t.Fatalf("force missing = %d %q", forced.StatusCode, forced.Body)
	}
	if decodeJob(t, do(t, srv, http.MethodGet, "/api/jobs/"+job.ID, "").Body).Status != models.StatusHealthy {
		t.Fatal("client marked the job missing")
	}
}

func TestOtherUsersJobLooksMissing(t *testing.T) {
	srv := newBehaviorServer(t)
	database, err := db.NewDatabase()
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	if _, err := database.GetDB().Exec(`
		INSERT INTO users (id, email, name, password_hash, created_at, updated_at)
		VALUES ('other-user', 'other@example.com', 'Other', 'x', NOW(), NOW())
		ON CONFLICT (id) DO NOTHING
	`); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	other := &models.Job{
		Name:       "Someone else",
		Schedule:   "0 0 * * *",
		GraceTime:  5,
		Status:     models.StatusHealthy,
		LastPing:   time.Now().UTC(),
		NextExpect: time.Now().UTC().Add(time.Hour),
		UserID:     "other-user",
	}
	if err := database.CreateJob(other); err != nil {
		t.Fatalf("create other job: %v", err)
	}

	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/jobs/" + other.ID, ""},
		{http.MethodGet, "/api/jobs/" + other.ID + "/events", ""},
		{http.MethodPut, "/api/jobs/" + other.ID, `{"name":"taken"}`},
		{http.MethodDelete, "/api/jobs/" + other.ID, ""},
	} {
		resp := do(t, srv, tc.method, tc.path, tc.body)
		if resp.StatusCode != http.StatusNotFound || strings.TrimSpace(resp.Body) != "Job not found" {
			t.Fatalf("%s %s = %d %q", tc.method, tc.path, resp.StatusCode, resp.Body)
		}
	}

	stillThere, err := database.GetJob(other.ID)
	if err != nil || stillThere == nil || stillThere.Name != "Someone else" {
		t.Fatalf("other job changed: %+v %v", stillThere, err)
	}

	pinged := do(t, srv, http.MethodPost, "/api/ping/"+other.ID, "")
	if pinged.StatusCode != http.StatusOK || pinged.Body != `{"status":"ok"}` {
		t.Fatalf("ping other job = %d %q", pinged.StatusCode, pinged.Body)
	}
}

func TestAccounts(t *testing.T) {
	srv := newBehaviorServer(t)

	me := do(t, srv, http.MethodGet, "/api/me", "")
	if me.StatusCode != http.StatusOK {
		t.Fatalf("me = %d %s", me.StatusCode, me.Body)
	}
	if strings.Contains(me.Body, "password") {
		t.Fatalf("password leaked: %s", me.Body)
	}
	var profile struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := json.Unmarshal([]byte(me.Body), &profile); err != nil {
		t.Fatal(err)
	}
	if profile.ID != behaviorUserID || profile.Email != "behavior@example.com" || profile.Name != "Behavior" {
		t.Fatalf("profile = %+v", profile)
	}

	registered := doAs(t, srv, "", http.MethodPost, "/api/register", `{
		"email": "Cookie@example.com",
		"password": "cookie-pass",
		"name": "Cookie"
	}`)
	cookies := (&http.Response{Header: registered.Header}).Cookies()
	if registered.StatusCode != http.StatusCreated {
		t.Fatalf("register = %d %s", registered.StatusCode, registered.Body)
	}
	var session struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(registered.Body), &session); err != nil {
		t.Fatal(err)
	}
	var sessionCookie *http.Cookie
	for _, cookie := range cookies {
		if cookie.Name == "opensentry_session" {
			sessionCookie = cookie
		}
	}
	if sessionCookie == nil || sessionCookie.Value == "" || !sessionCookie.HttpOnly {
		t.Fatalf("session cookie = %+v", cookies)
	}
	if sessionCookie.Value != session.Token {
		t.Fatal("cookie and json token differ")
	}

	cookieMe, err := http.NewRequest(http.MethodGet, srv.URL+"/api/me", nil)
	if err != nil {
		t.Fatal(err)
	}
	cookieMe.AddCookie(sessionCookie)
	cookieResp, err := http.DefaultClient.Do(cookieMe)
	if err != nil {
		t.Fatal(err)
	}
	cookieBody, _ := io.ReadAll(cookieResp.Body)
	cookieResp.Body.Close()
	if cookieResp.StatusCode != http.StatusOK || !strings.Contains(string(cookieBody), "cookie@example.com") {
		t.Fatalf("cookie me = %d %s", cookieResp.StatusCode, cookieBody)
	}

	wrong := doAs(t, srv, "", http.MethodPost, "/api/login", `{
		"email": "behavior@example.com",
		"password": "not-the-password"
	}`)
	unknown := doAs(t, srv, "", http.MethodPost, "/api/login", `{
		"email": "nobody@example.com",
		"password": "behavior-pass"
	}`)
	if wrong.StatusCode != http.StatusUnauthorized || unknown.StatusCode != http.StatusUnauthorized {
		t.Fatalf("login failures = %d %d", wrong.StatusCode, unknown.StatusCode)
	}
	if strings.TrimSpace(wrong.Body) != "Invalid email or password" || wrong.Body != unknown.Body {
		t.Fatalf("login errors differ: %q vs %q", wrong.Body, unknown.Body)
	}

	duplicate := doAs(t, srv, "", http.MethodPost, "/api/register", `{
		"email": "behavior@example.com",
		"password": "behavior-pass",
		"name": "Again"
	}`)
	if duplicate.StatusCode != http.StatusConflict || strings.TrimSpace(duplicate.Body) != "Email already registered" {
		t.Fatalf("duplicate = %d %q", duplicate.StatusCode, duplicate.Body)
	}

	short := doAs(t, srv, "", http.MethodPost, "/api/register", `{
		"email": "short@example.com",
		"password": "short",
		"name": "Short"
	}`)
	if short.StatusCode != http.StatusBadRequest || strings.TrimSpace(short.Body) != "Password must be at least 8 characters" {
		t.Fatalf("short password = %d %q", short.StatusCode, short.Body)
	}

	stale := doAs(t, srv, "not-a-real-session", http.MethodPost, "/api/logout", "")
	if stale.StatusCode != http.StatusNoContent || !sessionCookieCleared(stale.Header) {
		t.Fatalf("stale logout = %d cookies %v", stale.StatusCode, stale.Header.Values("Set-Cookie"))
	}
	if do(t, srv, http.MethodGet, "/api/me", "").StatusCode != http.StatusOK {
		t.Fatal("stale logout removed the real session")
	}

	loggedOut := do(t, srv, http.MethodPost, "/api/logout", "")
	if loggedOut.StatusCode != http.StatusNoContent || !sessionCookieCleared(loggedOut.Header) {
		t.Fatalf("logout = %d cookies %v body %s", loggedOut.StatusCode, loggedOut.Header.Values("Set-Cookie"), loggedOut.Body)
	}
	afterLogout := do(t, srv, http.MethodGet, "/api/me", "")
	if afterLogout.StatusCode != http.StatusUnauthorized {
		t.Fatalf("me after logout = %d %s", afterLogout.StatusCode, afterLogout.Body)
	}

	loggedIn := doAs(t, srv, "", http.MethodPost, "/api/login", `{
		"email": " Behavior@Example.com ",
		"password": "behavior-pass"
	}`)
	if loggedIn.StatusCode != http.StatusOK {
		t.Fatalf("login = %d %s", loggedIn.StatusCode, loggedIn.Body)
	}
	var logged struct {
		Token string `json:"token"`
		User  struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if err := json.Unmarshal([]byte(loggedIn.Body), &logged); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(loggedIn.Body, "password") || logged.User.ID != behaviorUserID || logged.Token == "" {
		t.Fatalf("login payload = %s", loggedIn.Body)
	}
	behaviorToken = logged.Token

	created := do(t, srv, http.MethodPost, "/api/jobs", `{
		"name": "Mine",
		"schedule": "0 0 * * *",
		"grace_time": 5
	}`)
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("create = %d %s", created.StatusCode, created.Body)
	}
	job := decodeJob(t, created.Body)

	other := doAs(t, srv, "", http.MethodPost, "/api/register", `{
		"email": "second@example.com",
		"password": "second-pass",
		"name": "Second"
	}`)
	if other.StatusCode != http.StatusCreated {
		t.Fatalf("second register = %d %s", other.StatusCode, other.Body)
	}
	var otherSession struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(other.Body), &otherSession); err != nil {
		t.Fatal(err)
	}
	otherList := doAs(t, srv, otherSession.Token, http.MethodGet, "/api/jobs", "")
	if strings.TrimSpace(otherList.Body) != "[]" {
		t.Fatalf("second user list = %s", otherList.Body)
	}
	otherGet := doAs(t, srv, otherSession.Token, http.MethodGet, "/api/jobs/"+job.ID, "")
	if otherGet.StatusCode != http.StatusNotFound {
		t.Fatalf("second user get = %d %s", otherGet.StatusCode, otherGet.Body)
	}
	ownList := do(t, srv, http.MethodGet, "/api/jobs", "")
	jobs := decodeJobs(t, ownList.Body)
	if len(jobs) != 1 || jobs[0].ID != job.ID {
		t.Fatalf("owner list = %+v", jobs)
	}
}

func decodeEvents(t *testing.T, body string) []models.JobEvent {
	t.Helper()
	var events []models.JobEvent
	if err := json.Unmarshal([]byte(body), &events); err != nil {
		t.Fatalf("decode events %q: %v", body, err)
	}
	return events
}

type recorded struct {
	StatusCode int
	Header     http.Header
	Body       string
}

func newBehaviorServer(t *testing.T) *httptest.Server {
	t.Helper()
	t.Setenv("DB_NAME", "cronsentry_test")
	ensureTestDatabase(t)

	database, err := db.NewDatabase()
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := database.InitDatabase(); err != nil {
		t.Fatalf("init schema: %v", err)
	}
	if _, err := database.GetDB().Exec(`TRUNCATE TABLE users RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate users: %v", err)
	}

	srv := httptest.NewServer(NewServer(database, log.New(io.Discard, "", 0)).Router())
	t.Cleanup(srv.Close)

	registered := doAs(t, srv, "", http.MethodPost, "/api/register", `{
		"email": "behavior@example.com",
		"password": "behavior-pass",
		"name": "Behavior"
	}`)
	if registered.StatusCode != http.StatusCreated {
		t.Fatalf("register behavior user: %d %s", registered.StatusCode, registered.Body)
	}
	var session struct {
		Token string `json:"token"`
		User  struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if err := json.Unmarshal([]byte(registered.Body), &session); err != nil {
		t.Fatalf("decode register: %v", err)
	}
	if session.Token == "" || session.User.ID == "" {
		t.Fatalf("register session = %+v", session)
	}
	behaviorToken = session.Token
	behaviorUserID = session.User.ID
	return srv
}

func ensureTestDatabase(t *testing.T) {
	t.Helper()
	host := envOr("DB_HOST", "localhost")
	port := envOr("DB_PORT", "5432")
	user := envOr("DB_USER", "postgres")
	password := envOr("DB_PASSWORD", "postgres")
	sslmode := envOr("DB_SSLMODE", "disable")
	admin, err := sql.Open("postgres", fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=postgres sslmode=%s", host, port, user, password, sslmode))
	if err != nil {
		t.Fatalf("open admin db: %v", err)
	}
	defer admin.Close()
	if err := admin.Ping(); err != nil {
		t.Fatalf("postgres is required for behavior tests: %v", err)
	}
	if _, err := admin.Exec(`CREATE DATABASE cronsentry_test`); err != nil && !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("create test database: %v", err)
	}
}

func sessionCookieCleared(header http.Header) bool {
	for _, cookie := range (&http.Response{Header: header}).Cookies() {
		if cookie.Name == "opensentry_session" && cookie.MaxAge < 0 {
			return true
		}
	}
	return false
}

func do(t *testing.T, srv *httptest.Server, method, path, body string) recorded {
	t.Helper()
	return doAs(t, srv, behaviorToken, method, path, body)
}

func doAs(t *testing.T, srv *httptest.Server, token, method, path, body string) recorded {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return recorded{StatusCode: resp.StatusCode, Header: resp.Header.Clone(), Body: string(payload)}
}

func decodeJob(t *testing.T, body string) models.Job {
	t.Helper()
	var job models.Job
	if err := json.Unmarshal([]byte(body), &job); err != nil {
		t.Fatalf("decode job %q: %v", body, err)
	}
	return job
}

func decodeJobs(t *testing.T, body string) []models.Job {
	t.Helper()
	var jobs []models.Job
	if err := json.Unmarshal([]byte(body), &jobs); err != nil {
		t.Fatalf("decode jobs %q: %v", body, err)
	}
	return jobs
}

var (
	behaviorToken  string
	behaviorUserID string
)

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
