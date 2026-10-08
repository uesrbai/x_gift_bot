package checkout

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"xgift/internal/vault"
)

var ErrManualLinkConflict = errors.New("existing order belongs to another username or plan")

// ManualLinkStageError identifies the operation that failed without exposing
// X cookies, Stripe credentials, card details or upstream response bodies.
// The underlying error remains available to errors.Is/As for safety checks.
type ManualLinkStageError struct {
	Stage string
	Cause error
}

func (e *ManualLinkStageError) Error() string { return e.Cause.Error() }
func (e *ManualLinkStageError) Unwrap() error { return e.Cause }

func manualLinkStage(stage string, err error) error {
	if err == nil { return nil }
	return &ManualLinkStageError{Stage: stage, Cause: err}
}

// ManualLinkFailureDetail returns only an allowlisted reason. Never send
// upstream error bodies, checkout session IDs, request IDs or card data to the
// browser. These codes distinguish a verified old-session refusal from a
// transport failure or a saved-order identity mismatch.
// ManualLinkStripeResponse reports fixed, non-secret Stripe diagnostics.
// Stripe's human error message, request URL, checkout ID, keys and payment
// credentials are intentionally never sent to the browser.
func ManualLinkStripeResponse(err error) (status int, typ string, code string) {
	var stripe *stripeError
	if !errors.As(err, &stripe) {
		return 0, "", ""
	}
	if stripe.HTTP >= 100 && stripe.HTTP <= 599 {
		status = stripe.HTTP
	}
	return status, safeErrorField(stripe.Type), safeErrorField(stripe.Code)
}

func ManualLinkFailureDetail(err error) string {
	if err == nil { return "" }
	switch {
	case errors.Is(err, ErrVerifyUnpaid):
		return "expired_unpaid_confirmation_required"
	case errors.Is(err, ErrPaymentActionRequired):
		return "bank_authentication_pending"
	case errors.Is(err, ErrXReadFailure):
		return "x_read_failed"
	case errors.Is(err, ErrManualLinkConflict):
		return "order_identity_or_plan_conflict"
	case errors.Is(err, ErrPaymentNodesCooling):
		return "payment_nodes_cooling"
	}
	var stripe *stripeError
	if errors.As(err, &stripe) {
		if stripe.Code == "checkout_not_active_session" { return "stripe_session_inactive" }
		switch stripe.HTTP {
		case 401, 403: return "stripe_authorization_rejected"
		case 429: return "stripe_rate_limited"
		}
		if stripe.HTTP >= 500 { return "stripe_upstream_unavailable" }
		return "stripe_checkout_request_rejected"
	}
	var transport *stripeTransportFailure
	if errors.As(err, &transport) {
		return "stripe_network_failure"
	}
	switch msg := err.Error(); {
	case strings.Contains(msg, "expired checkout is not conclusively unpaid"):
		return "expired_checkout_unverified_payment"
	case strings.Contains(msg, "Stripe merchant, session, recipient return URLs, currency or payment mode mismatch"):
		return "stripe_order_identity_mismatch"
	case strings.Contains(msg, "Stripe product, duration, quantity or unit amount mismatch"),
		strings.Contains(msg, "Stripe final total or line item count"):
		return "stripe_plan_or_amount_mismatch"
	case strings.Contains(msg, "an existing payment intent requires inspection"),
		strings.Contains(msg, "Stripe must explicitly return a null payment intent"):
		return "payment_intent_requires_verification"
	case strings.Contains(msg, "Stripe checkout is not open and unpaid"):
		return "stripe_checkout_not_open_unpaid"
	case strings.Contains(msg, "untrusted original checkout"):
		return "untrusted_original_session"
	case strings.Contains(msg, "recipient changed"):
		return "recipient_identity_changed"
	case strings.Contains(msg, "original payment outcome is unknown"):
		return "original_payment_state_unknown"
	case strings.Contains(msg, "bound order identity or price mismatch"):
		return "order_identity_or_plan_conflict"
	default:
		return "order_verification_failed"
	}
}
var ErrPaymentActionRequired = errors.New("bank authentication required")

// ManualLinkForUsername creates/reuses a guarded checkout without tokenizing or
// confirming a card. No redemption code is required. Caller holds checkout.lock.
func ManualLinkForUsername(ctx context.Context, v *vault.Vault, user string, port, months int, verifiedUnpaid bool) (*Record, error) {
	user, ok := NormalizeUsername(user)
	if !ok {
		return nil, errors.New("invalid username")
	}
	cat, err := ReadCatalog(v)
	if err != nil {
		return nil, manualLinkStage("catalog", err)
	}
	if _, err = cat.PlanFor(months); err != nil {
		return nil, manualLinkStage("catalog", err)
	}
	x, err := newXClient(v, port)
	if err != nil {
		return nil, manualLinkStage("x_auth", err)
	}
	defer x.close()
	recipient, err := x.identity(ctx, user, false)
	if err != nil {
		return nil, manualLinkStage("x_identity", err)
	}
	return manualLinkForRecipient(ctx, v, user, recipient, port, months, verifiedUnpaid)
}

func manualLinkForRecipient(ctx context.Context, v *vault.Vault, user, recipient string, port, months int, verifiedUnpaid bool) (*Record, error) {
	raw, err := v.Get("checkout:" + recipient)
	if errors.Is(err, sql.ErrNoRows) {
		record, createErr := RunForRecipient(ctx, v, user, recipient, false, port, months)
		return record, manualLinkStage("new_checkout", createErr)
	}
	if err != nil {
		return nil, manualLinkStage("checkout_read", err)
	}
	defer clear(raw)
	var r Record
	if err = json.Unmarshal(raw, &r); err != nil {
		return nil, manualLinkStage("checkout_record", err)
	}
	cat, err := ReadCatalog(v)
	if err != nil {
		return nil, manualLinkStage("catalog", err)
	}
	plan, err := cat.PlanFor(months)
	if err != nil {
		return nil, manualLinkStage("catalog", err)
	}
	if r.Username != user || r.RecipientID != recipient || r.Months != months || r.Amount != plan.Minor || r.Currency != strings.ToUpper(plan.Currency) || r.ProductID != plan.ProductID {
		return nil, ErrManualLinkConflict
	}
	if r.Status == "succeeded" {
		return &r, nil
	}
	if r.Status == "requires_action" {
		current, lookupErr := PrepareRecoveryLinkForRecipient(ctx, v, user, recipient, port, months, false)
		if lookupErr == nil || errors.Is(lookupErr, ErrPaymentActionRequired) {
			return current, nil
		}
		// Only a live Stripe confirmation of terminal cancellation can retire a
		// challenged payment. An operator checkbox cannot override this check.
		if err = RetireCanceledAuthentication(ctx, v, recipient); err != nil {
			return &r, manualLinkStage("bank_auth_reconciliation", err)
		}
		next, retryErr := RunForRecipient(ctx, v, user, recipient, false, port, months)
		return next, manualLinkStage("checkout_after_cancellation", retryErr)
	}
	current, verifyErr := PrepareRecoveryLinkForRecipient(ctx, v, user, recipient, port, months, verifiedUnpaid)
	return current, manualLinkStage("existing_checkout_verification", verifyErr)
}
