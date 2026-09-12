package handlers

import (
	"encoding/json"
	"net/http"
)

// writeJSON sends a JSON response with a status code.
func writeJSON(
	w http.ResponseWriter,
	status int,
	payload any,
) {

	w.Header().Set("Content-Type", "application/json")

	w.WriteHeader(status)

	if payload == nil {
		return
	}

	json.NewEncoder(w).Encode(payload)
}

// writeMessage sends a small JSON object with one
// "message" field.
//
// Errors are returned as JSON rather than as plain text so
// that a client can parse every response the same way.
func writeMessage(
	w http.ResponseWriter,
	status int,
	message string,
) {

	writeJSON(
		w,
		status,
		map[string]string{"message": message},
	)
}

// writeError sends a JSON error response.
func writeError(
	w http.ResponseWriter,
	status int,
	message string,
) {

	writeJSON(
		w,
		status,
		map[string]string{"error": message},
	)
}

// decodeJSONBody reads a JSON request body into a value.
//
// It reports failure by writing the response itself and
// returning false, so a handler can simply return:
//
//	if !decodeJSONBody(w, r, &request) {
//		return
//	}
func decodeJSONBody(
	w http.ResponseWriter,
	r *http.Request,
	target any,
) bool {

	// Cap the body so that a very large request cannot
	// exhaust memory.
	const maxBodyBytes = 1 << 20

	r.Body = http.MaxBytesReader(
		w,
		r.Body,
		maxBodyBytes,
	)

	err := json.NewDecoder(r.Body).Decode(target)

	if err != nil {

		writeError(
			w,
			http.StatusBadRequest,
			"Invalid JSON",
		)

		return false
	}

	return true
}
