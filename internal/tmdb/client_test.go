package tmdb

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/drywaters/dejaview/internal/metrics"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestTransportErrorsDoNotLeakReadAccessToken(t *testing.T) {
	const token = "fake-tmdb-read-access-token"
	transportErr := errors.New("dial tcp: connection refused")
	client := &Client{
		readAccessToken: token,
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Header.Get("Authorization") != "Bearer "+token || req.URL.Query().Has("api_key") || strings.Contains(req.URL.String(), token) {
				t.Error("credential was not confined to the Authorization header")
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
			if strings.Contains(err.Error(), token) {
				t.Fatalf("error leaks read-access token: %v", err)
			}
			if strings.Contains(err.Error(), "api_key=") {
				t.Fatalf("credential query parameter remains in error: %v", err)
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

func TestRequestsUseBearerHeaderAndPreserveResults(t *testing.T) {
	const token = "fake-read-token"
	const query = "Alien & friends? 日本語+#"
	for _, method := range []string{"Search", "GetMovie"} {
		t.Run(method, func(t *testing.T) {
			client := NewClient(token)
			calls := 0
			body := &countedResponseBody{}
			client.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Method != http.MethodGet || req.Header.Get("Authorization") != "Bearer "+token || strings.Contains(req.URL.String(), token) || req.URL.Query().Has("api_key") {
					t.Fatal("unexpected request method or credential placement")
				}
				if method == "Search" {
					if req.URL.Path != "/3/search/movie" || req.URL.Query().Get("query") != query || req.URL.Query().Get("include_adult") != "false" || len(req.URL.Query()) != 2 {
						t.Fatalf("search parameters changed: %s", req.URL)
					}
					body.reader = strings.NewReader(`{"page":1,"results":[{"id":348,"title":"Alien"}],"total_results":1,"total_pages":1}`)
				} else {
					if req.URL.Path != "/3/movie/348" || req.URL.RawQuery != "" {
						t.Fatalf("movie URL changed: %s", req.URL)
					}
					body.reader = strings.NewReader(`{"id":348,"title":"Alien","runtime":117}`)
				}
				return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
			})
			if method == "Search" {
				result, err := client.Search(context.Background(), query)
				if err != nil || len(result.Results) != 1 || result.Results[0].ID != 348 {
					t.Fatalf("search result=%v err=%v", result, err)
				}
				empty, err := client.Search(context.Background(), "")
				if err != nil || len(empty.Results) != 0 || calls != 1 {
					t.Fatal("empty search made an upstream request")
				}
			} else {
				result, err := client.GetMovie(context.Background(), 348)
				if err != nil || result.ID != 348 || result.Runtime != 117 {
					t.Fatalf("movie result=%v err=%v", result, err)
				}
			}
			if !body.closed {
				t.Fatal("successful response body left open")
			}
		})
	}
}

func TestClientNeverFollowsRedirects(t *testing.T) {
	for _, target := range []string{"/3/redirected", "https://api.themoviedb.org/3/redirected", "https://sub.api.themoviedb.org/", "https://other.example/", "http://api.themoviedb.org/"} {
		for _, status := range []int{301, 302, 303, 307, 308} {
			for _, method := range []string{"Search", "GetMovie"} {
				t.Run(fmt.Sprintf("%s/%d/%s", target, status, method), func(t *testing.T) {
					client := NewClient("fake-redirect-token")
					calls := 0
					body := &countedResponseBody{reader: strings.NewReader("redirect denied")}
					client.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
						calls++
						if calls > 1 {
							t.Fatal("redirect target received a request")
						}
						return &http.Response{StatusCode: status, Header: http.Header{"Location": {target}}, Body: body}, nil
					})
					var err error
					if method == "Search" {
						_, err = client.Search(context.Background(), "alien")
					} else {
						_, err = client.GetMovie(context.Background(), 348)
					}
					if err == nil || calls != 1 || !body.closed {
						t.Fatalf("err=%v requests=%d closed=%t", err, calls, body.closed)
					}
				})
			}
		}
	}
}

func TestClientTimeoutAndCancellation(t *testing.T) {
	client := NewClient("fake-timeout-token")
	if client.httpClient.Timeout != 10*time.Second {
		t.Fatal("client timeout changed")
	}
	client.httpClient.Timeout = 10 * time.Millisecond
	client.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	for _, method := range []string{"Search", "GetMovie"} {
		for _, canceled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/canceled=%t", method, canceled), func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if canceled {
					cancel()
				}
				var err error
				if method == "Search" {
					_, err = client.Search(ctx, "alien")
				} else {
					_, err = client.GetMovie(ctx, 348)
				}
				if canceled {
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("cancellation error=%v", err)
					}
				} else {
					var timeout net.Error
					if !errors.As(err, &timeout) || !timeout.Timeout() {
						t.Fatalf("timeout error=%v", err)
					}
				}
			})
		}
	}
}

func TestErrorDiagnosticsRedactEchoedCredential(t *testing.T) {
	const token = "fake-echoed-token"
	for _, method := range []string{"Search", "GetMovie"} {
		for _, truncated := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/truncated=%t", method, truncated), func(t *testing.T) {
				diagnostic := "rejected Authorization: Bearer " + token
				if truncated {
					diagnostic = strings.Repeat("x", maxErrorBodyBytes-10) + token
				}
				client := NewClient(token)
				client.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(strings.NewReader(diagnostic))}, nil
				})
				var err error
				if method == "Search" {
					_, err = client.Search(context.Background(), "alien")
				} else {
					_, err = client.GetMovie(context.Background(), 348)
				}
				if err == nil || strings.Contains(err.Error(), token[:10]) || !strings.Contains(err.Error(), "[REDACTED]") {
					t.Fatalf("unsafe diagnostic: %v", err)
				}
			})
		}
	}
}

