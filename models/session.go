package models

import "time"

// Session represents one authenticated login session.
//
// A session connects a random session token to the
// user who logged in. The token is the only thing the
// browser holds, so it is kept secret and sent back
// inside an HttpOnly cookie.
type Session struct {

	// Token is the random secret sent to the
	// user's browser in an HttpOnly cookie.
	Token string `json:"-"`

	// UserID identifies the account that owns
	// this session.
	UserID int `json:"user_id"`

	// ExpiresAt determines when the session
	// should no longer be accepted.
	ExpiresAt time.Time `json:"expires_at"`
}
