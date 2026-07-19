package ui

import (
	"fmt"
	"strconv"

	"github.com/drywaters/dejaview/internal/model"
	"github.com/google/uuid"
)

func IntToStr(n int) string {
	return strconv.Itoa(n)
}

func FormatFloat(f float64) string {
	return fmt.Sprintf("%.1f", f)
}

// FormatRuntime formats a duration in minutes for compact UI displays.
func FormatRuntime(minutes int) string {
	if minutes <= 0 {
		return "0m"
	}
	hours := minutes / 60
	mins := minutes % 60
	if hours > 0 && mins > 0 {
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dm", mins)
}

func GetRatingScore(entry *model.Entry, personID uuid.UUID) *float64 {
	for _, r := range entry.Ratings {
		if r.PersonID == personID {
			return &r.Score
		}
	}
	return nil
}
