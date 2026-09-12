package utils

import "golang.org/x/crypto/bcrypt"

// HashPassword securely hashes a plain-text password.
//
// The plain-text password should never be stored
// directly in the database.
func HashPassword(password string) (string, error) {

	hash, err := bcrypt.GenerateFromPassword(
		[]byte(password),
		bcrypt.DefaultCost,
	)

	if err != nil {
		return "", err
	}

	return string(hash), nil
}

// CheckPassword compares a plain-text password
// with a previously generated password hash.
//
// It returns true when they match and false when
// they do not match.
func CheckPassword(
	password string,
	passwordHash string,
) bool {

	err := bcrypt.CompareHashAndPassword(
		[]byte(passwordHash),
		[]byte(password),
	)

	return err == nil
}
