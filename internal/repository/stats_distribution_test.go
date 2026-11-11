package repository

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/drywaters/dejaview/internal/model"
	"github.com/google/uuid"
)

// TestRatingDistributionsPostgres covers the per-person score histogram:
// whole-point buckets with halves rounding up, exact means, every rating
// counting, and people with no ratings left out.
func TestRatingDistributionsPostgres(t *testing.T) {
	pool := ratingTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	entries := NewEntryRepository(pool)
	stats := NewStatsRepository(pool)

	if got, err := stats.GetRatingDistributions(ctx); err != nil || len(got) != 0 {
		t.Fatalf("distributions with no ratings = %+v (err %v), want none", got, err)
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

	// Scores are per person, one per movie in order. Partial ratings count:
	// nobody needs a family-rated movie to show up. A rates nothing.
	scores := map[string][]float64{
		"D": {8, 8, 7.5, 7.4, 0, 10},
		"J": {5},
		"C": {6.5, 6.5},
	}
	for i := 0; i < 6; i++ {
		var movieID uuid.UUID
		if err := pool.QueryRow(ctx, `INSERT INTO movies(title) VALUES ($1) RETURNING id`, fmt.Sprintf("Movie %d", i)).Scan(&movieID); err != nil {
			t.Fatal(err)
		}
		entry, err := entries.Create(ctx, model.CreateEntryInput{MovieID: movieID, GroupNumber: 1})
		if err != nil {
			t.Fatal(err)
		}
		for initial, given := range scores {
			if i >= len(given) {
				continue
			}
			if _, err := pool.Exec(ctx, `INSERT INTO ratings(person_id, entry_id, score) VALUES ($1, $2, $3)`, people[initial], entry.ID, given[i]); err != nil {
				t.Fatal(err)
			}
		}
	}

	got, err := stats.GetRatingDistributions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	type want struct {
		count   int
		mean    float64
		buckets map[int]int
	}
	expected := map[string]want{
		"D": {count: 6, mean: (8 + 8 + 7.5 + 7.4 + 0 + 10) / 6, buckets: map[int]int{0: 1, 7: 1, 8: 3, 10: 1}},
		"J": {count: 1, mean: 5, buckets: map[int]int{5: 1}},
		"C": {count: 2, mean: 6.5, buckets: map[int]int{7: 2}},
	}
	if len(got) != len(expected) {
		t.Fatalf("got %d distributions, want %d: %+v", len(got), len(expected), got)
	}
	for i, dist := range got {
		if i > 0 && got[i-1].Person.Name > dist.Person.Name {
			t.Fatalf("distributions not ordered by name: %s before %s", got[i-1].Person.Name, dist.Person.Name)
		}
		w, ok := expected[dist.Person.Initial]
		if !ok {
			t.Fatalf("unexpected distribution for %s", dist.Person.Initial)
		}
		if dist.Count != w.count || math.Abs(dist.Mean-w.mean) > 1e-9 {
			t.Fatalf("%s: count %d mean %v, want %d and %v", dist.Person.Initial, dist.Count, dist.Mean, w.count, w.mean)
		}
		for bucket, n := range dist.Buckets {
			if n != w.buckets[bucket] {
				t.Fatalf("%s: bucket %d holds %d, want %d (%v)", dist.Person.Initial, bucket, n, w.buckets[bucket], dist.Buckets)
			}
		}
	}
}
