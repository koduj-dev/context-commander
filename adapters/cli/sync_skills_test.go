package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/koduj-dev/context-commander/internal/skills"
)

// The planning and the name sanitizing are the parts worth testing on their
// own: one decides what gets downloaded, the other decides where bytes from
// a shared registry land on this machine. Both are pure, which is why they
// are pure — the IO around them is a thin shell.

func binding(skillID, versionID uuid.UUID) skills.SkillBinding {
	return skills.SkillBinding{SkillID: skillID, SkillVersionID: versionID}
}

func TestPlanSyncDownloadsOnlyWhatChanged(t *testing.T) {
	unchangedSkill, unchangedVersion := uuid.New(), uuid.New()
	movedSkill, oldVersion, newVersion := uuid.New(), uuid.New(), uuid.New()
	freshSkill, freshVersion := uuid.New(), uuid.New()
	goneSkill, goneVersion := uuid.New(), uuid.New()

	manifest := syncManifest{Skills: map[string]syncedSkill{
		unchangedSkill.String(): {SkillID: unchangedSkill.String(), VersionID: unchangedVersion.String(), Name: "steady", Dir: "steady"},
		movedSkill.String():     {SkillID: movedSkill.String(), VersionID: oldVersion.String(), Name: "moving", Dir: "moving"},
		goneSkill.String():      {SkillID: goneSkill.String(), VersionID: goneVersion.String(), Name: "removed", Dir: "removed"},
	}}

	steps := planSync([]skills.SkillBinding{
		binding(unchangedSkill, unchangedVersion),
		binding(movedSkill, newVersion),
		binding(freshSkill, freshVersion),
	}, manifest)

	got := map[uuid.UUID]syncAction{}
	for _, s := range steps {
		got[s.SkillID] = s.Action
	}
	if got[unchangedSkill] != actionUnchanged {
		t.Fatalf("a skill still bound to the same immutable version = %v, want unchanged (and no download)", got[unchangedSkill])
	}
	if got[movedSkill] != actionUpdate {
		t.Fatalf("a skill whose binding moved to a new version = %v, want update", got[movedSkill])
	}
	if got[freshSkill] != actionWrite {
		t.Fatalf("a skill not present locally = %v, want write", got[freshSkill])
	}
	if got[goneSkill] != actionPrune {
		t.Fatalf("a skill the project no longer binds = %v, want prune", got[goneSkill])
	}
	if len(steps) != 4 {
		t.Fatalf("steps = %d, want 4", len(steps))
	}
}

// TestPlanSyncIgnoresUnknownManifestEntries: a hand-edited or
// partially-corrupt manifest must not make the command delete something it
// cannot even identify.
func TestPlanSyncIgnoresUnknownManifestEntries(t *testing.T) {
	manifest := syncManifest{Skills: map[string]syncedSkill{
		"not-a-uuid": {Name: "mystery", Dir: "mystery"},
	}}
	steps := planSync(nil, manifest)
	if len(steps) != 0 {
		t.Fatalf("steps = %+v, want none: an unparseable entry must be left alone, not pruned", steps)
	}
}

func TestPlanSyncIsStable(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	bindings := []skills.SkillBinding{binding(a, uuid.New()), binding(b, uuid.New())}

	first := planSync(bindings, syncManifest{Skills: map[string]syncedSkill{}})
	second := planSync([]skills.SkillBinding{bindings[1], bindings[0]}, syncManifest{Skills: map[string]syncedSkill{}})
	for i := range first {
		if first[i].SkillID != second[i].SkillID {
			t.Fatalf("plan order depends on input order; step %d differs", i)
		}
	}
}

// TestSkillDirNameIsAlwaysOnePathComponent is the security test. Skill names
// come from a registry other people publish into, and this command writes to
// the caller's filesystem — a name must never be able to escape the target
// directory or name a dotfile.
func TestSkillDirNameIsAlwaysOnePathComponent(t *testing.T) {
	id := uuid.New()
	for _, name := range []string{
		"../../.ssh/authorized_keys",
		"..",
		".",
		"a/b/c",
		`..\..\windows`,
		"",
		"   ",
		".hidden",
		"normal-name",
		"Name With Spaces",
		"emoji-🙂-name",
	} {
		got := skillDirName(name, id)
		if got == "" {
			t.Fatalf("skillDirName(%q) is empty", name)
		}
		if got != filepath.Base(got) {
			t.Fatalf("skillDirName(%q) = %q, which is not a single path component", name, got)
		}
		if strings.ContainsAny(got, `/\`) {
			t.Fatalf("skillDirName(%q) = %q contains a path separator", name, got)
		}
		if got == "." || got == ".." || strings.HasPrefix(got, ".") {
			t.Fatalf("skillDirName(%q) = %q, which escapes or hides", name, got)
		}
		// And it stays inside the target directory once joined.
		joined := filepath.Clean(filepath.Join("/skills", got))
		if !strings.HasPrefix(joined, "/skills/") {
			t.Fatalf("skillDirName(%q) = %q escapes the target directory: %s", name, got, joined)
		}
	}
}

func TestSkillDirNameKeepsCleanNamesAndDisambiguatesDirty(t *testing.T) {
	id := uuid.New()
	if got := skillDirName("code-review", id); got != "code-review" {
		t.Fatalf("a already-safe name was mangled: %q", got)
	}
	// Two different skills whose names sanitize identically must not land
	// in the same directory.
	other := uuid.New()
	a := skillDirName("code review", id)
	b := skillDirName("code/review", other)
	if a == b {
		t.Fatalf("two different skills collided on directory %q", a)
	}
	if !strings.HasPrefix(a, "code-review-") {
		t.Fatalf("sanitized name = %q, want it to remain recognizable", a)
	}
}

func TestRemoveSkillDirRefusesAnythingButAPlainChild(t *testing.T) {
	for _, target := range []string{"..", ".", "", "a/b", "../escape", "/etc"} {
		if err := removeSkillDir(t.TempDir(), target); err == nil {
			t.Fatalf("removeSkillDir accepted %q from a manifest", target)
		}
	}
}
