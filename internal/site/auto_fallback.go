package site

import "xgift/internal/checkout"

// safeAutoFallback is deliberately strict: a manual link may be prepared
// automatically only if neither the old nor the current encrypted order
// contains evidence of a card tokenization/confirmation attempt. The caller
// must still perform a fresh live Stripe verification before showing a link.
// Unknown, declined, 3DS and submitted payments never qualify here.
func safeAutoFallback(before, after *checkout.Record) bool {
	if before == nil || after == nil ||
		(before.Status != "created" && before.Status != "creating") ||
		after.Status != "created" ||
		before.Username == "" || before.RecipientID == "" ||
		before.Username != after.Username ||
		before.RecipientID != after.RecipientID ||
		before.Months != after.Months ||
		before.Amount != after.Amount ||
		before.Currency != after.Currency ||
		before.ProductID != after.ProductID ||
		after.SessionID == "" || after.URL == "" ||
		!noPaymentSubmission(before) || !noPaymentSubmission(after) ||
		before.RecoveryAttempts != after.RecoveryAttempts ||
		before.ManualRecovery != after.ManualRecovery {
		return false
	}
	if before.SessionID != "" && before.SessionID != after.SessionID {
		// A replacement is allowed only when it was already archived through
		// the established verified-unpaid replacement protocol.
		return after.PreviousSession == before.SessionID &&
			after.ReplacementCount == before.ReplacementCount+1
	}
	return true
}

func noPaymentSubmission(r *checkout.Record) bool {
	return r.SubmittedAt == 0 && r.PaymentMethod == "" &&
		r.ConfirmParameters == "" && r.ConfirmKey == "" &&
		!r.PreflightSaved && r.CardFingerprint == "" && r.LastError == nil
}
