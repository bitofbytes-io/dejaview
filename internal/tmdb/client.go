package tmdb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/drywaters/dejaview/internal/metrics"
)

const (
	baseURL      = "https://api.themoviedb.org/3"
	imageBaseURL = "https://image.tmdb.org/t/p"
	// Keep upstream error diagnostics bounded, even for streaming responses.
	maxErrorBodyBytes = 4 << 10
)

// Client is a TMDB API client
type Client struct {
	readAccessToken string
	httpClient      *http.Client
	metrics         *metrics.Metrics
}

// NewClient creates a new TMDB client
func NewClient(readAccessToken string) *Client {
	return &Client{
		readAccessToken: readAccessToken,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
			// Never forward the credential to a redirect target, even a subdomain.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// WithMetrics returns a copy sharing the HTTP transport and recording to m.
// It leaves the original client unchanged and accepts a nil client or metrics.
func (c *Client) WithMetrics(m *metrics.Metrics) *Client {
	if c == nil {
		return nil
	}
	copy := *c
	copy.metrics = m
	return &copy
}

// SearchResult represents a movie search result from TMDB
type SearchResult struct {
	ID           int     `json:"id"`
	Title        string  `json:"title"`
	Overview     string  `json:"overview"`
	ReleaseDate  string  `json:"release_date"`
	PosterPath   *string `json:"poster_path"`
	VoteAverage  float64 `json:"vote_average"`
	VoteCount    int     `json:"vote_count"`
	Popularity   float64 `json:"popularity"`
	OriginalLang string  `json:"original_language"`
}

// SearchResponse represents the response from TMDB search API
type SearchResponse struct {
	Page         int            `json:"page"`
	Results      []SearchResult `json:"results"`
	TotalPages   int            `json:"total_pages"`
	TotalResults int            `json:"total_results"`
}

// MovieDetails represents detailed movie information from TMDB
type MovieDetails struct {
	ID                  int                 `json:"id"`
	Title               string              `json:"title"`
	OriginalTitle       string              `json:"original_title"`
	Overview            string              `json:"overview"`
	ReleaseDate         string              `json:"release_date"`
	PosterPath          *string             `json:"poster_path"`
	BackdropPath        *string             `json:"backdrop_path"`
	Runtime             int                 `json:"runtime"`
	VoteAverage         float64             `json:"vote_average"`
	VoteCount           int                 `json:"vote_count"`
	Popularity          float64             `json:"popularity"`
	IMDBId              *string             `json:"imdb_id"`
	Tagline             string              `json:"tagline"`
	Status              string              `json:"status"`
	Genres              []Genre             `json:"genres"`
	ProductionCompanies []ProductionCompany `json:"production_companies"`
	Budget              int64               `json:"budget"`
	Revenue             int64               `json:"revenue"`
}

// Genre represents a movie genre
type Genre struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// ProductionCompany represents a production company
type ProductionCompany struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Search searches for movies by title
func (c *Client) Search(ctx context.Context, query string) (*SearchResponse, error) {
	if strings.TrimSpace(query) == "" {
		return &SearchResponse{}, nil
	}

	params := url.Values{"query": {query}, "include_adult": {"false"}}
	endpoint := baseURL + "/search/movie?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", c.credentialSafeError(err))
	}

	req.Header.Set("Authorization", "Bearer "+c.readAccessToken)
	resultLabel := "failure"
	defer func() { c.metrics.TMDBLookup("search", resultLabel) }()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", c.credentialSafeError(err))
	}
	// Closing the read-only response must not replace a decode or API error.
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TMDB API error: %d - %s", resp.StatusCode, c.errorBody(resp.Body))
	}

	var result SearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", c.credentialSafeError(err))
	}

	resultLabel = "success"
	return &result, nil
}

// GetMovie fetches detailed movie information by TMDB ID
func (c *Client) GetMovie(ctx context.Context, tmdbID int) (*MovieDetails, error) {
	endpoint := fmt.Sprintf("%s/movie/%d", baseURL, tmdbID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", c.credentialSafeError(err))
	}

	req.Header.Set("Authorization", "Bearer "+c.readAccessToken)
	resultLabel := "failure"
	defer func() { c.metrics.TMDBLookup("movie", resultLabel) }()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", c.credentialSafeError(err))
	}
	// Closing the read-only response must not replace a decode or API error.
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		resultLabel = "not_found"
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TMDB API error: %d - %s", resp.StatusCode, c.errorBody(resp.Body))
	}

	var result MovieDetails
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", c.credentialSafeError(err))
	}

	resultLabel = "success"
	return &result, nil
}

// net/http can include an upstream Location header in an error before invoking
// CheckRedirect. Redact the displayed message while retaining the original
// error chain for errors.Is/As, including cancellation and timeout detection.
func (c *Client) credentialSafeError(err error) error {
	if c.readAccessToken == "" {
		return err
	}
	message := strings.ReplaceAll(err.Error(), c.readAccessToken, "[REDACTED]")
	return &credentialSafeError{message: message, cause: err}
}

type credentialSafeError struct {
	message string
	cause   error
}

func (e *credentialSafeError) Error() string { return e.message }
func (e *credentialSafeError) Unwrap() error { return e.cause }

func (c *Client) errorBody(r io.Reader) string {
	body, _ := io.ReadAll(io.LimitReader(r, maxErrorBodyBytes+1))
	truncated := len(body) > maxErrorBodyBytes
	if truncated {
		body = body[:maxErrorBodyBytes]
	}
	diagnostic := string(body)
	if c.readAccessToken != "" {
		diagnostic = strings.ReplaceAll(diagnostic, c.readAccessToken, "[REDACTED]")
		// A response cut off inside an echoed token must not expose its prefix.
		if truncated {
			for n := min(len(c.readAccessToken)-1, len(diagnostic)); n > 0; n-- {
				if strings.HasSuffix(diagnostic, c.readAccessToken[:n]) {
					diagnostic = diagnostic[:len(diagnostic)-n] + "[REDACTED]"
					break
				}
			}
		}
	}
	if truncated {
		diagnostic += " [truncated]"
	}
	return diagnostic
}

// PosterURL constructs the full URL for a poster image
// Size options: w92, w154, w185, w342, w500, w780, original
func (c *Client) PosterURL(path string, size string) string {
	if path == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s%s", imageBaseURL, size, path)
}

// RuntimeMinutes returns a TMDB runtime, or nil when TMDB has none. TMDB
// reports an unknown runtime as 0 or null, which both decode to 0.
func RuntimeMinutes(runtime int) *int {
	if runtime <= 0 {
		return nil
	}
	return &runtime
}

// ReleaseYear extracts the year from a TMDB release date string
func ReleaseYear(releaseDate string) *int {
	if len(releaseDate) < 4 {
		return nil
	}
	for i := 0; i < 4; i++ {
		if releaseDate[i] < '0' || releaseDate[i] > '9' {
			return nil
		}
	}
	year := int(releaseDate[0]-'0')*1000 +
		int(releaseDate[1]-'0')*100 +
		int(releaseDate[2]-'0')*10 +
		int(releaseDate[3]-'0')
	return &year
}
