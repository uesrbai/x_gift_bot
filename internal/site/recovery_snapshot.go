package site

// recoverySnapshotMatches verifies the durable, payment-sensitive fields of a
// preview before starting or executing an order. CheckoutURL and PaymentNode
// are display-only values derived from separately changing network metadata:
// comparing those strings would spuriously reject an otherwise unchanged
// checkout and leave the UI showing "pending" forever.
func recoverySnapshotMatches(preview, current recoveryItem) bool {
	return preview.State == "pending" &&
		current.State == "pending" &&
		preview.ID != "" && preview.ID == current.ID &&
		preview.Username != "" && preview.Username == current.Username &&
		preview.Recipient != "" && preview.Recipient == current.Recipient &&
		preview.Digest != "" && preview.Digest == current.Digest &&
		preview.Hint == current.Hint &&
		preview.Months == current.Months &&
		preview.Amount == current.Amount &&
		preview.Currency == current.Currency &&
		preview.NeedsUnpaidVerification == current.NeedsUnpaidVerification
}
