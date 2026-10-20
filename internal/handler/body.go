package handler

import (
	"errors"
	"io"
	"mime"
	"net/http"
)

// maxRequestBodyBytes allows 64 KiB for the app's small forms and reorder JSON.
// Enforce it while reading, including requests with unknown/chunked lengths.
const maxRequestBodyBytes = 64 << 10

func parseLimitedForm(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	err := r.ParseForm()
	if mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); err == nil && mediaType == "multipart/form-data" {
		// Preserve form fields without the implicit FormValue parser's larger
		// memory allowance. The same reader limit covers fields and file parts.
		err = r.ParseMultipartForm(maxRequestBodyBytes)
	}
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
