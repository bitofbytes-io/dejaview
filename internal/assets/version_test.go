package assets

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func mustVersion(t *testing.T, paths ...string) string {
	t.Helper()
	got, err := Version(paths...)
	if err != nil {
		t.Fatalf("Version(%v) error = %v", paths, err)
	}
	return got
}

func TestVersionFormat(t *testing.T) {
	dir := t.TempDir()
	a := writeFile(t, dir, "a.css", "body")

	got := mustVersion(t, a)
	if len(got) != shortHashLength {
		t.Errorf("len(version) = %d, want %d", len(got), shortHashLength)
	}
	if !regexp.MustCompile(`^[0-9a-f]+$`).MatchString(got) {
		t.Errorf("version %q is not lowercase hex", got)
	}
}

func TestVersionStable(t *testing.T) {
	dir := t.TempDir()
	a := writeFile(t, dir, "a.css", "body")
	b := writeFile(t, dir, "b.js", "console.log(1)")

	first := mustVersion(t, a, b)
	second := mustVersion(t, a, b)
	if first != second {
		t.Errorf("Version not stable: %q != %q", first, second)
	}
}

func TestVersionChangesWithContent(t *testing.T) {
	for _, edited := range []string{"a.css", "b.js", "c.js"} {
		t.Run(edited, func(t *testing.T) {
			dir := t.TempDir()
			paths := []string{
				writeFile(t, dir, "a.css", "body"),
				writeFile(t, dir, "b.js", "console.log(1)"),
				writeFile(t, dir, "c.js", "htmx"),
			}
			before := mustVersion(t, paths...)

			writeFile(t, dir, edited, "changed")
			if after := mustVersion(t, paths...); after == before {
				t.Errorf("version unchanged after editing %s", edited)
			}
		})
	}
}

func TestVersionOrderSensitive(t *testing.T) {
	dir := t.TempDir()
	a := writeFile(t, dir, "a.css", "body")
	b := writeFile(t, dir, "b.js", "console.log(1)")

	if mustVersion(t, a, b) == mustVersion(t, b, a) {
		t.Error("version should differ when path order differs")
	}
}

func TestVersionDistinguishesContentSplits(t *testing.T) {
	dir := t.TempDir()
	// The same concatenated bytes ("abcd") split differently across two files.
	a1 := writeFile(t, dir, "a1", "ab")
	b1 := writeFile(t, dir, "b1", "cd")
	a2 := writeFile(t, dir, "a2", "a")
	b2 := writeFile(t, dir, "b2", "bcd")

	if mustVersion(t, a1, b1) == mustVersion(t, a2, b2) {
		t.Error("different splits of the same bytes produced the same version")
	}
}

func TestVersionDistinguishesSplitsAtSamePaths(t *testing.T) {
	dir := t.TempDir()
	a := writeFile(t, dir, "a", "ab")
	b := writeFile(t, dir, "b", "cd")
	before := mustVersion(t, a, b)

	writeFile(t, dir, "a", "a")
	writeFile(t, dir, "b", "bcd")
	if after := mustVersion(t, a, b); after == before {
		t.Error("moving bytes between files did not change the version")
	}
}

func TestVersionIncludesPath(t *testing.T) {
	dir := t.TempDir()
	a := writeFile(t, dir, "a.css", "same")
	b := writeFile(t, dir, "b.css", "same")

	if mustVersion(t, a) == mustVersion(t, b) {
		t.Error("identical content at different paths produced the same version")
	}
}

func TestVersionEmptyFiles(t *testing.T) {
	dir := t.TempDir()
	a := writeFile(t, dir, "a", "")
	b := writeFile(t, dir, "b", "")

	if mustVersion(t, a) == mustVersion(t, a, b) {
		t.Error("adding an empty file should change the version")
	}
}

func TestVersionErrors(t *testing.T) {
	dir := t.TempDir()
	existing := writeFile(t, dir, "a.css", "body")
	missing := filepath.Join(dir, "missing.js")

	tests := []struct {
		name    string
		paths   []string
		wantErr string
	}{
		{"no paths", nil, "no asset paths"},
		{"missing file", []string{missing}, "open " + missing},
		{"missing after existing", []string{existing, missing}, "open " + missing},
		{"directory", []string{dir}, "read " + dir},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Version(tc.paths...)
			if err == nil {
				t.Fatalf("Version(%v) = %q, want error", tc.paths, got)
			}
			if got != "" {
				t.Errorf("version = %q, want empty on error", got)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}
