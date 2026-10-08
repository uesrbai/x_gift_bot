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

func rejectedStripeInitResponse() *http.Response {
	return &http.Response{
		StatusCode: 400,
		Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{"error":{"type":"invalid_request_error","code":"resource_missing","message":"This checkout cannot be initialized"}}`)),
	}
}

func TestOldCheckoutInitRejectionUsesReadOnlyGET(t *testing.T) {
	v := controlFixture(t)
	r := &Record{
		Username: "recipient", RecipientID: "1234", Months: 6, Amount: 60000,
		Currency: "USD", ProductID: "prod_Test", Status: "created",
		SessionID: "cs_live_Test", URL: "https://checkout.stripe.com/a/pay/cs_live_Test",
	}
	plan := Plan{Months: 6, Minor: 60000, Currency: "usd", ProductID: "prod_Test", Merchant: "acct_Test"}
	snapshot := publicPageFixture(r, plan)
	expired := strings.Replace(string(snapshot.raw), `"status":"open"`, `"status":"expired"`, 1)
	postCalls, getCalls := 0, 0
	client := &stripeClient{vault: v, key: "pk_live_Test", http: &http.Client{Transport: stripeRoundTrip(func(req *http.Request) (*http.Response, error) {
		switch req.Method {
		case http.MethodPost:
			postCalls++
			if !strings.HasSuffix(req.URL.Path, "/init") {
				t.Fatalf("unexpected write: %s", req.URL.Path)
			}
			return rejectedStripeInitResponse(), nil
		case http.MethodGet:
			getCalls++
			if !strings.HasSuffix(req.URL.Path, "/"+r.SessionID) {
				t.Fatalf("GET queried wrong checkout: %s", req.URL.Path)
			}
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(expired))}, nil
		default:
			t.Fatalf("unexpected HTTP method: %s", req.Method)
		}
		return nil, errors.New("unreachable")
	})}}
	page, err := readExistingCheckoutPage(context.Background(), client, r)
	if err != nil || page == nil || page.Status != "expired" || postCalls != 1 || getCalls != 1 {
		t.Fatalf("read-only recovery failed: page=%+v err=%v post=%d get=%d", page, err, postCalls, getCalls)
	}
	if !expiredUnsubmittedCheckout(r, page, plan) {
		t.Fatal("validated expired/unpaid checkout was not recognized")
	}
}

func TestOldCheckoutReadFallbackRequiresOperatorConfirmation(t *testing.T) {
	v := controlFixture(t)
	r := &Record{
		Username: "recipient", RecipientID: "1234", Months: 6, Amount: 60000,
		Currency: "USD", ProductID: "prod_Test", Status: "created", Created: 123,
		SessionID: "cs_live_Test", URL: "https://checkout.stripe.com/a/pay/cs_live_Test",
	}
	if err := save(v, r); err != nil { t.Fatal(err) }
	plan := Plan{Months: 6, Minor: 60000, Currency: "usd", ProductID: "prod_Test", Merchant: "acct_Test"}
	page := publicPageFixture(r, plan)
	expired := strings.Replace(string(page.raw), `"status":"open"`, `"status":"expired"`, 1)
	s := &stripeClient{vault: v, key: "pk_live_Test", http: &http.Client{Transport: stripeRoundTrip(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodPost {
			return rejectedStripeInitResponse(), nil
		}
		if req.Method != http.MethodGet {
			t.Fatalf("unexpected write %s", req.Method)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(expired))}, nil
	})}}
	created := false
	_, err := prepareRecoveryLink(context.Background(), v, r, s, plan, false,
		func() error { t.Fatal("eligibility should not be checked until operator confirmation"); return nil },
		func() (string, string, error) { created = true; return "", "", nil })
	if !errors.Is(err, ErrVerifyUnpaid) || created {
		t.Fatalf("the expired link must remain blocked without operator confirmation: %v created=%v", err, created)
	}
	previous, err := v.Get("checkout:1234")
	if err != nil { t.Fatal(err) }
	defer clear(previous)
	var persisted Record
	if err = json.Unmarshal(previous, &persisted); err != nil { t.Fatal(err) }
	if !persisted.LinkBlocked || persisted.SubmittedAt != 0 || persisted.SessionID != "cs_live_Test" {
		t.Fatalf("unsafe order mutation: %+v", persisted)
	}
}

func TestOldCheckoutFallbackNeverRetriesSensitiveStripeStatuses(t *testing.T) {
	for _, status := range []int{401, 403, 429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			r := &Record{SessionID:"cs_live_Test"}
			reads := 0
			v := controlFixture(t)
			s := &stripeClient{vault:v, key:"pk_live_Test", http:&http.Client{Transport: stripeRoundTrip(func(req *http.Request) (*http.Response,error) {
				if req.Method == http.MethodGet { reads++ }
				return &http.Response{
					StatusCode:status,
					Header:http.Header{},
					Body:io.NopCloser(strings.NewReader(`{"error":{"type":"invalid_request_error","code":"test_failure"}}`)),
				},nil
			})}}
			if _, err := readExistingCheckoutPage(context.Background(), s, r); err == nil {
				t.Fatal("rejected init was accepted")
			}
			if reads != 0 {
				t.Fatalf("unexpected fallback GET after HTTP %d", status)
			}
		})
	}
}

func TestManualLinkStripeResponseOnlySafeFields(t *testing.T) {
	wrapped := manualLinkStage("existing_checkout_verification", &stripeError{
		HTTP: 400, Type: "invalid_request_error", Code:"resource_missing",
		Message:"payment secret: pi_test_secret_sensitive", RequestID:"req_sensitive",
	})
	status, typ, code := ManualLinkStripeResponse(wrapped)
	if status != 400 || typ != "invalid_request_error" || code != "resource_missing" {
		t.Fatalf("safe Stripe error extraction failed: %d %s %s", status, typ, code)
	}
	status, typ, code = ManualLinkStripeResponse(errors.New("not a Stripe request"))
	if status != 0 || typ != "" || code != "" {
		t.Fatal("non-Stripe errors leaked or were mislabeled")
	}
}
