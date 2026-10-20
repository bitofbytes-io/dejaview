package tmdb

import (
	"context"
	"errors"
	"fmt"
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

func TestRuntimeMinutesTreatsZeroAsUnknown(t *testing.T) {
	for _, runtime := range []int{0, -1} {
		if got := RuntimeMinutes(runtime); got != nil {
			t.Fatalf("RuntimeMinutes(%d) = %d, want nil", runtime, *got)
		}
	}
	if got := RuntimeMinutes(117); got == nil || *got != 117 {
		t.Fatalf("RuntimeMinutes(117) = %v, want 117", got)
	}
}

type countedResponseBody struct {
	reader *strings.Reader
	read   int
	closed bool
}

func (b *countedResponseBody) Read(p []byte) (int, error) {
	n, err := b.reader.Read(p)
	b.read += n
	return n, err
}

func (b *countedResponseBody) Close() error {
	b.closed = true
	return nil
}

func TestUpstreamErrorBodiesAreBoundedAndClosed(t *testing.T) {
	for _, method := range []string{"Search", "GetMovie"} {
		for _, size := range []int{32, maxErrorBodyBytes, maxErrorBodyBytes + 1, 10 * maxErrorBodyBytes} {
			t.Run(fmt.Sprintf("%s/size=%d", method, size), func(t *testing.T) {
				body := &countedResponseBody{reader: strings.NewReader(strings.Repeat("x", size))}
				client := &Client{httpClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: http.StatusBadGateway, Body: body, ContentLength: -1}, nil
				})}}
				var err error
				if method == "Search" {
					_, err = client.Search(context.Background(), "alien")
				} else {
					_, err = client.GetMovie(context.Background(), 348)
				}
				if err == nil {
					t.Fatal("expected upstream error")
				}
				want := "TMDB API error: 502 - " + strings.Repeat("x", min(size, maxErrorBodyBytes))
				if size > maxErrorBodyBytes {
					want += " [truncated]"
				}
				if err.Error() != want {
					t.Fatalf("diagnostic length=%d want=%d; truncation=%t", len(err.Error()), len(want), strings.HasSuffix(err.Error(), " [truncated]"))
				}
				if body.read != min(size, maxErrorBodyBytes+1) || !body.closed {
					t.Fatalf("upstream bytes read=%d closed=%t", body.read, body.closed)
				}
			})
		}
	}
}

func TestGetMovieNotFoundClosesWithoutReadingBody(t *testing.T) {
	body := &countedResponseBody{reader: strings.NewReader(strings.Repeat("x", 10*maxErrorBodyBytes))}
	client := &Client{httpClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNotFound, Body: body}, nil
	})}}
	movie, err := client.GetMovie(context.Background(), 348)
	if movie != nil || err != nil || !body.closed || body.read != 0 {
		t.Fatalf("movie=%v err=%v closed=%t read=%d", movie, err, body.closed, body.read)
	}
}
