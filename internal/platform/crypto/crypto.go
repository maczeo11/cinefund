// Package crypto holds the small cryptographic primitives the domains need:
// HMAC verification for webhooks and (later) argon2 password hashing. Keeping
// them here, rather than inline in a handler, means they are unit-testable and
// used identically everywhere.
package crypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
)

// ErrSignatureInvalid means the HMAC did not match - the payload was tampered
// with, or was signed with a different secret (usually a config mistake).
var ErrSignatureInvalid = errors.New("invalid signature")

// VerifyRazorpaySignature checks X-Razorpay-Signature = hex(HMAC_SHA256(raw, secret)).
//
// raw must be the exact bytes that arrived on the wire. Re-marshalling the
// parsed JSON before verification will fail intermittently on key order and
// whitespace.
func VerifyRazorpaySignature(raw []byte, signature, secret string) error {
	if signature == "" || secret == "" {
		return ErrSignatureInvalid
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(raw)
	expected := hex.EncodeToString(mac.Sum(nil))

	if subtle.ConstantTimeCompare([]byte(expected), []byte(signature)) != 1 {
		return ErrSignatureInvalid
	}
	return nil
}
