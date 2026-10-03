package model

import (
	"time"

	"github.com/google/uuid"
)

// Entry represents a movie entry in a watch group
type Entry struct {
	ID               uuid.UUID  `json:"id"`
	MovieID          uuid.UUID  `json:"movie_id"`
	GroupNumber      int        `json:"group_number"`
	Position         int        `json:"position"` // Position within the group (1 = first)
	AddedAt          time.Time  `json:"added_at"`
	PickedByPersonID *uuid.UUID `json:"picked_by_person_id,omitempty"`

	// Joined data (populated by repository)
	Movie          *Movie    `json:"movie,omitempty"`
	Ratings        []*Rating `json:"ratings,omitempty"`
	PickedByPerson *Person   `json:"picked_by_person,omitempty"`
}

// CreateEntryInput represents the input for creating an entry
type CreateEntryInput struct {
	MovieID          uuid.UUID  `json:"movie_id"`
	GroupNumber      int        `json:"group_number"`
	PickedByPersonID *uuid.UUID `json:"picked_by_person_id,omitempty"`
}

// UpdateEntryInput represents the input for updating an entry
type UpdateEntryInput struct {
	GroupNumber      *int       `json:"group_number,omitempty"`
	PickedByPersonID *uuid.UUID `json:"picked_by_person_id,omitempty"`
}

// AverageRating returns the average rating for this entry, or nil if no ratings
func (e *Entry) AverageRating() *float64 {
	if len(e.Ratings) == 0 {
		return nil
	}

	var sum float64
	for _, r := range e.Ratings {
		sum += r.Score
	}
	avg := sum / float64(len(e.Ratings))
	return &avg
}

// RatingCount returns the number of ratings for this entry
func (e *Entry) RatingCount() int {
	return len(e.Ratings)
}
