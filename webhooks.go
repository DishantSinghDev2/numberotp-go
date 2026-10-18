package numberotp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// VerifyWebhook checks a webhook's x-numberotp-signature header ("sha256=<hex>")
// against the raw request body and your webhook secret. Verify before parsing JSON.
//
//	body, _ := io.ReadAll(r.Body)
//	if !numberotp.VerifyWebhook(body, r.Header.Get("X-NumberOTP-Signature"), secret) { … 401 … }
func VerifyWebhook(rawBody []byte, signatureHeader, secret string) bool {
	if signatureHeader == "" || secret == "" {
		return false
	}
	given := strings.ToLower(strings.TrimSpace(signatureHeader))
	given = strings.TrimPrefix(given, "sha256=")
	got, err := hex.DecodeString(given)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(rawBody)
	return hmac.Equal(mac.Sum(nil), got)
}

// WebhookPayload is the body NumberOTP POSTs when an SMS arrives.
type WebhookPayload struct {
	Event       string  `json:"event"`
	EventID     string  `json:"event_id"`
	ID          string  `json:"id"`
	PhoneNumber string  `json:"phone_number"`
	Service     string  `json:"service"`
	Country     string  `json:"country"`
	OTP         *string `json:"otp"`
	FullSMS     *string `json:"full_sms"`
	Status      string  `json:"status"`
}
