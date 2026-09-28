package tmdb

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestTransportErrorsDoNotLeakAPIKey(t *testing.T) {
	const apiKey = "super-secret-tmdb-key"
	transportErr := errors.New("dial tcp: connection refused")
	client := &Client{
		apiKey: apiKey,
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Query().Get("api_key") != apiKey {
				t.Errorf("request did not carry the api key")
			}
			return nil, transportErr
		})},
	}

	calls := map[string]func() error{
		"Search": func() error {
			_, err := client.Search(context.Background(), "alien")
			return err
		},
		"GetMovie": func() error {
			_, err := client.GetMovie(context.Background(), 348)
			return err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			err := call()
			if err == nil {
				t.Fatal("expected transport error")
			}
			if strings.Contains(err.Error(), apiKey) {
				t.Fatalf("error leaks api key: %v", err)
			}
			if !strings.Contains(err.Error(), "api_key=REDACTED") {
				t.Fatalf("expected redacted URL in error, got %v", err)
			}
			if !errors.Is(err, transportErr) {
				t.Fatalf("expected underlying transport error to stay wrapped, got %v", err)
			}
		})
	}
}
