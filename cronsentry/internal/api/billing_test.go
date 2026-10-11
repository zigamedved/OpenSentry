package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSmallestScheduleGap(t *testing.T) {
	from := time.Date(2026, 10, 11, 12, 0, 30, 0, time.UTC)
	minute, err := smallestScheduleGap("* * * * *", from)
	if err != nil || minute != time.Minute {
		t.Fatalf("every minute = %s %v", minute, err)
	}
	five, err := smallestScheduleGap("*/5 * * * *", from)
	if err != nil || five != 5*time.Minute {
		t.Fatalf("every five = %s %v", five, err)
	}
	hour, err := smallestScheduleGap("0 * * * *", from)
	if err != nil || hour != time.Hour {
		t.Fatalf("hourly = %s %v", hour, err)
	}
}

func TestStripeSignature(t *testing.T) {
	body := []byte(`{"id":"evt_1"}`)
	secret := "whsec_test"
	now := time.Now()
	header := stripeHeader(secret, body, now)
	if err := verifyStripeSignature(body, header, secret, now); err != nil {
		t.Fatal(err)
	}
	if err := verifyStripeSignature(body, header, "other", now); err == nil {
		t.Fatal("accepted the wrong secret")
	}
	stale := stripeHeader(secret, body, now.Add(-10*time.Minute))
	if err := verifyStripeSignature(body, stale, secret, now); err == nil {
		t.Fatal("accepted a stale signature")
	}
}

func TestSelfHostBillingIsUnlimited(t *testing.T) {
	t.Setenv("STRIPE_SECRET_KEY", "")
	t.Setenv("STRIPE_WEBHOOK_SECRET", "")
	t.Setenv("STRIPE_PRICE_ID", "")
	srv := newBehaviorServer(t)
	view := do(t, srv, http.MethodGet, "/api/billing", "")
	if view.StatusCode != http.StatusOK || !strings.Contains(view.Body, `"mode":"self_host"`) {
		t.Fatalf("billing = %d %s", view.StatusCode, view.Body)
	}
	if !strings.Contains(view.Body, "Self-hosted: unlimited checks") {
		t.Fatalf("message = %s", view.Body)
	}
	for i := 0; i < 6; i++ {
		created := do(t, srv, http.MethodPost, "/api/jobs", fmt.Sprintf(`{"name":"job-%d","schedule":"* * * * *","grace_time":5}`, i))
		if created.StatusCode != http.StatusCreated {
			t.Fatalf("create %d = %d %s", i, created.StatusCode, created.Body)
		}
	}
}

