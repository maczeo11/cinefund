// Package crypto holds the HMAC helpers used to authenticate Razorpay traffic.
package crypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
)

var ErrSignatureInvalid = errors.New("invalid signature")

// VerifyRazorpaySignature checks X-Razorpay-Signature = hex(HMAC_SHA256(raw, secret)).
//
// raw must be the exact bytes off the wire. Re-marshalling the parsed JSON
// first will fail intermittently on key order and whitespace.
func VerifyRazorpaySignature(raw []byte, signature, secret string) error {
	return verifyHMAC(raw, signature, secret)
}

// VerifyRazorpayCheckoutSignature checks the signature Checkout hands back to
// the browser. Note this one is keyed with the API key secret, not the webhook
// secret, and signs "order_id|payment_id" rather than a body.
func VerifyRazorpayCheckoutSignature(orderID, paymentID, signature, keySecret string) error {
	if orderID == "" || paymentID == "" {
		return ErrSignatureInvalid
	}
	return verifyHMAC([]byte(orderID+"|"+paymentID), signature, keySecret)
}

func verifyHMAC(payload []byte, signature, secret string) error {
	if signature == "" || secret == "" {
		return ErrSignatureInvalid
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))

	if subtle.ConstantTimeCompare([]byte(expected), []byte(signature)) != 1 {
		return ErrSignatureInvalid
	}
	return nil
}
