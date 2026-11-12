package model

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestMovieMetadata(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want MovieMetadata
	}{
		{"none", "", MovieMetadata{}},
		{"malformed", `{"tagline":`, MovieMetadata{}},
		{"null fields", `{"tagline":"","backdrop_path":null,"genres":null}`, MovieMetadata{}},
		{
			"full",
			`{"tagline":" One chance. ","backdrop_path":"/abc123.jpg","genres":[{"id":1,"name":"Drama"},{"id":2,"name":" "},{"id":3,"name":"Crime"}]}`,
			MovieMetadata{Tagline: "One chance.", BackdropPath: "/abc123.jpg", Genres: []string{"Drama", "Crime"}},
		},
		{"unsafe backdrop", `{"backdrop_path":"/a/../b.jpg?x=1"}`, MovieMetadata{}},
		{"relative backdrop", `{"backdrop_path":"abc.jpg"}`, MovieMetadata{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			movie := &Movie{MetadataJSON: json.RawMessage(tt.raw)}
			if got := movie.Metadata(); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Metadata() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestMovieIMDbURL(t *testing.T) {
	str := func(s string) *string { return &s }
	tests := []struct {
		id   *string
		want string
	}{
		{nil, ""},
		{str(""), ""},
		{str("nm0000001"), ""},
		{str("tt12/../x"), ""},
		{str("tt0088763"), "https://www.imdb.com/title/tt0088763/"},
	}
	for _, tt := range tests {
		if got := (&Movie{IMDBId: tt.id}).IMDbURL(); got != tt.want {
			t.Errorf("IMDbURL(%v) = %q, want %q", tt.id, got, tt.want)
		}
	}
}
