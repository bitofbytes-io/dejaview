package pages

import (
	"bytes"
	"context"
	"strings"
	"testing"

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
