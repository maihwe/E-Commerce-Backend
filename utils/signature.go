package utils

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"strings"
)

// ComputePaystackSignature produces the signature that
// Paystack would send for a request body.
//
// Paystack signs the raw request body with the secret key,
// using HMAC-SHA512, and sends the result in the
// x-paystack-signature header.
//
// This function is used to check incoming webhooks. It is
// also how the tests build a correctly signed payload, so
// the webhook path can be tested thoroughly without
// needing a real payment or a public URL.
func ComputePaystackSignature(
	secretKey string,
	body []byte,
) string {

	mac := hmac.New(
		sha512.New,
		[]byte(secretKey),
	)

	mac.Write(body)

	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyPaystackSignature reports whether a signature
// really came from Paystack.
//
// Two details matter here:
//
//  1. The comparison uses hmac.Equal, which takes the
//     same amount of time whatever the values are. A
//     plain == would stop at the first differing
//     character, and that tiny difference in timing can
//     be measured over many attempts to work out a valid
//     signature one character at a time.
//
//  2. The caller must pass the body exactly as it
//     arrived, before any JSON parsing. Re-encoding the
//     JSON would produce different bytes, and the
//     signature would never match.
func VerifyPaystackSignature(
	secretKey string,
	body []byte,
	signature string,
) bool {

	// A missing secret key means the check cannot be
	// performed, so the answer is no rather than yes.
	if secretKey == "" {
		return false
	}

	if signature == "" {
		return false
	}

	expected := ComputePaystackSignature(
		secretKey,
		body,
	)

	// Hex digits carry no case, so a signature that
	// arrived in capitals is the same signature. Paystack
	// always sends lower case, but a proxy or a test
	// client may not, and refusing one would be an
	// arbitrary failure rather than a security decision.
	// Lowercasing changes nothing about what is being
	// proved: the bytes still have to match exactly.
	presented := strings.ToLower(
		strings.TrimSpace(signature),
	)

	return hmac.Equal(
		[]byte(expected),
		[]byte(presented),
	)
}
