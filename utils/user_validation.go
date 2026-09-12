package utils

import (
	"net/mail"
	"strings"
)

// ValidateUserRegistration checks whether the
// registration information is acceptable.
//
// It returns an empty string when everything is
// valid, or a message explaining the first problem.
func ValidateUserRegistration(
	email string,
	password string,
) string {

	email = strings.TrimSpace(email)

	if email == "" {
		return "Email is required"
	}

	_, err := mail.ParseAddress(email)

	if err != nil {
		return "Invalid email address"
	}

	if password == "" {
		return "Password is required"
	}

	if len(password) < 8 {
		return "Password must be at least 8 characters"
	}

	return ""
}

// ValidateRole checks whether a role name is one
// that the marketplace recognises.
func ValidateRole(role string) string {

	role = strings.ToLower(strings.TrimSpace(role))

	switch role {

	case "buyer", "seller", "admin":
		return ""

	default:
		return "Role must be buyer, seller, or admin"
	}
}
