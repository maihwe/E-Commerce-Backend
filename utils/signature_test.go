package utils

import (
	"strings"
	"testing"
)

// testSecretKey stands in for PAYSTACK_SECRET_KEY.
//
// It is not a real key and never could be: Paystack's own
// test keys begin with sk_test_. A made-up value is used
// deliberately so that no credential ever appears in this
// repository.
const testSecretKey = "sk_test_not_a_real_key_for_tests"

// TestComputePaystackSignatureIsStable checks that the
// same key and body always produce the same signature.
//
// Paystack computes the signature over the raw request
// bytes, so if this were not stable, every genuine webhook
// would be rejected.
func TestComputePaystackSignatureIsStable(t *testing.T) {

	body := []byte(`{"event":"charge.success"}`)

	first := ComputePaystackSignature(
		testSecretKey,
		body,
	)

	second := ComputePaystackSignature(
		testSecretKey,
		body,
	)

	if first != second {

		t.Fatalf(
			"signature is not stable: %q then %q",
			first,
			second,
		)
	}

	// HMAC-SHA512 produces 64 bytes, which is 128 hex
	// characters.
	if len(first) != 128 {

		t.Fatalf(
			"expected a 128-character hex signature, got %d characters",
			len(first),
		)
	}
}

// TestComputePaystackSignatureDependsOnTheKey checks that
// a different key gives a different signature.
//
// This is the property that makes the signature worth
// anything: without it, anybody who knew the algorithm
// could forge a payment confirmation.
func TestComputePaystackSignatureDependsOnTheKey(t *testing.T) {

	body := []byte(`{"event":"charge.success"}`)

	genuine := ComputePaystackSignature(
		testSecretKey,
		body,
	)

	forged := ComputePaystackSignature(
		"sk_test_a_different_key",
		body,
	)

	if genuine == forged {

		t.Fatal(
			"two different keys produced the same signature",
		)
	}
}

// TestVerifyPaystackSignature accepts a genuine signature
// and rejects everything else.
func TestVerifyPaystackSignature(t *testing.T) {

	body := []byte(
		`{"event":"charge.success","data":{"reference":"ECB-1-abc"}}`,
	)

	genuine := ComputePaystackSignature(
		testSecretKey,
		body,
	)

	cases := []struct {
		name string

		secretKey string

		body []byte

		signature string

		want bool
	}{
		{
			name: "a genuine signature is accepted",

			secretKey: testSecretKey,
			body:      body,
			signature: genuine,

			want: true,
		},

		{
			name: "a tampered body is rejected",

			secretKey: testSecretKey,

			// One character changed, which is
			// enough: the whole point of the
			// signature is that any change at all
			// invalidates it.
			body: []byte(
				`{"event":"charge.success","data":{"reference":"ECB-1-abd"}}`,
			),

			signature: genuine,

			want: false,
		},

		{
			name: "a signature from a different key is rejected",

			secretKey: "sk_test_a_different_key",
			body:      body,
			signature: genuine,

			want: false,
		},

		{
			name: "an absent signature is rejected",

			secretKey: testSecretKey,
			body:      body,
			signature: "",

			want: false,
		},

		{
			name: "an empty key is rejected",

			secretKey: "",
			body:      body,
			signature: genuine,

			want: false,
		},

		{
			name: "nonsense is rejected rather than crashing",

			secretKey: testSecretKey,
			body:      body,
			signature: "not-hex-at-all",

			want: false,
		},
	}

	for _, testCase := range cases {

		t.Run(testCase.name, func(t *testing.T) {

			got := VerifyPaystackSignature(
				testCase.secretKey,
				testCase.body,
				testCase.signature,
			)

			if got != testCase.want {

				t.Errorf(
					"VerifyPaystackSignature returned %v, want %v",
					got,
					testCase.want,
				)
			}
		})
	}
}

// TestSignatureIsCaseInsensitiveAboutTheHeader checks
// that an uppercase signature still verifies.
//
// HTTP header values are opaque strings, and a proxy or a
// test client may pass one through in a different case.
// Hex digits carry no case, so refusing one would be an
// arbitrary failure.
func TestSignatureIsCaseInsensitiveAboutTheHeader(t *testing.T) {

	body := []byte(`{"event":"charge.success"}`)

	signature := ComputePaystackSignature(
		testSecretKey,
		body,
	)

	upper := strings.ToUpper(signature)

	if !VerifyPaystackSignature(
		testSecretKey,
		body,
		upper,
	) {

		t.Fatal(
			"an uppercase signature was rejected",
		)
	}
}

// TestSignatureIsOverTheRawBytes is the test that guards
// the most likely way this could be got wrong.
//
// Paystack signs the exact bytes it sends. If the handler
// were to parse the JSON and re-encode it before checking
// the signature, the bytes would differ — different key
// order, different whitespace — and a genuine webhook
// would be rejected. This test proves that the signature
// depends on the bytes rather than on the JSON value they
// represent.
func TestSignatureIsOverTheRawBytes(t *testing.T) {

	// These two bodies mean exactly the same thing to a
	// JSON parser, but they are different bytes.
	compact := []byte(`{"event":"charge.success","id":42}`)

	spaced := []byte(`{"event":"charge.success", "id": 42}`)

	compactSignature := ComputePaystackSignature(
		testSecretKey,
		compact,
	)

	if VerifyPaystackSignature(
		testSecretKey,
		spaced,
		compactSignature,
	) {

		t.Fatal(
			"a re-encoded body verified against a signature made over different bytes",
		)
	}

	// The matching bytes must still verify, so the test
	// above is proving a real distinction rather than
	// the function simply always failing.
	if !VerifyPaystackSignature(
		testSecretKey,
		compact,
		compactSignature,
	) {

		t.Fatal(
			"the original bytes failed to verify",
		)
	}
}
