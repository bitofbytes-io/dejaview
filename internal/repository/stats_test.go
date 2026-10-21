package repository

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/drywaters/dejaview/internal/model"
	"github.com/google/uuid"
)

// TestStatsRepositoryPostgres covers the family-rated filtering shared by the
// Trophy Room queries: only entries rated by all but one family member count
// toward rating averages and the top list, while runtime totals use every pick.
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
	rate(partial, map[string]float64{"D": 10, "J": 10}) // 2 of 4: does not count
	addEntry("Dune", minutes(90), 2, "")
	eraserhead := addEntry("Eraserhead", minutes(80), 2, "D")
	rate(eraserhead, map[string]float64{"D": 6, "J": 8, "C": 4}) // 3 of 4 counts: avg 6

	trophies, err := stats.GetTrophyStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	type want struct {
		avgGiven, avgReceived              float64
		given, picks, runtime, runtimePick int
	}
	// D's picks average per movie, (7.5 + 6) / 2, so the 3-rating Eraserhead
	// weighs the same as the 4-rating Alien; pooling the 7 scores would give
	// 48/7.
	expected := map[string]want{
		"D": {avgGiven: 6, given: 3, avgReceived: 6.75, picks: 2, runtime: 180, runtimePick: 2},
		"J": {avgGiven: 19.0 / 3, given: 3, avgReceived: 5, picks: 1, runtime: 120, runtimePick: 1},
		"C": {avgGiven: 17.0 / 3, given: 3},
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

	// Contrarian gaps compare each family-rated score with the average of
	// everyone else's scores on that movie. D: Alien |8-22/3|, Brazil
	// |4-16/3|, Eraserhead |6-6|; the 2-of-4 Cube does not count.
	gaps := map[string]struct {
		gap   float64
		count int
	}{
		"D": {2.0 / 3, 3},
		"J": {5.0 / 3, 3},
		"C": {5.0 / 3, 3},
		"A": {1, 2},
	}
	for _, stat := range trophies {
		w := gaps[stat.Person.Initial]
		if math.Abs(stat.AvgRatingGap-w.gap) > 1e-9 || stat.GapRatings != w.count {
			t.Fatalf("%s: rating gap %v over %d, want %v over %d", stat.Person.Initial, stat.AvgRatingGap, stat.GapRatings, w.gap, w.count)
		}
	}

	// Eraserhead's 4 to 8 beats Alien's 6 to 9; Cube's split is not family-rated.
	divisive, err := stats.GetMostDivisiveMovie(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if divisive == nil || divisive.EntryID != eraserhead || divisive.Title != "Eraserhead" || divisive.LowScore != 4 || divisive.HighScore != 8 {
		t.Fatalf("most divisive = %+v, want Eraserhead 4 to 8", divisive)
	}

	top, err := stats.GetTopRatedMovies(ctx, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 3 || top[0].Entry.ID != alien || top[1].Entry.ID != eraserhead || top[2].Entry.ID != brazil {
		t.Fatalf("unexpected top movies: %+v", top)
	}
	if top[1].AverageRating != 6 {
		t.Fatalf("3-of-4 movie averages its ratings: got %v, want 6", top[1].AverageRating)
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
	if watched != 5 || runtime != 390 || fullyRated != 3 {
		t.Fatalf("summary watched=%d runtime=%d fullyRated=%d, want 5/390/3", watched, runtime, fullyRated)
	}

	// The advantage goes to whoever picked the last entry of the group
	// before the current (highest) one.
	holder, group, err := stats.GetAdvantageHolder(ctx)
	if err != nil || group != 1 || holder == nil || holder.Initial != "J" {
		t.Fatalf("advantage holder=%+v group=%d err=%v, want J in group 1", holder, group, err)
	}

	// The threshold follows the number of people: with a fifth person, a
	// movie needs 4 ratings, so the 3-of-4 Eraserhead drops out until
	// someone else rates it.
	var eve uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO persons (initial, name) VALUES ('E', 'Eve') RETURNING id`).Scan(&eve); err != nil {
		t.Fatal(err)
	}
	people["E"] = eve
	if _, _, fullyRated, err := stats.GetSummaryStats(ctx); err != nil || fullyRated != 2 {
		t.Fatalf("family rated with five people = %d (err %v), want 2", fullyRated, err)
	}
	if top, err := stats.GetTopRatedMovies(ctx, 5); err != nil || len(top) != 2 || top[0].Entry.ID != alien || top[1].Entry.ID != brazil {
		t.Fatalf("top movies with five people = %+v (err %v), want Alien and Brazil", top, err)
	}
	rate(eraserhead, map[string]float64{"E": 10}) // 4 of 5: avg 7
	if top, err := stats.GetTopRatedMovies(ctx, 5); err != nil || len(top) != 3 || top[1].Entry.ID != eraserhead || top[1].AverageRating != 7 {
		t.Fatalf("top movies after Eve rates Eraserhead = %+v (err %v), want it second at 7", top, err)
	}
}

// TestTasteMatchPostgres covers pairwise rating agreement over every shared
// rating, and the Most Divisive Movie's tie-breaks and empty state.
func TestTasteMatchPostgres(t *testing.T) {
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

	if pairs, err := stats.GetTastePairs(ctx); err != nil || len(pairs) != 0 {
		t.Fatalf("pairs with no ratings = %+v (err %v), want none", pairs, err)
	}
	if divisive, err := stats.GetMostDivisiveMovie(ctx); err != nil || divisive != nil {
		t.Fatalf("most divisive with no ratings = %+v (err %v), want nil", divisive, err)
	}

	addRated := func(title string, scores map[string]float64) uuid.UUID {
		t.Helper()
		var movieID uuid.UUID
		if err := pool.QueryRow(ctx, `INSERT INTO movies(title) VALUES ($1) RETURNING id`, title).Scan(&movieID); err != nil {
			t.Fatal(err)
		}
		entry, err := entries.Create(ctx, model.CreateEntryInput{MovieID: movieID, GroupNumber: 1})
		if err != nil {
			t.Fatal(err)
		}
		for initial, score := range scores {
			if _, err := pool.Exec(ctx, `INSERT INTO ratings(person_id, entry_id, score) VALUES ($1, $2, $3)`, people[initial], entry.ID, score); err != nil {
				t.Fatal(err)
			}
		}
		return entry.ID
	}

	// J scores one point above D every time, C mirrors D, and A rates
	// everything 5. Only D rates the last movie, so no pair shares it.
	addRated("Alien", map[string]float64{"D": 2, "J": 3, "C": 9, "A": 5})
	addRated("Brazil", map[string]float64{"D": 4, "J": 5, "C": 7, "A": 5})
	addRated("Cube", map[string]float64{"D": 6, "J": 7, "C": 5, "A": 5})
	addRated("Dune", map[string]float64{"D": 8, "J": 9, "C": 3, "A": 5})
	addRated("Eraserhead", map[string]float64{"D": 7, "J": 8, "C": 4})
	addRated("Fargo", map[string]float64{"D": 10})

	pairs, err := stats.GetTastePairs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	type pairKey struct{ a, b string }
	initials := map[uuid.UUID]string{}
	for initial, id := range people {
		initials[id] = initial
	}
	got := map[pairKey]model.TastePair{}
	for _, pair := range pairs {
		if pair.PersonA.String() >= pair.PersonB.String() {
			t.Fatalf("pair not ordered: %+v", pair)
		}
		a, b := initials[pair.PersonA], initials[pair.PersonB]
		if b < a {
			a, b = b, a
		}
		got[pairKey{a, b}] = pair
	}
	corr := func(v float64) *float64 { return &v }
	expected := map[pairKey]struct {
		shared int
		gap    float64
		corr   *float64
	}{
		{"D", "J"}: {5, 1, corr(1)},
		{"C", "D"}: {5, 3.8, corr(-1)}, // |2-9|, |4-7|, |6-5|, |8-3|, |7-4|
		{"C", "J"}: {5, 4, corr(-1)},   // |3-9|, |5-7|, |7-5|, |9-3|, |8-4|
		{"A", "D"}: {4, 2, nil},        // A's constant scores have no correlation
		{"A", "J"}: {4, 2, nil},
		{"A", "C"}: {4, 2, nil},
	}
	if len(got) != len(expected) {
		t.Fatalf("got %d pairs, want %d: %+v", len(got), len(expected), pairs)
	}
	for k, w := range expected {
		pair, ok := got[k]
		if !ok {
			t.Fatalf("missing pair %v", k)
		}
		if pair.SharedMovies != w.shared || math.Abs(pair.AvgGap-w.gap) > 1e-9 {
			t.Errorf("%v: shared=%d gap=%v, want %d and %v", k, pair.SharedMovies, pair.AvgGap, w.shared, w.gap)
		}
		switch {
		case w.corr == nil && pair.Correlation != nil:
			t.Errorf("%v: correlation %v, want none", k, *pair.Correlation)
		case w.corr != nil && (pair.Correlation == nil || math.Abs(*pair.Correlation-*w.corr) > 1e-9):
			t.Errorf("%v: correlation %v, want %v", k, pair.Correlation, *w.corr)
		}
	}

	// Alien's 2 to 9 spans 7 points, Dune's 3 to 9 only 6, and Fargo has a
	// single rating so it is not family-rated. A tie on range goes to the
	// wider overall spread: Garden State's 1, 1, 8, 8 beats Alien's 2, 3, 9, 5.
	divisive, err := stats.GetMostDivisiveMovie(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if divisive == nil || divisive.Title != "Alien" || divisive.LowScore != 2 || divisive.HighScore != 9 {
		t.Fatalf("most divisive = %+v, want Alien 2 to 9", divisive)
	}
	garden := addRated("Garden State", map[string]float64{"D": 1, "J": 1, "C": 8, "A": 8})
	if divisive, err := stats.GetMostDivisiveMovie(ctx); err != nil || divisive == nil || divisive.EntryID != garden {
		t.Fatalf("most divisive after a wider split = %+v (err %v), want Garden State", divisive, err)
	}
}

// TestTrophyRatingThresholdPostgres checks the SQL rule against
// model.TrophyRatingThreshold as the family shrinks: all but one person must
// rate a movie, and with one or two people a single rating is enough.
func TestTrophyRatingThresholdPostgres(t *testing.T) {
	pool := ratingTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	entries := NewEntryRepository(pool)
	stats := NewStatsRepository(pool)

	var movieID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO movies(title) VALUES ('Alien') RETURNING id`).Scan(&movieID); err != nil {
		t.Fatal(err)
	}
	entry, err := entries.Create(ctx, model.CreateEntryInput{MovieID: movieID, GroupNumber: 1})
	if err != nil {
		t.Fatal(err)
	}
	// Seeded people, rated in this order: D, J, C, A.
	order := []string{"D", "J", "C", "A"}
	setRaters := func(raters int) {
		t.Helper()
		if _, err := pool.Exec(ctx, `DELETE FROM ratings`); err != nil {
			t.Fatal(err)
		}
		for _, initial := range order[:raters] {
			if _, err := pool.Exec(ctx, `INSERT INTO ratings(person_id, entry_id, score) SELECT id, $1, 7 FROM persons WHERE initial = $2`, entry.ID, initial); err != nil {
				t.Fatal(err)
			}
		}
	}

	for _, people := range []int{4, 3, 2, 1} {
		// Remove people from the end of the order, so the raters remain.
		if _, err := pool.Exec(ctx, `DELETE FROM ratings WHERE person_id IN (SELECT id FROM persons WHERE initial = ANY($1))`, order[people:]); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM persons WHERE initial = ANY($1)`, order[people:]); err != nil {
			t.Fatal(err)
		}
		threshold := model.TrophyRatingThreshold(people)
		for raters := 0; raters <= people; raters++ {
			setRaters(raters)
			want := 0
			if raters > 0 && raters >= threshold {
				want = 1
			}
			_, _, got, err := stats.GetSummaryStats(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Errorf("%d of %d people rated: counted %d, want %d", raters, people, got, want)
			}
		}
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
	if err := pool.QueryRow(ctx, `SELECT id, initial, name, color FROM persons ORDER BY initial LIMIT 1`).Scan(&picker.ID, &picker.Initial, &picker.Name, &picker.Color); err != nil {
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
	add("Dune", 3) // the original group 3 was compacted to group 2
	check(2)       // group 2's last entry has no picker
}

// TestGenrePickStatsPostgres covers reading genres from saved TMDB metadata:
// per-picker genre counts, family ratings only for family-rated entries, and
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
	rate(partial, map[string]float64{"D": 10}) // 1 of 4: does not count
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
			// The Trophy Room colours genre chips by the picker's colour.
			if !model.ValidPersonColor(stat.Person.Color) {
				t.Fatalf("%s has no colour: %+v", stat.Person.Name, stat.Person)
			}
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
