package tmdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	baseURL      = "https://api.themoviedb.org/3"
	imageBaseURL = "https://image.tmdb.org/t/p"
)

// Client is a TMDB API client
type Client struct {
	apiKey     string
	httpClient *http.Client
}

// NewClient creates a new TMDB client
func NewClient(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
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
	ID               int      `json:"id"`
	Title            string   `json:"title"`
	OriginalTitle    string   `json:"original_title"`
	Overview         string   `json:"overview"`
	ReleaseDate      string   `json:"release_date"`
	PosterPath       *string  `json:"poster_path"`
	BackdropPath     *string  `json:"backdrop_path"`
	Runtime          int      `json:"runtime"`
	VoteAverage      float64  `json:"vote_average"`
	VoteCount        int      `json:"vote_count"`
	Popularity       float64  `json:"popularity"`
	IMDBId           *string  `json:"imdb_id"`
	Tagline          string   `json:"tagline"`
	Status           string   `json:"status"`
	Genres           []Genre  `json:"genres"`
	ProductionCompanies []ProductionCompany `json:"production_companies"`
	Budget           int64    `json:"budget"`
	Revenue          int64    `json:"revenue"`
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
	if query == "" {
		return &SearchResponse{}, nil
	}

	endpoint := fmt.Sprintf("%s/search/movie?api_key=%s&query=%s&include_adult=false",
		baseURL,
		c.apiKey,
		url.QueryEscape(query),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", redactURLError(err))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", redactURLError(err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("TMDB API error: %d - %s", resp.StatusCode, string(body))
	}

	var result SearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &result, nil
}

// GetMovie fetches detailed movie information by TMDB ID
func (c *Client) GetMovie(ctx context.Context, tmdbID int) (*MovieDetails, error) {
	endpoint := fmt.Sprintf("%s/movie/%d?api_key=%s",
		baseURL,
		tmdbID,
		c.apiKey,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", redactURLError(err))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", redactURLError(err))
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("TMDB API error: %d - %s", resp.StatusCode, string(body))
	}

	var result MovieDetails
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &result, nil
}

// redactURLError removes the api_key query parameter from the URL that
// *url.Error embeds in its message, so transport failures never leak the key
// into logs. Other errors are returned unchanged.
func redactURLError(err error) error {
	var urlErr *url.Error
	if !errors.As(err, &urlErr) {
		return err
	}
	redacted := *urlErr
	redacted.URL = redactAPIKey(urlErr.URL)
	return &redacted
}

func redactAPIKey(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "[unparseable TMDB URL]"
	}
	query := u.Query()
	if query.Has("api_key") {
		query.Set("api_key", "REDACTED")
		u.RawQuery = query.Encode()
	}
	return u.String()
}

// PosterURL constructs the full URL for a poster image
// Size options: w92, w154, w185, w342, w500, w780, original
func (c *Client) PosterURL(path string, size string) string {
	if path == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s%s", imageBaseURL, size, path)
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

