package model

import (
	"testing"

	"github.com/google/uuid"
)

func TestMissingRaters(t *testing.T) {
	ana := &Person{ID: uuid.New(), Initial: "A"}
	ben := &Person{ID: uuid.New(), Initial: "B"}
	cy := &Person{ID: uuid.New(), Initial: "C"}
	persons := []*Person{ana, ben, cy}

	entry := &Entry{Ratings: []*Rating{{PersonID: ben.ID, Score: 7}}}
	missing := entry.MissingRaters(persons)
	if len(missing) != 2 || missing[0] != ana || missing[1] != cy {
		t.Fatalf("MissingRaters = %v, want Ana and Cy in order", missing)
	}
	if !entry.RatedBy(ben.ID) || entry.RatedBy(ana.ID) {
		t.Fatal("RatedBy does not match the ratings")
	}

	entry.Ratings = append(entry.Ratings, &Rating{PersonID: ana.ID}, &Rating{PersonID: cy.ID})
	if missing := entry.MissingRaters(persons); len(missing) != 0 {
		t.Fatalf("fully rated entry still misses %v", missing)
	}
}