func TestMalformedRedirectErrorsDoNotExposeCredential(t *testing.T) {
	const token = "FAKE-REVIEW-READ-TOKEN"
	for _, method := range []string{"Search", "GetMovie"} {
		t.Run(method, func(t *testing.T) {
			client := NewClient(token)
			calls := 0
			body := &countedResponseBody{reader: strings.NewReader("redirect")}
			client.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: http.StatusFound, Header: http.Header{
					"Location": {"https://example.invalid/%xx?echo=" + token},
				}, Body: body}, nil
			})
			var err error
			if method == "Search" {
				_, err = client.Search(context.Background(), "alien")
			} else {
				_, err = client.GetMovie(context.Background(), 348)
			}
			if err == nil || strings.Contains(err.Error(), token) {
				t.Fatalf("malformed redirect error exposes credential: %v", err)
			}
			var urlErr *url.Error
			if !errors.As(err, &urlErr) {
				t.Fatalf("request error lost URL error type: %T", err)
			}
			if calls != 1 || !body.closed {
				t.Fatalf("requests=%d closed=%t", calls, body.closed)
			}
		})
	}
}

func TestCredentialSafeErrorsKeepCancellationAndTimeoutSemantics(t *testing.T) {
	const token = "fake-error-chain-token"
	for _, method := range []string{"Search", "GetMovie"} {
		for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
			t.Run(method+"/"+cause.Error(), func(t *testing.T) {
				original := &url.Error{Op: "read", URL: "https://example.invalid/?echo=" + token, Err: cause}
				client := NewClient(token)
				client.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, original })
				var err error
				if method == "Search" {
					_, err = client.Search(context.Background(), "alien")
				} else {
					_, err = client.GetMovie(context.Background(), 348)
				}
				if err == nil || strings.Contains(err.Error(), token) || !errors.Is(err, original) || !errors.Is(err, cause) {
					t.Fatalf("unsafe error or broken cause chain: %v", err)
				}
				var netErr net.Error
				if !errors.As(err, &netErr) || netErr.Timeout() != errors.Is(cause, context.DeadlineExceeded) {
					t.Fatalf("timeout semantics changed: %v", err)
				}
			})
		}
	}
}

// TestLookupMetrics verifies HTTP and decode outcomes without contacting TMDB.
func TestLookupMetrics(t *testing.T) {
	for _, operation := range []string{"search", "movie"} {
		for _, tc := range []struct {
			name         string
			status       int
			body         string
			transportErr error
			want         string
		}{
			{name: "success", status: 200, body: `{"id":348,"results":[]}`, want: "success"},
			{name: "not found", status: 404, body: `{}`, want: "not_found"},
			{name: "server failure", status: 500, body: `upstream failure`, want: "failure"},
			{name: "malformed JSON", status: 200, body: `{"id":`, want: "failure"},
			{name: "transport failure", transportErr: errors.New("dial failed"), want: "failure"},
		} {
			t.Run(operation+"/"+tc.name, func(t *testing.T) {
				m := metrics.New()
				original := NewClient("fake-private-token")
				client := original.WithMetrics(m)
				if original.metrics != nil {
					t.Fatal("WithMetrics changed original client")
				}
				calls := 0
				client.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
					calls++
					if got := lookupMetricSamples(t, m); len(got) != 0 {
						t.Fatalf("counter advanced before API result: %v", got)
					}
					if req.Header.Get("Authorization") != "Bearer fake-private-token" {
						t.Fatal("Bearer header changed")
					}
					if tc.transportErr != nil {
						return nil, tc.transportErr
					}
					return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
				})
				var err error
				if operation == "search" {
					_, err = client.Search(context.Background(), "private query")
				} else {
					_, err = client.GetMovie(context.Background(), 348)
				}
				want := tc.want
				if operation == "search" && want == "not_found" {
					want = "failure"
				}
				if (err != nil) != (want == "failure") {
					t.Fatalf("result=%s err=%v", want, err)
				}
				if calls != 1 {
					t.Fatalf("calls=%d", calls)
				}
				samples := lookupMetricSamples(t, m)
				expected := fmt.Sprintf(`dejaview_tmdb_lookups_total{operation="%s",result="%s"} 1`, operation, want)
				if len(samples) != 1 || samples[0] != expected {
					t.Fatalf("samples=%v, want %s", samples, expected)
				}
			})
		}
	}
}

// lookupMetricSamples reads only TMDB counter samples from the private handler.
func lookupMetricSamples(t *testing.T, m *metrics.Metrics) []string {
	t.Helper()
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("scrape status %d", rec.Code)
	}
	var samples []string
	for _, line := range strings.Split(rec.Body.String(), "\n") {
		if strings.HasPrefix(line, "dejaview_tmdb_lookups_total{") {
			samples = append(samples, line)
		}
	}
	return samples
}

// TestBlankSearchDoesNotRecordLookup keeps skipped HTTP calls out of the counter.
func TestBlankSearchDoesNotRecordLookup(t *testing.T) {
	m := metrics.New()
	client := NewClient("fake-token").WithMetrics(m)
	client.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("blank query made an HTTP call"); return nil, nil })
	for _, query := range []string{"", " ", "\t\n"} {
		result, err := client.Search(context.Background(), query)
		if err != nil || result == nil || len(result.Results) != 0 {
			t.Fatalf("result=%v err=%v", result, err)
		}
	}
	if samples := lookupMetricSamples(t, m); len(samples) != 0 {
		t.Fatalf("blank search counter: %v", samples)
	}
	var absent *Client
	if absent.WithMetrics(m) != nil {
		t.Fatal("nil client became non-nil")
	}
}
