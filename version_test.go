package tisane

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

var semverPattern = regexp.MustCompile(`^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)

func TestVersionMatchesFileAndSemver(t *testing.T) {
	if Version == "" {
		t.Fatal("Version must not be empty")
	}
	if !semverPattern.MatchString(Version) {
		t.Fatalf("Version %q does not match semver format", Version)
	}

	raw, err := os.ReadFile("VERSION")
	if err != nil {
		t.Fatalf("read VERSION file: %v", err)
	}
	want := strings.TrimSpace(string(raw))
	if Version != want {
		t.Fatalf("Version = %q, want %q from VERSION file", Version, want)
	}
}
