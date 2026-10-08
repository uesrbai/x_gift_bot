package checkout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"xgift/internal/vault"
)

var ErrVerifyUnpaid = errors.New("inactive checkout requires operator verification that the original order is unpaid")

// CheckoutLink validates before exposing a persisted link to the admin UI.
func CheckoutLink(r *Record) string {
	if r != nil && !r.LinkBlocked && (r.Status == "created" || r.Status == "requires_action") && sessionURL(r.URL, r.SessionID) {
		return r.URL
	}
	return ""
}
func inactiveCheckout(err error) bool {
	var e *stripeError
	return errors.As(err, &e) && e.Code == "checkout_not_active_session"
}

// readExistingCheckoutPage checks the recorded Stripe session, not a new one.
// Some inactive sessions reject POST /init with a non-specific 400/404/410/422
// instead of checkout_not_active_session. In that case, try a READ-ONLY GET
// before declaring the old payment unknown. The caller still performs the
// full merchant/recipient/amount/intent checks before using any returned data.
// Never retry authentication failures, rate limits, payment submissions or
// an ambiguous successful Stripe result through this path.
// ExistingCheckoutLookupError records only allowlisted Stripe response fields
// for *both* attempts to inspect an already saved session. Neither a missing
// resource nor an init failure proves the payment did not happen.
type ExistingCheckoutLookupError struct {
	InitHTTP, ReadHTTP int
	InitType, InitCode string
	ReadType, ReadCode string
	Cause error
}

func (e *ExistingCheckoutLookupError) Error() string {
	return "saved Stripe checkout unavailable from both initialization and read-only lookup; payment state remains unknown"
}
func (e *ExistingCheckoutLookupError) Unwrap() error { return e.Cause }

func checkoutLookupFields(err error) (status int, typ, code string) {
	var se *stripeError
	if !errors.As(err, &se) { return 0, "", "" }
	return se.HTTP, safeErrorField(se.Type), safeErrorField(se.Code)
}

func readExistingCheckoutPage(ctx context.Context, s *stripeClient, r *Record) (*paymentPage, error) {
	page, err := s.page(ctx, r, true)
	if err == nil {
		return page, nil
	}
	var upstream *stripeError
	if !errors.As(err, &upstream) {
		return page, err
	}
	switch upstream.HTTP {
	case 400, 404, 410, 422:
		snapshot, lookupErr := s.page(ctx, r, false)
		if lookupErr == nil {
			return snapshot, nil
		}
		initHTTP, initType, initCode := checkoutLookupFields(err)
		readHTTP, readType, readCode := checkoutLookupFields(lookupErr)
		// Never infer an unpaid session from 404. Two errors provide a safe
		// diagnostic only; the caller keeps the order blocked as before.
		return page, &ExistingCheckoutLookupError{
			InitHTTP: initHTTP, InitType: initType, InitCode: initCode,
			ReadHTTP: readHTTP, ReadType: readType, ReadCode: readCode,
			Cause: err,
		}
	default:
		return page, err
	}
}

// expiredUnsubmittedCheckout requires explicit Stripe evidence that an
// untouched one-time checkout expired without a payment intent or charge.
// This is not, by itself, permission to create a replacement: the operator
// must still affirm no charge, and the normal replacement creation checks
// and archival of the original order remain mandatory.
func expiredUnsubmittedCheckout(r *Record, page *paymentPage, plan Plan) bool {
	return r != nil && page != nil && r.Status == "created" &&
		unsubmitted(r) && page.Status == "expired" &&
		page.PaymentStatus == "unpaid" && page.IntentPresent &&
		page.IntentNull && page.Intent == nil &&
		page.Total.Due == plan.Minor && page.Group.Due == plan.Minor
}

