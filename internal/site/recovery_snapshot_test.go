package site

import "testing"

func TestRecoverySnapshotMatchesDurableOrder(t *testing.T) {
	p := recoveryItem{
		ID: "order-a", Username: "recipient", Recipient: "recipient-id",
		Digest: "vault-hash", State: "pending", Months: 6,
		Amount: 60000, Currency: "BDT", Hint: "A4",
		CheckoutURL: "https://checkout.stripe.com/a/pay/cs_live_OLD",
		PaymentNode: "node-1", NeedsUnpaidVerification: true,
	}
	if !recoverySnapshotMatches(p, p) {
		t.Fatal("unchanged order must match")
	}
	// Display metadata may be refreshed independently of the checkout.
	changedDisplay := p
	changedDisplay.CheckoutURL = ""
	changedDisplay.PaymentNode = "node-2"
	changedDisplay.Detail = "实时节点状态已更新"
	if !recoverySnapshotMatches(p, changedDisplay) {
		t.Fatal("changing only display metadata should not block the recovery start")
	}
	replacements := []struct {
		name string
		edit func(*recoveryItem)
	}{
		{"checkout bytes", func(x *recoveryItem) { x.Digest = "changed" }},
		{"missing checkout digest", func(x *recoveryItem) { x.Digest = "" }},
		{"recipient", func(x *recoveryItem) { x.Recipient = "other" }},
		{"account", func(x *recoveryItem) { x.Username = "other" }},
		{"order id", func(x *recoveryItem) { x.ID = "other" }},
		{"plan", func(x *recoveryItem) { x.Months = 3 }},
		{"amount", func(x *recoveryItem) { x.Amount++ }},
		{"currency", func(x *recoveryItem) { x.Currency = "USD" }},
		{"status", func(x *recoveryItem) { x.State = "skipped" }},
		{"unpaid verification", func(x *recoveryItem) { x.NeedsUnpaidVerification = false }},
		{"card hint", func(x *recoveryItem) { x.Hint = "OTHER" }},
	}
	for _, tt := range replacements {
		t.Run(tt.name, func(t *testing.T) {
			changed := p
			tt.edit(&changed)
			if recoverySnapshotMatches(p, changed) {
				t.Fatal("payment-sensitive change must invalidate the preview")
			}
		})
	}
	previewNotPending := p
	previewNotPending.State = "skipped"
	if recoverySnapshotMatches(previewNotPending, p) {
		t.Fatal("a skipped preview must never start")
	}
}
