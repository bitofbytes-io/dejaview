package model

import "testing"

func TestPersonColorClass(t *testing.T) {
	for _, color := range PersonColors {
		if got := (&Person{Color: color}).ColorClass(); got != "person-"+color {
			t.Errorf("ColorClass for %q = %q", color, got)
		}
	}
	for _, color := range []string{"", "teal", "person-cyan", "cyan x"} {
		if got := (&Person{Color: color}).ColorClass(); got != "" {
			t.Errorf("ColorClass for unknown %q = %q, want empty", color, got)
		}
	}
}