func TestHostedPlanLimitsAndWebhook(t *testing.T) {
	t.Setenv("STRIPE_SECRET_KEY", "sk_test_x")
	t.Setenv("STRIPE_WEBHOOK_SECRET", "whsec_test")
	t.Setenv("STRIPE_PRICE_ID", "price_pro")
	t.Setenv("DASHBOARD_URL", "http://localhost:5173")
	stripe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk_test_x" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/checkout/sessions":
			_, _ = w.Write([]byte(`{"id":"cs_test","url":"https://checkout.stripe.test/c/cs_test"}`))
		case "/v1/billing_portal/sessions":
			_, _ = w.Write([]byte(`{"id":"bps_test","url":"https://billing.stripe.test/p/bps_test"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(stripe.Close)
	t.Setenv("STRIPE_API_BASE", stripe.URL)

	srv := newBehaviorServer(t)
	view := do(t, srv, http.MethodGet, "/api/billing", "")
	if view.StatusCode != http.StatusOK || !strings.Contains(view.Body, `"plan":"free"`) || !strings.Contains(view.Body, "Free plan: 0 of 5 checks") {
		t.Fatalf("billing = %d %s", view.StatusCode, view.Body)
	}

	tooFast := do(t, srv, http.MethodPost, "/api/jobs", `{"name":"fast","schedule":"* * * * *","grace_time":5}`)
	if tooFast.StatusCode != http.StatusBadRequest || !strings.Contains(tooFast.Body, "at least 5 minutes") {
		t.Fatalf("fast schedule = %d %s", tooFast.StatusCode, tooFast.Body)
	}

	for i := 0; i < freeMaxChecks; i++ {
		created := do(t, srv, http.MethodPost, "/api/jobs", fmt.Sprintf(`{"name":"job-%d","schedule":"0 * * * *","grace_time":5}`, i))
		if created.StatusCode != http.StatusCreated {
			t.Fatalf("create %d = %d %s", i, created.StatusCode, created.Body)
		}
	}
	blocked := do(t, srv, http.MethodPost, "/api/jobs", `{"name":"one-more","schedule":"0 * * * *","grace_time":5}`)
	if blocked.StatusCode != http.StatusPaymentRequired || strings.TrimSpace(blocked.Body) != "Free plan allows 5 checks. Upgrade to add more." {
		t.Fatalf("over limit = %d %q", blocked.StatusCode, blocked.Body)
	}

	checkout := do(t, srv, http.MethodPost, "/api/billing/checkout", "")
	if checkout.StatusCode != http.StatusOK || !strings.Contains(checkout.Body, "https://checkout.stripe.test/c/cs_test") {
		t.Fatalf("checkout = %d %s", checkout.StatusCode, checkout.Body)
	}
	portal := do(t, srv, http.MethodPost, "/api/billing/portal", "")
	if portal.StatusCode != http.StatusBadRequest || !strings.Contains(portal.Body, "Subscribe before opening") {
		t.Fatalf("portal before subscribe = %d %s", portal.StatusCode, portal.Body)
	}

	unsigned := do(t, srv, http.MethodPost, "/api/stripe/webhook", `{"id":"evt_x","created":1}`)
	if unsigned.StatusCode != http.StatusBadRequest {
		t.Fatalf("unsigned webhook = %d", unsigned.StatusCode)
	}

	postWebhook(t, srv, "whsec_test", subscriptionEvent("evt_old", behaviorUserID, "active", "price_pro", 100))
	postWebhook(t, srv, "whsec_test", subscriptionEvent("evt_new", behaviorUserID, "active", "price_pro", 200))
	// An older delivery must not downgrade the newer active subscription.
	postWebhook(t, srv, "whsec_test", subscriptionEvent("evt_late", behaviorUserID, "canceled", "price_pro", 150))
	upgraded := do(t, srv, http.MethodGet, "/api/billing", "")
	if !strings.Contains(upgraded.Body, `"plan":"pro"`) || !strings.Contains(upgraded.Body, "Pro plan: 5 of 50") {
		t.Fatalf("after webhook = %s", upgraded.Body)
	}
	replay := postWebhook(t, srv, "whsec_test", subscriptionEvent("evt_new", behaviorUserID, "canceled", "price_pro", 200))
	if replay.StatusCode != http.StatusOK {
		t.Fatalf("replay = %d %s", replay.StatusCode, replay.Body)
	}
	if still := do(t, srv, http.MethodGet, "/api/billing", ""); !strings.Contains(still.Body, `"plan":"pro"`) {
		t.Fatalf("replay changed plan: %s", still.Body)
	}

	extra := do(t, srv, http.MethodPost, "/api/jobs", `{"name":"pro-job","schedule":"* * * * *","grace_time":5}`)
	if extra.StatusCode != http.StatusCreated {
		t.Fatalf("pro create = %d %s", extra.StatusCode, extra.Body)
	}

	postWebhook(t, srv, "whsec_test", subscriptionEvent("evt_cancel", behaviorUserID, "canceled", "price_pro", 300))
	downgraded := do(t, srv, http.MethodGet, "/api/billing", "")
	if !strings.Contains(downgraded.Body, `"plan":"free"`) {
		t.Fatalf("after cancel = %s", downgraded.Body)
	}
	again := do(t, srv, http.MethodPost, "/api/jobs", `{"name":"after-cancel","schedule":"0 * * * *","grace_time":5}`)
	if again.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("create after cancel = %d %s", again.StatusCode, again.Body)
	}

	bound := postWebhook(t, srv, "whsec_test", fmt.Sprintf(`{
		"id":"evt_checkout","type":"checkout.session.completed","created":400,
		"data":{"object":{"customer":"cus_1","subscription":"sub_1","client_reference_id":%q}}
	}`, behaviorUserID))
	if bound.StatusCode != http.StatusOK {
		t.Fatalf("checkout event = %d %s", bound.StatusCode, bound.Body)
	}
	if portal := do(t, srv, http.MethodPost, "/api/billing/portal", ""); portal.StatusCode != http.StatusOK || !strings.Contains(portal.Body, "https://billing.stripe.test/p/bps_test") {
		t.Fatalf("portal = %d %s", portal.StatusCode, portal.Body)
	}
}

func subscriptionEvent(id, userID, status, price string, created int64) string {
	return fmt.Sprintf(`{
		"id":%q,"type":"customer.subscription.updated","created":%d,
		"data":{"object":{
			"id":"sub_1","customer":"cus_1","status":%q,
			"metadata":{"user_id":%q},
			"items":{"data":[{"price":{"id":%q}}]}
		}}
	}`, id, created, status, userID, price)
}

func postWebhook(t *testing.T, srv *httptest.Server, secret, body string) recorded {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/stripe/webhook", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Stripe-Signature", stripeHeader(secret, []byte(body), time.Now()))
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

func stripeHeader(secret string, body []byte, when time.Time) string {
	timestamp := when.Unix()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(fmt.Sprintf("%d.%s", timestamp, body)))
	return fmt.Sprintf("t=%d,v1=%s", timestamp, hex.EncodeToString(mac.Sum(nil)))
}
