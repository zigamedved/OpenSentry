package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/adhocore/gronx"
)

const (
	freeMaxChecks   = 5
	proMaxChecks    = 50
	freeMinGap      = 5 * time.Minute
	proMinGap       = time.Minute
	stripeTolerance = 5 * time.Minute
)

type billingConfig struct {
	enabled       bool
	secretKey     string
	webhookSecret string
	priceID       string
	apiBase       string
}

func billingConfigFromEnv(logger *log.Logger) billingConfig {
	cfg := billingConfig{
		secretKey:     strings.TrimSpace(os.Getenv("STRIPE_SECRET_KEY")),
		webhookSecret: strings.TrimSpace(os.Getenv("STRIPE_WEBHOOK_SECRET")),
		priceID:       strings.TrimSpace(os.Getenv("STRIPE_PRICE_ID")),
		apiBase:       strings.TrimRight(strings.TrimSpace(os.Getenv("STRIPE_API_BASE")), "/"),
	}
	if cfg.apiBase == "" {
		cfg.apiBase = "https://api.stripe.com"
	}
	set := 0
	for _, value := range []string{cfg.secretKey, cfg.webhookSecret, cfg.priceID} {
		if value != "" {
			set++
		}
	}
	cfg.enabled = set == 3
	if set > 0 && !cfg.enabled && logger != nil {
		logger.Printf("Stripe billing stays off until STRIPE_SECRET_KEY, STRIPE_WEBHOOK_SECRET, and STRIPE_PRICE_ID are all set. Self-host checks stay unlimited.")
	}
	return cfg
}

