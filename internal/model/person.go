package model

import "github.com/google/uuid"

// Person represents a family member who can rate movies
type Person struct {
	ID      uuid.UUID `json:"id"`
	Initial string    `json:"initial"` // D, J, C, A
	Name    string    `json:"name"`    // Daniel, Jennifer, Caleb, Aiden
}

// FamilyInitials is the ordered list of family member initials
var FamilyInitials = []string{"D", "J", "C", "A"}

// FamilyNames maps initials to full names
var FamilyNames = map[string]string{
	"D": "Daniel",
	"J": "Jennifer",
	"C": "Caleb",
	"A": "Aiden",
}

// personColorClasses maps initials to person-specific CSS color classes
var personColorClasses = map[string]string{
	"D": "person-daniel",
	"J": "person-jennifer",
	"C": "person-caleb",
	"A": "person-aiden",
}

// ColorClass returns the person-specific CSS class for neon color styling
func (p *Person) ColorClass() string {
	if cls, ok := personColorClasses[p.Initial]; ok {
		return cls
	}
	return ""
}


