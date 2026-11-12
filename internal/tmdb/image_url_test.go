package tmdb

import "testing"

func TestImageURL(t *testing.T) {
	if got, want := ImageURL("/abc.jpg", "w1280"), "https://image.tmdb.org/t/p/w1280/abc.jpg"; got != want {
		t.Errorf("ImageURL = %q, want %q", got, want)
	}
	if got := ImageURL("", "w1280"); got != "" {
		t.Errorf("ImageURL of an empty path = %q, want empty", got)
	}
	if got, want := (&Client{}).PosterURL("/p.jpg", "w500"), "https://image.tmdb.org/t/p/w500/p.jpg"; got != want {
		t.Errorf("PosterURL = %q, want %q", got, want)
	}
}