// PrepareRecoveryLinkForRecipient can create a checkout, but NEVER tokenizes or
// confirms a card. Caller holds checkout.lock. Replacement is opt-in and audited.
func PrepareRecoveryLinkForRecipient(ctx context.Context, v *vault.Vault, user, recipient string, port, months int, verifiedUnpaid bool) (*Record, error) {
	raw, err := v.Get("checkout:" + recipient)
	if err != nil {
		return nil, err
	}
	defer clear(raw)
	var r Record
	if err = json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	cat, err := ReadCatalog(v)
	if err != nil {
		return nil, err
	}
	plan, err := cat.PlanFor(months)
	if err != nil {
		return nil, err
	}
	if r.Username != user || r.RecipientID != recipient || r.Months != months || r.Amount != plan.Minor || r.Currency != strings.ToUpper(plan.Currency) || r.ProductID != plan.ProductID {
		return nil, errors.New("bound order identity or price mismatch")
	}
	if r.Status == "succeeded" {
		return &r, nil
	}
	if ManualRetryBlocked(&r) {
		return &r, errors.New("payment provider forbids retry with this card")
	}
	if r.Status == "creating" && unsubmitted(&r) {
		return RunForRecipient(ctx, v, user, recipient, false, port, months)
	}
	if !sessionURL(r.URL, r.SessionID) {
		return &r, errors.New("untrusted original checkout")
	}
	s, err := newStripe(ctx, v, r.RecipientID, paymentRead)
	if err != nil {
		return &r, err
	}
	defer s.close()
	eligible := func() error {
		x, e := newXClient(v, port)
		if e != nil {
			return e
		}
		defer x.close()
		id, e := x.recipient(ctx, user)
		if e != nil {
			return e
		}
		if id != recipient {
			return errors.New("recipient changed")
		}
		return x.quote(ctx, user, plan)
	}
	create := func() (string, string, error) {
		x, e := newXClient(v, port)
		if e != nil {
			return "", "", e
		}
		defer x.close()
		return x.create(ctx, user, recipient, plan)
	}
	return prepareRecoveryLink(ctx, v, &r, s, plan, verifiedUnpaid, eligible, create)
}
func prepareRecoveryLink(ctx context.Context, v *vault.Vault, r *Record, s *stripeClient, plan Plan, verified bool, eligible func() error, create func() (string, string, error)) (*Record, error) {
	r.LinkBlocked = true
	if err := save(v, r); err != nil {
		return r, err
	}
	var lookupErr error
	explicitExpiredUnpaid := false
	if unsubmitted(r) {
		page, err := readExistingCheckoutPage(ctx, s, r)
		lookupErr = err
		if err == nil {
			if err = page.guard(r, plan, false); err != nil {
				return r, err
			}
			if page.Status == "complete" && page.PaymentStatus == "paid" {
				r.Status = "succeeded"
				r.LastError = nil
				return r, save(v, r)
			}
			if page.Status == "expired" {
				// Stripe can return a valid *expired* checkout page instead of
				// checkout_not_active_session. The previous open-only guard
				// rejected it before the verified-unpaid replacement protocol.
				// Never treat an expired checkout with a PaymentIntent,
				// payment submission, or ambiguous evidence as safe.
				if !expiredUnsubmittedCheckout(r, page, plan) {
					return r, errors.New("expired checkout is not conclusively unpaid")
				}
				explicitExpiredUnpaid = true
			} else {
				if err = page.guard(r, plan, true); err != nil {
					return r, err
				}
			}
			if !explicitExpiredUnpaid {
				if err = eligible(); err != nil {
					return r, err
				}
				if err = rememberVerifiedCheckout(v, r, plan, page); err != nil {
					return r, err
				}
				r.LinkBlocked = false
				if err = save(v, r); err != nil {
					return r, err
				}
				return r, holdPublicCheckout(v, r, plan, time.Now())
			}
		}
	} else {
		if err := verifySubmission(v, r, plan); err != nil {
			return r, err
		}
		state, err := s.poll(ctx, r, plan)
		lookupErr = err
		if err == nil {
			if state == "succeeded" {
				r.Status = "succeeded"
				r.LastError = nil
				return r, save(v, r)
			}
			if state == "requires_action" {
				r.Status = state
				if err = save(v, r); err != nil {
					return r, err
				}
				if err = markAuthenticationRequired(v, r); err != nil {
					return r, err
				}
				return r, ErrPaymentActionRequired
			}
			if !IsPaymentDeclined(r) {
				return r, errors.New("original payment outcome is unknown")
			}
			if err = eligible(); err != nil {
				return r, err
			}
			_, _, err = s.manualPreflight(ctx, r, plan)
			if err == nil {
				return r, nil
			}
			lookupErr = err
		}
	}
	if !explicitExpiredUnpaid && !inactiveCheckout(lookupErr) {
		return r, lookupErr
	}
	if !verified {
		return r, ErrVerifyUnpaid
	}
	if err := replacementEvidence(v, r); err != nil {
		return r, err
	}
	if err := eligible(); err != nil {
		return r, err
	}
	return replaceRecoveryLink(ctx, v, r, s, plan, create)
}

