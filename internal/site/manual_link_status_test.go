package site

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"xgift/internal/checkout"
)

// Cloudflare may replace HTTP 502 from the application with an HTML error page.
// A rejected checkout must remain JSON with a non-gateway status so the admin
// can see the reason and distinguish verification from an origin outage.
func TestManualLinkUpstreamVerificationReturnsJSONNotGateway(t *testing.T) {
	o := failedUpstreamVerification()
	o.body["reason_code"] = "upstream_or_order_verification"
	o.body["order_check_required"] = true
	w := httptest.NewRecorder()
	o.write(w)
	if w.Code != http.StatusFailedDependency {
		t.Fatalf("verification failure HTTP status = %d; want 424", w.Code)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("unexpected Content-Type: %s", got)
	}
	var response struct {
		Message string `json:"message"`
		Reason string `json:"reason_code"`
		Check bool `json:"order_check_required"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Message == "" || response.Reason != "upstream_or_order_verification" || !response.Check {
		t.Fatalf("incomplete structured business failure: %+v", response)
	}
}

func TestManualLinkMissingValidatedCheckoutIsConflictNotGateway(t *testing.T) {
	s := &server{}
	result := s.linkResult(&checkout.Record{Username: "example", Months: 6, Amount: 60000, Currency: "BDT", Status: "created"}, "")
	if result.status != http.StatusConflict {
		t.Fatalf("missing verified checkout status = %d, want 409", result.status)
	}
	if result.body["reason_code"] != "link_missing" || result.body["order_check_required"] != true {
		t.Fatalf("missing safe follow-up reason: %+v", result.body)
	}
}

func TestManualLinkUnverifiedDoesNotExposeLink(t *testing.T) {
	s := &server{}
	out := s.linkResult(&checkout.Record{Username: "example", Months: 6, Amount: 60000, Currency: "BDT", Status: "unknown", URL: "https://checkout.stripe.com/a/pay/cs_live_Test", SessionID: "cs_live_Test"}, "")
	if out.status != http.StatusConflict {
		t.Fatalf("unknown result should be blocked, got %d", out.status)
	}
	if _, ok := out.body["checkout_url"]; ok {
		t.Fatal("must not expose an unverified checkout link")
	}
}
