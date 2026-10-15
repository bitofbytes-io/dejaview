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
// exists, after a move empties the group before the current one, and when the
// previous group's last entry has no picker.
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
	var picker model.Person
	if err := pool.QueryRow(ctx, `SELECT id, initial, name FROM persons ORDER BY initial LIMIT 1`).Scan(&picker.ID, &picker.Initial, &picker.Name); err != nil {
		t.Fatal(err)
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
	// Moving group 2's only entry to the end of group 1 empties group 2, so
	// the previous group is now group 1, where Brazil was picked last.
	group1 := 1
	if err := entries.Update(ctx, brazil, model.UpdateEntryInput{GroupNumber: &group1, PickedByPersonID: &picker.ID}); err != nil {
		t.Fatal(err)
	}
	holder, group, err := stats.GetAdvantageHolder(ctx)
	if err != nil || group != 1 || holder == nil || *holder != picker {
		t.Fatalf("advantage holder=%+v group=%d err=%v, want %s in group 1", holder, group, err, picker.Initial)
	}
	add("Dune", 2)
	check(2) // group 2's last entry has no picker
}

// TestGenrePickStatsPostgres covers reading genres from saved TMDB metadata:
// per-picker genre counts, family ratings only for fully rated entries, and
// movies with missing or malformed genre metadata being skipped.
func TestGenrePickStatsPostgres(t *testing.T) {
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

	addEntry := func(title, metadata, picker string) uuid.UUID {
		t.Helper()
		var meta any
		if metadata != "" {
			meta = metadata
		}
		var movieID uuid.UUID
		if err := pool.QueryRow(ctx, `INSERT INTO movies(title, metadata_json) VALUES ($1, $2::jsonb) RETURNING id`, title, meta).Scan(&movieID); err != nil {
			t.Fatal(err)
		}
		input := model.CreateEntryInput{MovieID: movieID, GroupNumber: 1}
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

	alien := addEntry("Alien", `{"genres":[{"id":27,"name":"Horror"},{"id":878,"name":"Science Fiction"}]}`, "D")
	rate(alien, map[string]float64{"D": 8, "J": 6, "C": 7, "A": 9}) // avg 7.5
	addEntry("Brazil", `{"genres":[{"id":878,"name":"Science Fiction"},{"id":878,"name":"Science Fiction"}]}`, "D")
	cube := addEntry("Cube", `{"genres":[{"id":27,"name":"Horror"}]}`, "J")
	rate(cube, map[string]float64{"D": 5, "J": 5, "C": 5, "A": 5}) // avg 5
	partial := addEntry("Dune", `{"genres":[{"id":878,"name":"Science Fiction"}]}`, "J")
	rate(partial, map[string]float64{"D": 10}) // not fully rated
	addEntry("Eraserhead", `{"genres":[{"id":1,"name":"Drama"}]}`, "")
	addEntry("Fargo", "", "D")                          // no metadata
	addEntry("Gattaca", `{"title":"Gattaca"}`, "D")     // no genres key
	addEntry("Heat", `{"genres":null}`, "D")            // null genres
	addEntry("Inception", `{"genres":"Thriller"}`, "D") // genres is not an array
	addEntry("Jaws", `{"genres":["Thriller",{"id":2},{"name":"  "},{"name":99},{"name":null},{"name":" Thriller "}]}`, "C")

	got, err := stats.GetGenrePickStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	type key struct{ picker, genre string }
	type want struct {
		picks, tagged, rated int
		total                float64
	}
	expected := map[key]want{
		{"Caleb", "Thriller"}:           {picks: 1, tagged: 1},
		{"Daniel", "Horror"}:            {picks: 1, tagged: 2, rated: 1, total: 7.5},
		{"Daniel", "Science Fiction"}:   {picks: 2, tagged: 2, rated: 1, total: 7.5},
		{"Jennifer", "Horror"}:          {picks: 1, tagged: 2, rated: 1, total: 5},
		{"Jennifer", "Science Fiction"}: {picks: 1, tagged: 2},
		{"", "Drama"}:                   {picks: 1, tagged: 1},
	}
	if len(got) != len(expected) {
		t.Fatalf("got %d genre rows, want %d: %+v", len(got), len(expected), got)
	}
	for _, stat := range got {
		k := key{genre: stat.Genre}
		if stat.Person != nil {
			k.picker = stat.Person.Name
		}
		w, ok := expected[k]
		if !ok {
			t.Fatalf("unexpected genre row %+v", stat)
		}
		if stat.Picks != w.picks || stat.TaggedPicks != w.tagged || stat.RatedPicks != w.rated || math.Abs(stat.RatingTotal-w.total) > 1e-9 {
			t.Fatalf("%v: got picks=%d tagged=%d rated=%d total=%v, want %+v", k, stat.Picks, stat.TaggedPicks, stat.RatedPicks, stat.RatingTotal, w)
		}
	}
}