// Inactivity alone is not proof of no charge. The admin must attest to checking
// the original payment, and submitted records must also have a definite decline
// and the original validated failed poll. Unknown outcomes are never replaced.
func replacementEvidence(v *vault.Vault, r *Record) error {
	if r.Status == "succeeded" || r.Status == "requires_action" {
		return errors.New("original payment is not replaceable")
	}
	if unsubmitted(r) && r.Status == "created" {
		return nil
	}
	if !IsPaymentDeclined(r) || r.LastError == nil || r.LastError.Code != "card_declined" || r.LastError.Replayed || ManualRetryBlocked(r) {
		return errors.New("original payment lacks a definite retryable decline")
	}
	b, err := v.Get("stripe-result:" + r.SessionID)
	if err != nil {
		return err
	}
	defer clear(b)
	var p struct {
		Session    string `json:"session_id"`
		Live       bool   `json:"livemode"`
		Sandbox    *bool  `json:"is_sandbox_merchant"`
		Mode       string `json:"mode"`
		State      string `json:"state"`
		Payment    string `json:"payment_object_status"`
		SuccessURL string `json:"success_url"`
	}
	if json.Unmarshal(b, &p) != nil || p.Session != r.SessionID || !p.Live || p.Sandbox == nil || *p.Sandbox || p.Mode != "payment" || p.State != "active" || p.Payment != "requires_payment_method" || p.SuccessURL != "https://x.com/"+r.Username+"/gift-premium/success" {
		return errors.New("original failed payment evidence mismatch")
	}
	return nil
}
func replaceRecoveryLink(ctx context.Context, v *vault.Vault, r *Record, s *stripeClient, plan Plan, create func() (string, string, error)) (*Record, error) {
	old, err := json.Marshal(r)
	if err != nil {
		return r, err
	}
	defer clear(old)
	// Exact current vault bytes are used for compare-and-swap, while the decoded
	// identity and submission evidence have already been checked under the lock.
	expected, err := v.Get("checkout:" + r.RecipientID)
	if err != nil {
		return r, err
	}
	defer clear(expected)
	var current Record
	if json.Unmarshal(expected, &current) != nil {
		return r, errors.New("invalid original checkout")
	}
	decoded, _ := json.Marshal(&current)
	defer clear(decoded)
	if string(decoded) != string(old) {
		return r, errors.New("original checkout changed")
	}
	gate := &xClient{vault: v, readCheckoutPaid: s.verifiedCheckoutPaid, readCheckout: func(ctx context.Context, active *Record) (*paymentPage, error) { return s.page(ctx, active, true) }}
	if err := gate.checkCreation(ctx, time.Now()); err != nil {
		return r, err
	}
	next := Record{Username: r.Username, RecipientID: r.RecipientID, Months: r.Months, Amount: r.Amount, Currency: r.Currency, ProductID: r.ProductID, Status: "creating", Created: time.Now().Unix(), ReplacementCount: r.ReplacementCount + 1, PreviousSession: r.SessionID}
	fresh, err := json.Marshal(&next)
	if err != nil {
		return r, err
	}
	defer clear(fresh)
	failedPoll, _ := v.Get("stripe-result:" + r.SessionID)
	defer clear(failedPoll)
	proof, err := json.Marshal(map[string]any{"original": json.RawMessage(old), "failed_poll": json.RawMessage(failedPoll), "operator_verified_unpaid": true, "inactive_checked_at": time.Now().Unix(), "replacement_number": next.ReplacementCount})
	if err != nil {
		return r, err
	}
	defer clear(proof)
	if err = v.ReplaceArchived("checkout:"+r.RecipientID, "replacement-original:"+r.SessionID, expected, proof, fresh); err != nil {
		return r, err
	}
	next.SessionID, next.URL, err = create()
	if err != nil {
		return &next, err
	}
	if next.SessionID == r.SessionID || !sessionURL(next.URL, next.SessionID) {
		return &next, errors.New("upstream did not create a distinct trusted checkout")
	}
	next.Created = time.Now().Unix()
	next.Status = "created"
	if err = save(v, &next); err != nil {
		return &next, err
	}
	page, err := s.page(ctx, &next, true)
	if err != nil {
		return &next, err
	}
	if err = page.guard(&next, plan, true); err != nil {
		return &next, err
	}
	if err = rememberVerifiedCheckout(v, &next, plan, page); err != nil {
		return &next, err
	}
	return &next, holdPublicCheckout(v, &next, plan, time.Now())
}

func RecoverWithNewLink(ctx context.Context, v *vault.Vault, user, recipient string, port, months int, verifiedUnpaid, linksOnly bool) (*Record, error) {
	r, err := PrepareRecoveryLinkForRecipient(ctx, v, user, recipient, port, months, verifiedUnpaid)
	if err != nil || r == nil || r.Status == "succeeded" || linksOnly {
		return r, err
	}
	if IsPaymentDeclined(r) {
		return ManualRecoverForRecipient(ctx, v, user, recipient, port, months)
	}
	return ResumeForRecipient(ctx, v, user, recipient, port, months)
}

func replacementMessage(err error) string {
	if errors.Is(err, ErrVerifyUnpaid) {
		return "原链接已失效；请先核对原订单未扣款，再勾选允许生成新链接。"
	}
	return fmt.Sprint(err)
}
