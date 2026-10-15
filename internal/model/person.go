package model

import "github.com/google/uuid"

// Person represents a family member who can rate movies
type Person struct {
	ID      uuid.UUID `json:"id"`
	Initial string    `json:"initial"` // one letter shown in badges, e.g. D
	Name    string    `json:"name"`    // e.g. Daniel
	Color   string    `json:"color"`   // one of PersonColors
}

// PersonColors lists the colours a person can have. Each name has a matching
// .person-<name> class in tailwind/styles.css.
var PersonColors = []string{"cyan", "rose", "amber", "mint", "violet"}

// ValidPersonColor reports whether color is one of PersonColors.
func ValidPersonColor(color string) bool {
	for _, c := range PersonColors {
		if c == color {
			return true
		}
	}
	return false
}

// ColorClass returns the person-specific CSS class for neon color styling,
// or "" when the stored colour is not in the palette.
func (p *Person) ColorClass() string {
	if ValidPersonColor(p.Color) {
		return "person-" + p.Color
	}
	return ""
}