func (s *Server) handleBilling(w http.ResponseWriter, r *http.Request) {
	view, err := s.billingView(currentUserID(r))
	if err != nil {
		s.logger.Printf("billing: %v", err)
		http.Error(w, "Failed to load billing", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) billingView(userID string) (map[string]any, error) {
	used, err := s.db.CountJobsByUser(userID)
	if err != nil {
		return nil, err
	}
	if !s.billing.enabled {
		return map[string]any{
			"mode":                 "self_host",
			"plan":                 "self_host",
			"status":               "",
			"max_checks":           nil,
			"min_interval_seconds": 0,
			"used_checks":          used,
			"portal":               false,
			"message":              "Self-hosted: unlimited checks. Hosted OpenSentry uses Free (5 checks, schedules at least 5 minutes apart) or Pro (50 checks, at least 1 minute apart).",
		}, nil
	}
	account, err := s.db.BillingAccount(userID)
	if err != nil {
		return nil, err
	}
	plan := "free"
	maxChecks := freeMaxChecks
	minGap := freeMinGap
	if account.Pro() {
		plan = "pro"
		maxChecks = proMaxChecks
		minGap = proMinGap
	}
	message := fmt.Sprintf("%s plan: %d of %d checks. Schedules must be at least %s apart.", planLabel(plan), used, maxChecks, gapLabel(minGap))
	return map[string]any{
		"mode":                 "hosted",
		"plan":                 plan,
		"status":               account.Status,
		"max_checks":           maxChecks,
		"min_interval_seconds": int(minGap.Seconds()),
		"used_checks":          used,
		"portal":               account.CustomerID != "",
		"message":              message,
	}, nil
}

func planLabel(plan string) string {
	if plan == "pro" {
		return "Pro"
	}
	return "Free"
}

func gapLabel(gap time.Duration) string {
	minutes := int(gap.Minutes())
	if minutes == 1 {
		return "1 minute"
	}
	return fmt.Sprintf("%d minutes", minutes)
}

func (s *Server) allowNewJob(w http.ResponseWriter, r *http.Request, schedule string) bool {
	if !s.allowSchedule(w, r, schedule) {
		return false
	}
	if !s.billing.enabled {
		return true
	}
	account, err := s.db.BillingAccount(currentUserID(r))
	if err != nil {
		s.logger.Printf("billing: %v", err)
		http.Error(w, "Failed to check plan", http.StatusInternalServerError)
		return false
	}
	used, err := s.db.CountJobsByUser(currentUserID(r))
	if err != nil {
		s.logger.Printf("billing: %v", err)
		http.Error(w, "Failed to check plan", http.StatusInternalServerError)
		return false
	}
	maxChecks := freeMaxChecks
	label := "Free"
	if account.Pro() {
		maxChecks = proMaxChecks
		label = "Pro"
	}
	if used >= maxChecks {
		if label == "Pro" {
			http.Error(w, fmt.Sprintf("Pro plan allows %d checks.", maxChecks), http.StatusPaymentRequired)
		} else {
			http.Error(w, fmt.Sprintf("Free plan allows %d checks. Upgrade to add more.", maxChecks), http.StatusPaymentRequired)
		}
		return false
	}
	return true
}

func (s *Server) allowSchedule(w http.ResponseWriter, r *http.Request, schedule string) bool {
	if !s.billing.enabled {
		return true
	}
	account, err := s.db.BillingAccount(currentUserID(r))
	if err != nil {
		s.logger.Printf("billing: %v", err)
		http.Error(w, "Failed to check plan", http.StatusInternalServerError)
		return false
	}
	minGap := freeMinGap
	label := "Free"
	if account.Pro() {
		minGap = proMinGap
		label = "Pro"
	}
	gap, err := smallestScheduleGap(schedule, time.Now().UTC())
	if err != nil {
		http.Error(w, "Invalid CRON schedule provided", http.StatusBadRequest)
		return false
	}
	if gap < minGap {
		http.Error(w, fmt.Sprintf("%s plan requires schedules at least %s apart.", label, gapLabel(minGap)), http.StatusBadRequest)
		return false
	}
	return true
}

func smallestScheduleGap(schedule string, from time.Time) (time.Duration, error) {
	// The wait until the next tick depends on the clock. The plan limit is the
	// gap between runs, so start at the next tick and measure the ones after it.
	cursor, err := gronx.NextTickAfter(schedule, from.UTC(), false)
	if err != nil {
		return 0, err
	}
	var min time.Duration
	for i := 0; i < 8; i++ {
		next, err := gronx.NextTickAfter(schedule, cursor, false)
		if err != nil {
			return 0, err
		}
		gap := next.Sub(cursor)
		if gap <= 0 {
			return 0, fmt.Errorf("schedule did not move forward")
		}
		if i == 0 || gap < min {
			min = gap
		}
		cursor = next
	}
	return min, nil
}

func (s *Server) handleBillingCheckout(w http.ResponseWriter, r *http.Request) {
	if !s.billing.enabled {
		http.Error(w, "Billing is not configured. Self-hosted checks are unlimited.", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(s.dashboardURL) == "" {
		http.Error(w, "Set DASHBOARD_URL before starting checkout", http.StatusBadRequest)
		return
	}
	user := currentUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	account, err := s.db.BillingAccount(user.ID)
	if err != nil {
		s.logger.Printf("billing: %v", err)
		http.Error(w, "Failed to start checkout", http.StatusInternalServerError)
		return
	}
	if account.Pro() {
		http.Error(w, "Already on the Pro plan. Use the billing portal to manage it.", http.StatusBadRequest)
		return
	}
	form := url.Values{}
	form.Set("mode", "subscription")
	form.Set("line_items[0][price]", s.billing.priceID)
	form.Set("line_items[0][quantity]", "1")
	form.Set("success_url", strings.TrimRight(s.dashboardURL, "/")+"/?billing=success")
	form.Set("cancel_url", strings.TrimRight(s.dashboardURL, "/")+"/?billing=cancel")
	form.Set("client_reference_id", user.ID)
	form.Set("metadata[user_id]", user.ID)
	form.Set("subscription_data[metadata][user_id]", user.ID)
	if account.CustomerID != "" {
		form.Set("customer", account.CustomerID)
	} else {
		form.Set("customer_email", user.Email)
	}
	var session struct {
		URL string `json:"url"`
	}
	if err := s.stripeForm("/v1/checkout/sessions", form, &session); err != nil {
		s.logger.Printf("stripe checkout: %v", err)
		http.Error(w, "Failed to start checkout", http.StatusBadGateway)
		return
	}
	if session.URL == "" {
		http.Error(w, "Failed to start checkout", http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": session.URL})
}

func (s *Server) handleBillingPortal(w http.ResponseWriter, r *http.Request) {
	if !s.billing.enabled {
		http.Error(w, "Billing is not configured. Self-hosted checks are unlimited.", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(s.dashboardURL) == "" {
		http.Error(w, "Set DASHBOARD_URL before opening the billing portal", http.StatusBadRequest)
		return
	}
	account, err := s.db.BillingAccount(currentUserID(r))
	if err != nil {
		s.logger.Printf("billing: %v", err)
		http.Error(w, "Failed to open billing portal", http.StatusInternalServerError)
		return
	}
	if account.CustomerID == "" {
		http.Error(w, "Subscribe before opening the billing portal.", http.StatusBadRequest)
		return
	}
	form := url.Values{}
	form.Set("customer", account.CustomerID)
	form.Set("return_url", strings.TrimRight(s.dashboardURL, "/"))
	var session struct {
		URL string `json:"url"`
	}
	if err := s.stripeForm("/v1/billing_portal/sessions", form, &session); err != nil {
		s.logger.Printf("stripe portal: %v", err)
		http.Error(w, "Failed to open billing portal", http.StatusBadGateway)
		return
	}
	if session.URL == "" {
		http.Error(w, "Failed to open billing portal", http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": session.URL})
}

func (s *Server) stripeForm(path string, form url.Values, dest any) error {
	req, err := http.NewRequest(http.MethodPost, s.billing.apiBase+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.billing.secretKey)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("stripe %s: %d %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, dest); err != nil {
		return err
	}
	return nil
}

func (s *Server) handleStripeWebhook(w http.ResponseWriter, r *http.Request) {
	if !s.billing.enabled {
		http.Error(w, "Billing is not configured", http.StatusNotFound)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "Invalid webhook body", http.StatusBadRequest)
		return
	}
	if err := verifyStripeSignature(body, r.Header.Get("Stripe-Signature"), s.billing.webhookSecret, time.Now()); err != nil {
		http.Error(w, "Invalid signature", http.StatusBadRequest)
		return
	}
	var event struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Created int64  `json:"created"`
		Data    struct {
			Object json.RawMessage `json:"object"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &event); err != nil || event.ID == "" || event.Created <= 0 {
		http.Error(w, "Invalid webhook body", http.StatusBadRequest)
		return
	}
	eventAt := time.Unix(event.Created, 0).UTC()
	applied, err := s.db.WithStripeEvent(event.ID, func() error {
		switch event.Type {
		case "checkout.session.completed":
			return s.bindCheckout(event.Data.Object)
		case "customer.subscription.created", "customer.subscription.updated", "customer.subscription.deleted":
			return s.applySubscriptionEvent(event.Data.Object, eventAt)
		default:
			return nil
		}
	})
	if err != nil {
		s.logger.Printf("stripe webhook %s: %v", event.Type, err)
		http.Error(w, "Failed to apply webhook", http.StatusInternalServerError)
		return
	}
	if !applied {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) bindCheckout(raw json.RawMessage) error {
	var session struct {
		Customer          json.RawMessage   `json:"customer"`
		Subscription      json.RawMessage   `json:"subscription"`
		ClientReferenceID string            `json:"client_reference_id"`
		Metadata          map[string]string `json:"metadata"`
	}
	if err := json.Unmarshal(raw, &session); err != nil {
		return err
	}
	userID := session.ClientReferenceID
	if userID == "" {
		userID = session.Metadata["user_id"]
	}
	if userID == "" {
		return nil
	}
	return s.db.BindStripeCustomer(userID, jsonID(session.Customer), jsonID(session.Subscription))
}

func (s *Server) applySubscriptionEvent(raw json.RawMessage, eventAt time.Time) error {
	var sub struct {
		ID       string            `json:"id"`
		Customer json.RawMessage   `json:"customer"`
		Status   string            `json:"status"`
		Metadata map[string]string `json:"metadata"`
		Items    struct {
			Data []struct {
				Price struct {
					ID string `json:"id"`
				} `json:"price"`
			} `json:"data"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &sub); err != nil {
		return err
	}
	customerID := jsonID(sub.Customer)
	userID := sub.Metadata["user_id"]
	if userID == "" {
		var err error
		userID, err = s.db.BillingUserByCustomer(customerID)
		if err != nil {
			return err
		}
	}
	if userID == "" {
		s.logger.Printf("stripe subscription %s has no user", sub.ID)
		return nil
	}
	priceID := ""
	if len(sub.Items.Data) > 0 {
		priceID = sub.Items.Data[0].Price.ID
	}
	return s.db.ApplySubscription(userID, customerID, sub.ID, sub.Status, priceID, s.billing.priceID, eventAt)
}

func jsonID(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var id string
	if err := json.Unmarshal(raw, &id); err == nil {
		return id
	}
	var obj struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		return obj.ID
	}
	return ""
}

func verifyStripeSignature(payload []byte, header, secret string, now time.Time) error {
	if strings.TrimSpace(secret) == "" || strings.TrimSpace(header) == "" {
		return errors.New("missing signature")
	}
	var timestamp string
	var signatures []string
	for _, part := range strings.Split(header, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch key {
		case "t":
			timestamp = value
		case "v1":
			signatures = append(signatures, value)
		}
	}
	if timestamp == "" || len(signatures) == 0 {
		return errors.New("missing signature")
	}
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return errors.New("bad timestamp")
	}
	signedAt := time.Unix(seconds, 0)
	if now.Sub(signedAt) > stripeTolerance || signedAt.Sub(now) > stripeTolerance {
		return errors.New("stale signature")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))
	for _, signature := range signatures {
		if hmac.Equal([]byte(expected), []byte(signature)) {
			return nil
		}
	}
	return errors.New("signature mismatch")
}
