package model

import (
	"encoding/json"
	"regexp"
	"strings"
)

// MovieMetadata is the part of the saved TMDB metadata that the movie detail
// page shows. It is read from Movie.MetadataJSON, so rendering never calls TMDB.
type MovieMetadata struct {
	Tagline      string
	BackdropPath string
	Genres       []string
}

var (
	// TMDB image paths are a slash and a file name. Anything else is dropped
	// rather than placed in an image URL.
	backdropPathPattern = regexp.MustCompile(`^/[A-Za-z0-9._-]+$`)
	imdbIDPattern       = regexp.MustCompile(`^tt[0-9]+$`)
)

// Metadata decodes the saved TMDB metadata. Missing, malformed or partial
// metadata yields the zero value for the fields it cannot supply.
func (m *Movie) Metadata() MovieMetadata {
	var raw struct {
		Tagline      string  `json:"tagline"`
		BackdropPath *string `json:"backdrop_path"`
		Genres       []struct {
			Name string `json:"name"`
		} `json:"genres"`
	}
	if len(m.MetadataJSON) == 0 || json.Unmarshal(m.MetadataJSON, &raw) != nil {
		return MovieMetadata{}
	}

	meta := MovieMetadata{Tagline: strings.TrimSpace(raw.Tagline)}
	if raw.BackdropPath != nil && backdropPathPattern.MatchString(*raw.BackdropPath) {
		meta.BackdropPath = *raw.BackdropPath
	}
	for _, genre := range raw.Genres {
		if name := strings.TrimSpace(genre.Name); name != "" {
			meta.Genres = append(meta.Genres, name)
		}
	}
	return meta
}

// IMDbURL returns the movie's IMDb title page, or "" when the movie has no
// well-formed IMDb ID.
func (m *Movie) IMDbURL() string {
	if m.IMDBId == nil || !imdbIDPattern.MatchString(*m.IMDBId) {
		return ""
	}
	return "https://www.imdb.com/title/" + *m.IMDBId + "/"
}
