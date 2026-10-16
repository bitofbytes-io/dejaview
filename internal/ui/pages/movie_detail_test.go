package pages

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/drywaters/dejaview/internal/model"
	"github.com/google/uuid"
)

func TestRatingsCardCountsEveryPerson(t *testing.T) {
	persons := []*model.Person{
		{ID: uuid.New(), Initial: "A", Name: "Ana"},
		{ID: uuid.New(), Initial: "B", Name: "Ben"},
		{ID: uuid.New(), Initial: "C", Name: "Cy"},
	}
	entry := &model.Entry{ID: uuid.New(), Ratings: []*model.Rating{
		{PersonID: persons[0].ID, Score: 8},
		{PersonID: persons[2].ID, Score: 6},
	}}
	var buf bytes.Buffer
	if err := RatingsCard(entry, persons, true).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	if html := buf.String(); !strings.Contains(html, "2 of 3 rated") {
		t.Fatalf("ratings card does not say 2 of 3 rated:\n%s", html)
	}
}

func ratedEntry() (*model.Entry, []*model.Person) {
	persons := []*model.Person{
		{ID: uuid.New(), Initial: "A", Name: "Ana", Color: "rose"},
		{ID: uuid.New(), Initial: "B", Name: "Ben", Color: "mint"},
	}
	entry := &model.Entry{
		ID:      uuid.New(),
		Movie:   &model.Movie{Title: "Heat"},
		Ratings: []*model.Rating{{PersonID: persons[0].ID, Score: 8}},
	}
	return entry, persons
}

func render(t *testing.T, c templ.Component) string {
	t.Helper()
	var buf bytes.Buffer
	if err := c.Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// Edit controls that must never reach a visitor who isn't logged in.
var ratingEditControls = []string{
	"Save ratings",
	`class="btn-icon rating-clear"`,
	`class="rating-input"`,
	`type="submit"`,
	`/ratings"`,
}

func TestRatingsCardIsReadOnlyWhenLoggedOut(t *testing.T) {
	entry, persons := ratedEntry()
	html := render(t, RatingsCard(entry, persons, false))

	for _, unwanted := range ratingEditControls {
		if strings.Contains(html, unwanted) {
			t.Errorf("logged-out ratings card contains %q:\n%s", unwanted, html)
		}
	}
	for _, want := range []string{
		`class="rating-badge rating-score rating-high"`,
		">8.0</span>",
		"Not rated",
		"Log in to save ratings",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("logged-out ratings card is missing %q:\n%s", want, html)
		}
	}
}

func TestRatingsCardIsEditableWhenLoggedIn(t *testing.T) {
	entry, persons := ratedEntry()
	html := render(t, RatingsCard(entry, persons, true))

	for _, want := range []string{"Save ratings", "rating-input", "rating-clear", `value="8.0"`} {
		if !strings.Contains(html, want) {
			t.Errorf("logged-in ratings card is missing %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, "rating-score") || strings.Contains(html, "Log in to save ratings") {
		t.Errorf("logged-in ratings card shows the read-only view:\n%s", html)
	}
}

func TestMovieDetailPageHasNoRatingsFormWhenLoggedOut(t *testing.T) {
	entry, persons := ratedEntry()

	html := render(t, MovieDetailPage(entry, persons, false))
	for _, unwanted := range ratingEditControls {
		if strings.Contains(html, unwanted) {
			t.Errorf("logged-out movie page contains %q", unwanted)
		}
	}

	html = render(t, MovieDetailPage(entry, persons, true))
	if !strings.Contains(html, `hx-put="/api/entries/`+entry.ID.String()+`/ratings"`) {
		t.Error("logged-in movie page has no ratings form")
	}
}

// Edit controls for the entry itself, hidden entirely from visitors.
var entryEditControls = []string{
	`id="picked-by"`,
	"Picked by</label>",
	"hx-delete=",
	"Remove from collection",
}

func TestMovieDetailPageHidesEntryControlsWhenLoggedOut(t *testing.T) {
	entry, persons := ratedEntry()
	entry.PickedByPersonID = &persons[1].ID
	entry.PickedByPerson = persons[1]

	html := render(t, MovieDetailPage(entry, persons, false))
	for _, unwanted := range entryEditControls {
		if strings.Contains(html, unwanted) {
			t.Errorf("logged-out movie page contains %q", unwanted)
		}
	}
	// Visitors still see who picked the movie and its group.
	for _, want := range []string{"Picked by Ben", `class="text-kicker">Group</span>`} {
		if !strings.Contains(html, want) {
			t.Errorf("logged-out movie page is missing %q", want)
		}
	}

	html = render(t, MovieDetailPage(entry, persons, true))
	for _, want := range entryEditControls {
		if !strings.Contains(html, want) {
			t.Errorf("logged-in movie page is missing %q", want)
		}
	}
	if strings.Contains(html, "disabled") {
		t.Error("logged-in movie page has disabled controls")
	}
}
