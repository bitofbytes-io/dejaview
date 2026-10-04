package repository

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/drywaters/dejaview/internal/model"
	"github.com/google/uuid"
)

// TestStatsRepositoryPostgres covers the fully-rated filtering shared by the
// Trophy Room queries: only entries rated by every family member count toward
// rating averages and the top list, while runtime totals use every pick.
func TestStatsRepositoryPostgres(t *testing.T) {
	pool := ratingTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	entries := NewEntryRepository(pool)
	stats := NewStatsRepository(pool)

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

	addEntry := func(title string, runtime *int, group int, picker string) uuid.UUID {
		t.Helper()
		var movieID uuid.UUID
		if err := pool.QueryRow(ctx, `INSERT INTO movies(title, runtime_minutes) VALUES ($1, $2) RETURNING id`, title, runtime).Scan(&movieID); err != nil {
			t.Fatal(err)
		}
		input := model.CreateEntryInput{MovieID: movieID, GroupNumber: group}
		if picker != "" {
			id := people[picker]
			input.PickedByPersonID = &id
		}
		entry, err := entries.Create(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		return entry.ID
	}
	rate := func(entryID uuid.UUID, scores map[string]float64) {
		t.Helper()
		for initial, score := range scores {
			if _, err := pool.Exec(ctx, `INSERT INTO ratings(person_id, entry_id, score) VALUES ($1, $2, $3)`, people[initial], entryID, score); err != nil {
				t.Fatal(err)
			}
		}
	}
	minutes := func(m int) *int { return &m }

	alien := addEntry("Alien", minutes(100), 1, "D")
	rate(alien, map[string]float64{"D": 8, "J": 6, "C": 7, "A": 9}) // avg 7.5
	brazil := addEntry("Brazil", minutes(120), 1, "J")
	rate(brazil, map[string]float64{"D": 4, "J": 5, "C": 6, "A": 5}) // avg 5
	partial := addEntry("Cube", nil, 2, "D")
	rate(partial, map[string]float64{"D": 10, "J": 10}) // not fully rated
	addEntry("Dune", minutes(90), 2, "")

	trophies, err := stats.GetTrophyStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	type want struct {
		avgGiven, avgReceived              float64
		given, picks, runtime, runtimePick int
	}
	expected := map[string]want{
		"D": {avgGiven: 6, given: 2, avgReceived: 7.5, picks: 1, runtime: 100, runtimePick: 1},
		"J": {avgGiven: 5.5, given: 2, avgReceived: 5, picks: 1, runtime: 120, runtimePick: 1},
		"C": {avgGiven: 6.5, given: 2},
		"A": {avgGiven: 7, given: 2},
	}
	if len(trophies) != len(expected) {
		t.Fatalf("got %d trophy rows, want %d", len(trophies), len(expected))
	}
	for i, stat := range trophies {
		if i > 0 && trophies[i-1].Person.Name > stat.Person.Name {
			t.Fatalf("trophy rows not ordered by name: %s before %s", trophies[i-1].Person.Name, stat.Person.Name)
		}
		w := expected[stat.Person.Initial]
		got := want{stat.AvgRatingGiven, stat.AvgRatingReceived, stat.RatingsGiven, stat.FullyRatedPicks, stat.TotalRuntimePicked, stat.PicksWithKnownRuntime}
		if math.Abs(got.avgGiven-w.avgGiven) > 1e-9 || math.Abs(got.avgReceived-w.avgReceived) > 1e-9 ||
			got.given != w.given || got.picks != w.picks || got.runtime != w.runtime || got.runtimePick != w.runtimePick {
			t.Fatalf("%s: got %+v, want %+v", stat.Person.Initial, got, w)
		}
	}

	top, err := stats.GetTopRatedMovies(ctx, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 2 || top[0].Entry.ID != alien || top[1].Entry.ID != brazil {
		t.Fatalf("unexpected top movies: %+v", top)
	}
	if top[0].AverageRating != 7.5 || top[0].Entry.Movie.Title != "Alien" || top[0].Entry.PickedByPerson == nil || top[0].Entry.PickedByPerson.Initial != "D" {
		t.Fatalf("unexpected top movie: %+v", top[0])
	}
	if limited, err := stats.GetTopRatedMovies(ctx, 1); err != nil || len(limited) != 1 {
		t.Fatalf("limit not applied: %d rows, err=%v", len(limited), err)
	}

	watched, runtime, fullyRated, err := stats.GetSummaryStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if watched != 4 || runtime != 310 || fullyRated != 2 {
		t.Fatalf("summary watched=%d runtime=%d fullyRated=%d, want 4/310/2", watched, runtime, fullyRated)
	}

	// The advantage goes to whoever picked the last entry of the group
	// before the current (highest) one.
	holder, group, err := stats.GetAdvantageHolder(ctx)
	if err != nil || group != 1 || holder == nil || holder.Initial != "J" {
		t.Fatalf("advantage holder=%+v group=%d err=%v, want J in group 1", holder, group, err)
	}

	// "Fully rated" follows the number of people: with a fifth person, no
	// entry is fully rated until they rate it too.
	var eve uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO persons (initial, name) VALUES ('E', 'Eve') RETURNING id`).Scan(&eve); err != nil {
		t.Fatal(err)
	}
	people["E"] = eve
	if _, _, fullyRated, err := stats.GetSummaryStats(ctx); err != nil || fullyRated != 0 {
		t.Fatalf("fully rated with five people = %d (err %v), want 0", fullyRated, err)
	}
	rate(alien, map[string]float64{"E": 9})
	if top, err := stats.GetTopRatedMovies(ctx, 5); err != nil || len(top) != 1 || top[0].Entry.ID != alien {
		t.Fatalf("top movies with five people = %+v (err %v), want only Alien", top, err)
	}
}

// TestAdvantageHolderEdgesPostgres covers the advantage before a second group
// exists and when the previous group's last entry has no picker.
func TestAdvantageHolderEdgesPostgres(t *testing.T) {
	pool := ratingTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	entries := NewEntryRepository(pool)
	stats := NewStatsRepository(pool)

	check := func(wantGroup int) {
		t.Helper()
		holder, group, err := stats.GetAdvantageHolder(ctx)
		if err != nil || holder != nil || group != wantGroup {
			t.Fatalf("advantage holder=%+v group=%d err=%v, want nil in group %d", holder, group, err, wantGroup)
		}
	}
	add := func(title string, group int) uuid.UUID {
		t.Helper()
		var movieID uuid.UUID
		if err := pool.QueryRow(ctx, `INSERT INTO movies(title) VALUES ($1) RETURNING id`, title).Scan(&movieID); err != nil {
			t.Fatal(err)
		}
		entry, err := entries.Create(ctx, model.CreateEntryInput{MovieID: movieID, GroupNumber: group})
		if err != nil {
			t.Fatal(err)
		}
		return entry.ID
	}

	check(0) // no entries
	add("Alien", 1)
	check(0) // only group 1
	brazil := add("Brazil", 2)
	add("Cube", 3)
	group1 := 1
	if err := entries.Update(ctx, brazil, model.UpdateEntryInput{GroupNumber: &group1}); err != nil {
		t.Fatal(err)
	}
	check(2) // group 2 is empty
	add("Dune", 2)
	check(2) // group 2's last entry has no picker
}
