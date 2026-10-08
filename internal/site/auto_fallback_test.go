package site

import (
	"testing"

	"xgift/internal/checkout"
)

func fallbackFixture() (*checkout.Record, *checkout.Record) {
	before := &checkout.Record{
		Username: "example", RecipientID: "12345", Months: 6,
		Amount: 60000, Currency: "BDT", ProductID: "prod_Test",
		SessionID: "cs_live_Example", URL: "https://checkout.stripe.com/a/pay/cs_live_Example",
		Status: "created",
	}
	after := *before
	return before, &after
}

func TestSafeAutoFallback(t *testing.T) {
	before, after := fallbackFixture()
	if !safeAutoFallback(before, after) {
		t.Fatal("matching unsubmitted checkout should be eligible for fresh verification")
	}
	cases := []struct {
		name string
		edit func(*checkout.Record)
	}{
		{"submitted", func(r *checkout.Record) { r.SubmittedAt = 123 }},
		{"tokenized", func(r *checkout.Record) { r.PaymentMethod = "pm_test" }},
		{"confirmed", func(r *checkout.Record) { r.ConfirmKey = "key" }},
		{"preflight", func(r *checkout.Record) { r.PreflightSaved = true }},
		{"different recipient", func(r *checkout.Record) { r.RecipientID = "999" }},
		{"different amount", func(r *checkout.Record) { r.Amount++ }},
		{"unknown", func(r *checkout.Record) { r.Status = "unknown" }},
		{"requires action", func(r *checkout.Record) { r.Status = "requires_action" }},
		{"declined", func(r *checkout.Record) { r.Status = "declined" }},
		{"succeeded", func(r *checkout.Record) { r.Status = "succeeded" }},
		{"untrusted replacement", func(r *checkout.Record) { r.SessionID = "cs_live_Another" }},
		{"missing session", func(r *checkout.Record) { r.URL = "" }},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, altered := fallbackFixture()
			tt.edit(altered)
			if safeAutoFallback(before, altered) {
				t.Fatal("unsafe checkout must never automatically enter manual fallback")
			}
		})
	}
	_, creating := fallbackFixture()
	creating.Status, creating.URL, creating.SessionID = "creating", "", ""
	if !safeAutoFallback(creating, after) {
		t.Fatal("created session without any submission remains eligible for verification")
	}
	_, replacement := fallbackFixture()
	replacement.SessionID = "cs_live_New"
	replacement.PreviousSession = before.SessionID
	replacement.ReplacementCount = before.ReplacementCount + 1
	if !safeAutoFallback(before, replacement) {
		t.Fatal("archived verified-unpaid replacement can be verified again")
	}
	before.SubmittedAt = 456
	if safeAutoFallback(before, after) {
		t.Fatal("previously submitted payments must never trigger fallback")
	}
}
