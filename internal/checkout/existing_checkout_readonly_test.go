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

func TestMissingOldStripeSessionDoesNotCreateReplacement(t *testing.T) {
	v := controlFixture(t)
	r := &Record{
		Username: "recipient", RecipientID: "1234", Months: 6, Amount: 60000,
		Currency: "USD", ProductID: "prod_Test", Status: "created", Created: 123,
		SessionID: "cs_live_Test", URL: "https://checkout.stripe.com/a/pay/cs_live_Test",
	}
	if err := save(v, r); err != nil { t.Fatal(err) }
	plan := Plan{Months: 6, Minor: 60000, Currency: "usd", ProductID: "prod_Test", Merchant: "acct_Test"}
	postCount, getCount := 0, 0
	s := &stripeClient{vault: v, key: "pk_live_Test", http: &http.Client{Transport: stripeRoundTrip(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodPost { postCount++ }
		if req.Method == http.MethodGet { getCount++ }
		return &http.Response{
			StatusCode: 404, Header: http.Header{},
			Body: io.NopCloser(strings.NewReader(`{"error":{"type":"invalid_request_error","code":"resource_missing","message":"No such checkout session: cs_live_sensitive"}}`)),
		}, nil
	})}}
	created := false
	_, err := prepareRecoveryLink(context.Background(), v, r, s, plan, true,
		func() error { t.Fatal("unverified old session must not check replacement eligibility"); return nil },
		func() (string, string, error) { created = true; return "", "", nil })
	var lookup *ExistingCheckoutLookupError
	if !errors.As(err, &lookup) {
		t.Fatalf("expected combined Stripe lookup error, got %v", err)
	}
	if postCount != 1 || getCount != 1 || created {
		t.Fatalf("old checkout replaced without payment evidence: post=%d get=%d created=%v", postCount, getCount, created)
	}
	if code := ManualLinkFailureDetail(err); code != "saved_stripe_session_not_accessible" {
		t.Fatalf("incorrect missing-session code: %s", code)
	}
	if status, typ, code := ManualLinkReadOnlyResponse(err); status != 404 || typ != "invalid_request_error" || code != "resource_missing" {
		t.Fatalf("read-only rejection lost: status=%d type=%s code=%s", status, typ, code)
	}
	initHTTP, _, initCode := ManualLinkStripeResponse(err)
	if initHTTP != 404 || initCode != "resource_missing" {
		t.Fatalf("initialization rejection lost: %d %s", initHTTP, initCode)
	}
	stored, getErr := v.Get("checkout:1234")
	if getErr != nil { t.Fatal(getErr) }
	defer clear(stored)
	var current Record
	if err := json.Unmarshal(stored, &current); err != nil { t.Fatal(err) }
	if current.SessionID != r.SessionID || current.Status != "created" || current.SubmittedAt != 0 || !current.LinkBlocked {
		t.Fatalf("old payment evidence changed or exposed: %+v", current)
	}
}

func TestReadOnlyLookupDifferentFailureIsNotAssumedUnpaid(t *testing.T) {
	v := controlFixture(t)
	r := &Record{SessionID:"cs_live_Test"}
	client := &stripeClient{vault:v,key:"pk_live_Test",http:&http.Client{Transport:stripeRoundTrip(func(req *http.Request)(*http.Response,error){
		code, body := 404, `{"error":{"type":"invalid_request_error","code":"resource_missing"}}`
		if req.Method == http.MethodGet {
			code,body = 503,`{"error":{"type":"api_error","code":"api_unavailable"}}`
		}
		return &http.Response{StatusCode:code,Header:http.Header{},Body:io.NopCloser(strings.NewReader(body))},nil
	})}}
	_,err:=readExistingCheckoutPage(context.Background(),client,r)
	if got:=ManualLinkFailureDetail(err);got!="old_session_readonly_lookup_failed" {
		t.Fatalf("read-only unavailable not distinguished: %s",got)
	}
	getStatus,_,getCode:=ManualLinkReadOnlyResponse(err)
	if getStatus!=503||getCode!="api_unavailable" {t.Fatalf("lost second error: %d %s",getStatus,getCode)}
}
