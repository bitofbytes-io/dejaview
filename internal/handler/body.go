package handler

import (
	"errors"
	"io"
	"net/http"
)

// maxRequestBodyBytes allows 64 KiB for the app's small forms and reorder JSON.
// Enforce it while reading, including requests with unknown/chunked lengths.
const maxRequestBodyBytes = 64 << 10

func parseLimitedForm(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	err := r.ParseForm()
	// ParseForm ignores bodies with other content types. Check those too before
	// allowing query parameters to trigger a mutation or create a session.
	if _, readErr := io.Copy(io.Discard, r.Body); readErr != nil {
		return readErr
	}
	return err
}

func requestBodyTooLarge(w http.ResponseWriter, err error) bool {
	var sizeErr *http.MaxBytesError
	if !errors.As(err, &sizeErr) {
		return false
	}
	http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
	return true
}
