package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/drywaters/dejaview/internal/model"
	"github.com/google/uuid"
)

// TestMonthlyPicksPostgres covers the picks over time grouping: a fixed window
// of months ending with the current one, empty months included, entries
// grouped by picker, entries with no picker (never set, or the picker was
// deleted) counted separately and listed last, and entries outside the window
// left out: those before its first month and those from the start of the next
// UTC month onward. Later days of the current month are still counted.
func TestMonthlyPicksPostgres(t *testing.T) {
	pool := ratingTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	stats := NewStatsRepository(pool)
	now := time.Date(2026, time.November, 17, 12, 0, 0, 0, time.UTC)

	got, err := stats.GetMonthlyPicks(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != model.MonthsOfPicks {
		t.Fatalf("got %d months, want %d", len(got), model.MonthsOfPicks)
	}
	if first := got[0].Month; !first.Equal(time.Date(2025, time.December, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("first month = %v, want 2025-12-01", first)
	}
	if last := got[len(got)-1].Month; !last.Equal(time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("last month = %v, want 2026-11-01", last)
	}
	for _, month := range got {
		if month.Total != 0 || len(month.Picks) != 0 {
			t.Fatalf("month %v of an empty table = %+v, want no picks", month.Month, month)
		}
	}

	people := map[string]uuid.UUID{}
	rows, err := pool.Query(ctx, `SELECT initial, id FROM persons`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var initial string
		var id uuid.UUID
		if err := rows.Scan(&initial, &id); err != nil {
			t.Fatal(err)
		}
		people[initial] = id
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	var doomed uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO persons(initial, name) VALUES ('Z', 'Zed') RETURNING id`).Scan(&doomed); err != nil {
		t.Fatal(err)
	}

	entries := NewEntryRepository(pool)
	n := 0
	add := func(addedAt time.Time, picker *uuid.UUID) {
		t.Helper()
		n++
		var movieID uuid.UUID
		if err := pool.QueryRow(ctx, `INSERT INTO movies(title) VALUES ($1) RETURNING id`, fmt.Sprintf("Movie %d", n)).Scan(&movieID); err != nil {
			t.Fatal(err)
		}
		entry, err := entries.Create(ctx, model.CreateEntryInput{MovieID: movieID, GroupNumber: 1, PickedByPersonID: picker})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE entries SET added_at = $1 WHERE id = $2`, addedAt, entry.ID); err != nil {
			t.Fatal(err)
		}
	}
	d, j := people["D"], people["J"]
	at := func(year int, month time.Month, day, hour int) time.Time {
		return time.Date(year, month, day, hour, 0, 0, 0, time.UTC)
	}

	add(at(2025, time.November, 30, 23), &d) // before the window
	add(at(2025, time.December, 1, 0), &j)   // first instant of the window
	add(at(2026, time.March, 5, 20), &j)
	add(at(2026, time.March, 31, 23), &d) // last instant of March, still UTC
	add(at(2026, time.March, 9, 9), &d)
	add(at(2026, time.March, 12, 9), nil) // no picker
	add(at(2026, time.March, 13, 9), &doomed)
	add(at(2026, time.November, 30, 23), &d)
	add(at(2026, time.December, 1, 0), &d) // next month, not yet

	// Deleting a person sets picked_by_person_id to NULL, so Zed's pick joins
	// the unpicked entries.
	if _, err := pool.Exec(ctx, `DELETE FROM persons WHERE id = $1`, doomed); err != nil {
		t.Fatal(err)
	}

	got, err = stats.GetMonthlyPicks(ctx, now)
	if err != nil {
		t.Fatal(err)
	}

	type pick struct {
		initial string // "" for no picker
		count   int
	}
	want := map[time.Month][]pick{
		time.December: {{"J", 1}},
		time.March:    {{"D", 2}, {"J", 1}, {"", 2}},
		time.November: {{"D", 1}},
	}
	for _, month := range got {
		picks := want[month.Month.Month()]
		if month.Month.Month() == time.December && month.Month.Year() != 2025 {
			picks = nil
		}
		if len(month.Picks) != len(picks) {
			t.Fatalf("%s: got %+v, want %v", month.Month.Format("2006-01"), month.Picks, picks)
		}
		total := 0
		for i, p := range picks {
			g := month.Picks[i]
			if g.Count != p.count || (p.initial == "") != (g.Person == nil) || (g.Person != nil && g.Person.Initial != p.initial) {
				t.Fatalf("%s pick %d = %+v, want %+v", month.Month.Format("2006-01"), i, g, p)
			}
			total += p.count
		}
		if month.Total != total {
			t.Fatalf("%s total = %d, want %d", month.Month.Format("2006-01"), month.Total, total)
		}
	}
}
