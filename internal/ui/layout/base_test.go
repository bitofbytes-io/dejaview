package layout

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func renderHeader(t *testing.T, isAuthenticated bool, current string) string {
	t.Helper()
	var buf bytes.Buffer
	if err := Header(isAuthenticated, current).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestHeaderRendersInlineNavAndPhoneMenu(t *testing.T) {
	html := renderHeader(t, true, "people")

	inline, menu, ok := strings.Cut(html, `<details class="nav-menu">`)
	if !ok {
		t.Fatalf("header has no phone menu:\n%s", html)
	}
	// Both navs carry every link, so phones lose nothing to the menu.
	for name, nav := range map[string]string{"inline nav": inline, "phone menu": menu} {
		for _, want := range []string{
			`href="/"`,
			`href="/stats"`,
			`<a href="/people" class="nav-link" aria-current="page">People</a>`,
			`action="/logout"`,
		} {
			if !strings.Contains(nav, want) {
				t.Errorf("%s is missing %q:\n%s", name, want, nav)
			}
		}
	}
}

func TestHeaderForVisitorsOffersLogIn(t *testing.T) {
	html := renderHeader(t, false, "movies")

	if got := strings.Count(html, `href="/login"`); got != 2 {
		t.Errorf("want a log in link in the inline nav and the phone menu, got %d:\n%s", got, html)
	}
	for _, unwanted := range []string{`href="/people"`, `action="/logout"`} {
		if strings.Contains(html, unwanted) {
			t.Errorf("visitor header contains %q:\n%s", unwanted, html)
		}
	}
}
