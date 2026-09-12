package storage

import (
	"sync"

	"e-commerce-backend/models"
)

// Sessions live in memory rather than in PostgreSQL.
//
// A session is short-lived and can simply be recreated
// by logging in again, so keeping it in memory avoids a
// database round trip on every authenticated request.
//
// sessionsMutex protects the map because HTTP handlers
// run concurrently, and Go maps are not safe for
// simultaneous reads and writes.
var sessions = make(map[string]models.Session)
var sessionsMutex sync.RWMutex

// CreateSession stores a new session so that later
// requests carrying its token can be recognised.
func CreateSession(session models.Session) {

	sessionsMutex.Lock()
	defer sessionsMutex.Unlock()

	sessions[session.Token] = session
}

// GetSession looks up a session by its token.
//
// The second return value reports whether the
// session was found.
func GetSession(token string) (models.Session, bool) {

	sessionsMutex.RLock()
	defer sessionsMutex.RUnlock()

	session, exists := sessions[token]

	if !exists {
		return models.Session{}, false
	}

	return session, true
}

// DeleteSession removes one session, which logs
// that user out.
func DeleteSession(token string) {

	sessionsMutex.Lock()
	defer sessionsMutex.Unlock()

	delete(sessions, token)
}

// DeleteSessionsForUser removes all active sessions
// belonging to a specific user.
//
// This is used when an account's role changes, so that
// an old session cannot keep using the previous
// privileges.
func DeleteSessionsForUser(userID int) {

	sessionsMutex.Lock()
	defer sessionsMutex.Unlock()

	for token, session := range sessions {

		if session.UserID == userID {
			delete(sessions, token)
		}
	}
}
