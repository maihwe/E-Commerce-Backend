package utils

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// GeneratePaymentReference creates a reference for a
// payment.
//
// Two properties matter:
//
//   - It must be unique. Paystack rejects a reference it
//     has seen before, so a repeat would block a real
//     payment. 16 random bytes make a collision
//     vanishingly unlikely.
//
//   - It should be recognisable. Starting with the order
//     number means a reference seen in the Paystack
//     dashboard can be traced back to an order at a
//     glance, which is a real help when something needs
//     investigating.
func GeneratePaymentReference(orderID int) (string, error) {

	randomBytes := make([]byte, 16)

	_, err := rand.Read(randomBytes)

	if err != nil {
		return "", err
	}

	return fmt.Sprintf(
		"ECB-%d-%s",
		orderID,
		hex.EncodeToString(randomBytes),
	), nil
}
