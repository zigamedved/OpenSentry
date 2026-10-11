package db

import (
	"database/sql"
	"fmt"
	"time"
)

// BillingAccount is the hosted plan for one user. A missing row is the Free plan.
type BillingAccount struct {
	UserID         string
	Plan           string
	CustomerID     string
	SubscriptionID string
	Status         string
	EventCreatedAt time.Time
}

// Pro is the paid tier while Stripe reports the subscription as usable.
func (b BillingAccount) Pro() bool {
	if b.Plan != "pro" {
		return false
	}
	return b.Status == "active" || b.Status == "trialing"
}

func (d *Database) BillingAccount(userID string) (BillingAccount, error) {
	account := BillingAccount{UserID: userID, Plan: "free"}
	var customer, subscription sql.NullString
	var eventAt sql.NullTime
	err := d.db.QueryRow(`
		SELECT plan, stripe_customer_id, stripe_subscription_id, subscription_status, event_created_at
		FROM billing_accounts
		WHERE user_id = $1
	`, userID).Scan(&account.Plan, &customer, &subscription, &account.Status, &eventAt)
	if err == sql.ErrNoRows {
		return account, nil
	}
	if err != nil {
		return account, fmt.Errorf("billing account: %w", err)
	}
	account.CustomerID = customer.String
	account.SubscriptionID = subscription.String
	if eventAt.Valid {
		account.EventCreatedAt = eventAt.Time
	}
	if account.Plan == "" {
		account.Plan = "free"
	}
	return account, nil
}

func (d *Database) BillingUserByCustomer(customerID string) (string, error) {
	if customerID == "" {
		return "", nil
	}
	var userID string
	err := d.db.QueryRow(`
		SELECT user_id FROM billing_accounts WHERE stripe_customer_id = $1
	`, customerID).Scan(&userID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("billing user by customer: %w", err)
	}
	return userID, nil
}

func (d *Database) CountJobsByUser(userID string) (int, error) {
	var n int
	if err := d.db.QueryRow(`SELECT COUNT(*) FROM jobs WHERE user_id = $1`, userID).Scan(&n); err != nil {
		return 0, fmt.Errorf("count jobs: %w", err)
	}
	return n, nil
}

// WithStripeEvent runs fn once per Stripe event id. A repeated id is not an error.
// If fn fails, the id is removed so Stripe can retry.
func (d *Database) WithStripeEvent(eventID string, fn func() error) (bool, error) {
	if eventID == "" {
		return false, fmt.Errorf("stripe event id is required")
	}
	result, err := d.db.Exec(`
		INSERT INTO stripe_events (id, received_at) VALUES ($1, $2)
		ON CONFLICT (id) DO NOTHING
	`, eventID, time.Now().UTC())
	if err != nil {
		return false, fmt.Errorf("record stripe event: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("stripe event rows: %w", err)
	}
	if rows == 0 {
		return false, nil
	}
	if err := fn(); err != nil {
		_, _ = d.db.Exec(`DELETE FROM stripe_events WHERE id = $1`, eventID)
		return false, err
	}
	return true, nil
}

// BindStripeCustomer stores the Checkout customer without changing the plan.
func (d *Database) BindStripeCustomer(userID, customerID, subscriptionID string) error {
	user, err := d.UserByID(userID)
	if err != nil {
		return err
	}
	if user == nil {
		return nil
	}
	now := time.Now().UTC()
	_, err = d.db.Exec(`
		INSERT INTO billing_accounts (
			user_id, plan, stripe_customer_id, stripe_subscription_id, subscription_status, updated_at
		) VALUES ($1, 'free', NULLIF($2, ''), NULLIF($3, ''), '', $4)
		ON CONFLICT (user_id) DO UPDATE SET
			stripe_customer_id = COALESCE(EXCLUDED.stripe_customer_id, billing_accounts.stripe_customer_id),
			stripe_subscription_id = COALESCE(EXCLUDED.stripe_subscription_id, billing_accounts.stripe_subscription_id),
			updated_at = EXCLUDED.updated_at
	`, userID, customerID, subscriptionID, now)
	if err != nil {
		return fmt.Errorf("bind stripe customer: %w", err)
	}
	return nil
}

// ApplySubscription sets Free or Pro from a subscription event.
// An older event does not overwrite a newer one.
func (d *Database) ApplySubscription(userID, customerID, subscriptionID, status, priceID, paidPrice string, eventAt time.Time) error {
	if userID == "" || eventAt.IsZero() {
		return nil
	}
	user, err := d.UserByID(userID)
	if err != nil {
		return err
	}
	if user == nil {
		return nil
	}
	plan := "free"
	if (status == "active" || status == "trialing") && priceID != "" && priceID == paidPrice {
		plan = "pro"
	}
	now := time.Now().UTC()
	_, err = d.db.Exec(`
		INSERT INTO billing_accounts (
			user_id, plan, stripe_customer_id, stripe_subscription_id, subscription_status, event_created_at, updated_at
		) VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), $5, $6, $7)
		ON CONFLICT (user_id) DO UPDATE SET
			plan = EXCLUDED.plan,
			stripe_customer_id = COALESCE(EXCLUDED.stripe_customer_id, billing_accounts.stripe_customer_id),
			stripe_subscription_id = COALESCE(EXCLUDED.stripe_subscription_id, billing_accounts.stripe_subscription_id),
			subscription_status = EXCLUDED.subscription_status,
			event_created_at = EXCLUDED.event_created_at,
			updated_at = EXCLUDED.updated_at
		WHERE billing_accounts.event_created_at IS NULL
		   OR billing_accounts.event_created_at <= EXCLUDED.event_created_at
	`, userID, plan, customerID, subscriptionID, status, eventAt.UTC(), now)
	if err != nil {
		return fmt.Errorf("apply subscription: %w", err)
	}
	return nil
}
