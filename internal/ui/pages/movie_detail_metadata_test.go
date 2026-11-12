package pages

import (
	"strings"
	"testing"
)

func TestMovieDetailPageShowsSavedMetadata(t *testing.T) {
	entry, persons := ratedEntry()
	imdb := "tt0113277"
	entry.Movie.IMDBId = &imdb
	entry.Movie.MetadataJSON = []byte(`{"tagline":"A cop & a thief","backdrop_path":"/heat.jpg","genres":[{"id":80,"name":"Crime"},{"id":18,"name":"Drama"}]}`)

	html := render(t, MovieDetailPage(entry, persons, false))
	for _, want := range []string{
		`src="https://image.tmdb.org/t/p/w1280/heat.jpg"`,
		`class="detail-tagline">A cop &amp; a thief</p>`,
		`>Crime</span>`,
		`>Drama</span>`,
		`href="https://www.imdb.com/title/tt0113277/"`,
		`rel="noopener noreferrer"`,
		"Family ratings",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("movie page is missing %q", want)
		}
	}
}

func TestMovieDetailPageOmitsMissingMetadata(t *testing.T) {
	entry, persons := ratedEntry()
	for name, raw := range map[string]string{
		"no metadata":     "",
		"empty fields":    `{"tagline":"","backdrop_path":null,"genres":[]}`,
		"malformed":       `{"tagline":`,
		"unsafe backdrop": `{"backdrop_path":"https://evil.example/x.jpg"}`,
	} {
		entry.Movie.MetadataJSON = []byte(raw)
		html := render(t, MovieDetailPage(entry, persons, false))
		for _, unwanted := range []string{"detail-backdrop", "detail-tagline", "detail-tags", "imdb.com", "evil.example"} {
			if strings.Contains(html, unwanted) {
				t.Errorf("%s: movie page contains %q", name, unwanted)
			}
		}
		if !strings.Contains(html, "Heat") {
			t.Errorf("%s: movie page lost the title", name)
		}
	}
}
