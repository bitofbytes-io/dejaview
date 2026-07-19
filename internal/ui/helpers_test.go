package ui

import "testing"

func TestFormatRuntime(t *testing.T) {
	tests := map[int]string{
		0:   "0m",
		45:  "45m",
		60:  "1h",
		125: "2h 5m",
	}
	for minutes, want := range tests {
		if got := FormatRuntime(minutes); got != want {
			t.Errorf("FormatRuntime(%d) = %q, want %q", minutes, got, want)
		}
	}
}
