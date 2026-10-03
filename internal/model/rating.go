package model

import (
	"time"

	"github.com/google/uuid"
)

// Rating represents a family member's rating for a movie entry
type Rating struct {
	ID        uuid.UUID `json:"id"`
	PersonID  uuid.UUID `json:"person_id"`
	EntryID   uuid.UUID `json:"entry_id"`
	Score     float64   `json:"score"` // 0.0 - 10.0
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Joined data (populated by repository)
	Person *Person `json:"person,omitempty"`
}

// RatingColor returns the color class based on the score
func (r *Rating) RatingColor() string {
	if r.Score < 4.0 {
		return "rating-low"
	}
	if r.Score < 7.0 {
		return "rating-mid"
	}
	return "rating-high"
}

// ScoreColorClass returns the CSS class for a given score value
func ScoreColorClass(score float64) string {
	if score < 4.0 {
		return "rating-low"
	}
	if score < 7.0 {
		return "rating-mid"
	}
	return "rating-high"
}

// RatingChange updates one person's rating; a nil Score deletes it.
type RatingChange struct {
	PersonID uuid.UUID
	Score    *float64
}
