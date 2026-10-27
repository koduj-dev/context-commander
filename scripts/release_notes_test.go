// Package scripts holds tests for the repo's helper scripts.
package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// releaseNotes runs release-notes.sh against changelog and returns its output.
func releaseNotes(t *testing.T, changelog, version string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "sh", "release-notes.sh", version) //nolint:gosec // the repo's own script; version is a test constant
	cmd.Env = append(os.Environ(), "CHANGELOG="+changelog, "GITHUB_REPOSITORY=acme/app")
	out, err := cmd.Output()
	return string(out), err
}

// The GitHub release text is the version's CHANGELOG section. A release is
// immutable, so a version without one must fail before anything is published.
func TestReleaseNotes(t *testing.T) {
	cl := filepath.Join(t.TempDir(), "CHANGELOG.md")
	changelog := "# Changelog\n\n## [Unreleased]\n\n- next\n\n" +
		"## [1.0.0] — 2026-02-01\n\n- stable\n\n" +
		"## [1.0.0-beta.1] — 2026-01-02\n\n### Added\n- **Thing.** Detail.\n\n" +
		"## [0.9.0] — 2026-01-01\n\n- old\n"
	if err := os.WriteFile(cl, []byte(changelog), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := releaseNotes(t, cl, "v1.0.0-beta.1")
	if err != nil {
		t.Fatal(err)
	}
	want := "### Added\n- **Thing.** Detail.\n\n**Full changelog:** https://github.com/acme/app/blob/v1.0.0-beta.1/CHANGELOG.md\n"
	if out != want {
		t.Errorf("notes:\n%q\nwant:\n%q", out, want)
	}
	if strings.Contains(out, "old") || strings.Contains(out, "next") || strings.Contains(out, "stable") {
		t.Error("the notes ran into another version's section")
	}

	// 1.0.0 is a prefix of 1.0.0-beta.1's heading only if the match is sloppy.
	if out, err := releaseNotes(t, cl, "v1.0.0"); err != nil || !strings.HasPrefix(out, "- stable\n") {
		t.Errorf("v1.0.0: %q (err %v)", out, err)
	}

	if _, err := releaseNotes(t, cl, "v9.9.9"); err == nil {
		t.Error("a version without a CHANGELOG section produced notes")
	}
}

// Every version the real CHANGELOG links has a section the script finds, so
// the step can't trip over the CHANGELOG's own formatting.
func TestReleaseNotesForEveryShippedVersion(t *testing.T) {
	b, err := os.ReadFile("../CHANGELOG.md")
	if err != nil {
		t.Fatal(err)
	}
	versions := regexp.MustCompile(`(?m)^\[(\d+\.\d+\.\d+(?:-[0-9A-Za-z.]+)?)\]: `).FindAllStringSubmatch(string(b), -1)
	for _, v := range versions {
		if out, err := releaseNotes(t, "../CHANGELOG.md", "v"+v[1]); err != nil || len(strings.TrimSpace(out)) < 60 {
			t.Errorf("v%s: no usable notes (err %v)", v[1], err)
		}
	}
}
