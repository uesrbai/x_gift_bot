package checkout

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestExpiredUnsubmittedCheckoutRequiresExplicitEvidence(t *testing.T) {
	plan := Plan{Months: 6, Minor: 60000, Currency: "usd", ProductID: "prod_Test", Merchant: "acct_Test"}
	base := Record{
		Username: "recipient", RecipientID: "1234", Months: 6, Amount: 60000,
		Currency: "USD", ProductID: "prod_Test", SessionID: "cs_live_Test",
		URL: "https://checkout.stripe.com/a/pay/cs_live_Test", Status: "created",
	}
	original := publicPageFixture(&base, plan)
	original.Status = "expired"
	if !expiredUnsubmittedCheckout(&base, original, plan) {
		t.Fatal("Stripe-confirmed expired, unpaid checkout with null intent should be eligible for operator confirmation")
	}
	for _, tc := range []struct {
		name string
		change func(*Record, *paymentPage)
	}{
		{"old submitted", func(r *Record, _ *paymentPage) { r.SubmittedAt = 1 }},
		{"old method", func(r *Record, _ *paymentPage) { r.PaymentMethod = "pm_123" }},
		{"unknown old state", func(r *Record, _ *paymentPage) { r.Status = "submitting" }},
		{"still open", func(_ *Record, p *paymentPage) { p.Status = "open" }},
		{"status complete", func(_ *Record, p *paymentPage) { p.Status = "complete" }},
		{"payment processing", func(_ *Record, p *paymentPage) { p.PaymentStatus = "unpaid"; p.IntentPresent = true; p.IntentNull = false }},
		{"paid", func(_ *Record, p *paymentPage) { p.PaymentStatus = "paid" }},
		{"intent absent", func(_ *Record, p *paymentPage) { p.IntentPresent = false }},
		{"intent not null", func(_ *Record, p *paymentPage) { p.IntentNull = false }},
		{"unknown remaining amount", func(_ *Record, p *paymentPage) { p.Total.Due = 0 }},
		{"unknown line balance", func(_ *Record, p *paymentPage) { p.Group.Due = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, p := base, *original
			tc.change(&r, &p)
			if expiredUnsubmittedCheckout(&r, &p, plan) {
				t.Fatal("ambiguous or submitted checkout must not permit replacement")
			}
		})
	}
}

func TestExpiredUnpaidRecoveryRequiresOperatorConfirmation(t *testing.T) {
	v := controlFixture(t)
	r := &Record{
		Username: "recipient", RecipientID: "1234", Months: 6,
		Amount: 60000, Currency: "USD", ProductID: "prod_Test",
		Status: "created", SessionID: "cs_live_Test",
		URL: "https://checkout.stripe.com/a/pay/cs_live_Test", Created: 42,
	}
	if err := save(v, r); err != nil { t.Fatal(err) }
	plan := Plan{Months: 6, Minor: 60000, Currency: "usd", ProductID: "prod_Test", Merchant: "acct_Test"}
	page := publicPageFixture(r, plan)
	raw := []byte(strings.Replace(string(page.raw), `"status":"open"`, `"status":"expired"`, 1))
	requests := 0
	s := &stripeClient{vault: v, key: "pk_live_Test", http: &http.Client{Transport: stripeRoundTrip(func(req *http.Request) (*http.Response, error) {
		requests++
		if !strings.HasSuffix(req.URL.Path, "/init") {
			t.Fatalf("unexpected Stripe operation: %s", req.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
	})}}
	eligibilityCalled := false
	createCalled := false
	eligible := func() error { eligibilityCalled = true; return nil }
	create := func() (string, string, error) { createCalled = true; return "", "", nil }
	out, err := prepareRecoveryLink(context.Background(), v, r, s, plan, false, eligible, create)
	if !errors.Is(err, ErrVerifyUnpaid) {
		t.Fatalf("expired checkout must request operator confirmation, got %v", err)
	}
	if out == nil || requests != 1 || eligibilityCalled || createCalled {
		t.Fatalf("unconfirmed expired checkout caused a side effect: requests=%d eligible=%t created=%t", requests, eligibilityCalled, createCalled)
	}
	stored, err := v.Get("checkout:1234")
	if err != nil { t.Fatal(err) }
	var current Record
	if err = json.Unmarshal(stored, &current); err != nil { t.Fatal(err) }
	clear(stored)
	if current.Status != "created" || !current.LinkBlocked || current.SessionID != r.SessionID || current.SubmittedAt != 0 {
		t.Fatal("unconfirmed expired order was changed or exposed")
	}
}

func TestExpiredIntentNeverAutomaticallyReplaced(t *testing.T) {
	v := controlFixture(t)
	r := &Record{
		Username: "recipient", RecipientID: "1234", Months: 6,
		Amount: 60000, Currency: "USD", ProductID: "prod_Test",
		Status: "created", SessionID: "cs_live_Test",
		URL: "https://checkout.stripe.com/a/pay/cs_live_Test",
	}
	if err := save(v, r); err != nil { t.Fatal(err) }
	plan := Plan{Months: 6, Minor: 60000, Currency: "usd", ProductID: "prod_Test", Merchant: "acct_Test"}
	page := publicPageFixture(r, plan)
	raw := strings.Replace(string(page.raw), `"status":"open"`, `"status":"expired"`, 1)
	raw = strings.Replace(raw, `"payment_intent":null`, `"payment_intent":{"id":"pi_test","status":"processing","amount":60000,"currency":"usd"}`, 1)
	s := &stripeClient{vault: v, key: "pk_live_Test", http: &http.Client{Transport: stripeRoundTrip(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(raw))}, nil
	})}}
	called := false
	_, err := prepareRecoveryLink(context.Background(), v, r, s, plan, true,
		func() error { called = true; return nil },
		func() (string, string, error) { called = true; return "", "", nil })
	if err == nil || called {
		t.Fatal("expired checkout with a payment intent must never be replaced based on checkbox")
	}
}

func TestManualLinkFailureDetailIsAllowlisted(t *testing.T) {
	cases := []struct {
		err error
		want string
	}{
		{ErrVerifyUnpaid, "expired_unpaid_confirmation_required"},
		{&stripeError{Code: "checkout_not_active_session", HTTP: 400}, "stripe_session_inactive"},
		{&stripeError{HTTP: 403}, "stripe_authorization_rejected"},
		{&stripeError{HTTP: 500}, "stripe_upstream_unavailable"},
		{&stripeTransportFailure{}, "stripe_network_failure"},
		{errors.New("Stripe checkout is not open and unpaid at the exact authorized amount"), "stripe_checkout_not_open_unpaid"},
		{errors.New("expired checkout is not conclusively unpaid"), "expired_checkout_unverified_payment"},
		{errors.New("credentials: auth_token=not-for-user"), "order_verification_failed"},
	}
	for _, tc := range cases {
		if actual := ManualLinkFailureDetail(manualLinkStage("existing_checkout_verification", tc.err)); actual != tc.want {
			t.Fatalf("failure code %v: %s, want %s", tc.err, actual, tc.want)
		}
	}
}
