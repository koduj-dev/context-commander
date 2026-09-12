package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/koduj-dev/context-commander/internal/skills"
)

// `ccd sync-skills` is the client-side half of the skill registry, and the
// one docs/adr/0012 reserved: "the server never writes to a client's
// filesystem ... a client-side adapter does the actual `.claude/skills/`-
// shaped write". The server hands out bytes; deciding where they land on
// this machine is this command's job and nobody else's.
//
// It is incremental by construction. A SkillVersion is immutable, so the
// version id a binding points at is a complete answer to "is this the same
// content I already have" — the manifest written alongside the files
// records it, and a sync downloads only the skills whose version changed.
// The checksum is then verified against what was downloaded, which
// internal/skills.GetVersion already does.

const (
	// syncManifestName is written inside the skill directory, so a
	// directory can be moved or deleted wholesale without leaving state
	// behind somewhere else.
	syncManifestName = ".context-commander.json"
	// skillFileName matches the layout Claude Code expects:
	// <dir>/<skill>/SKILL.md.
	skillFileName = "SKILL.md"

	defaultSkillDir = ".claude/skills"
)

// syncedSkill is one entry of the manifest: what was written, where, and
// which immutable version it came from.
type syncedSkill struct {
	SkillID   string `json:"skill_id"`
	VersionID string `json:"version_id"`
	Name      string `json:"name"`
	Dir       string `json:"dir"`
	Checksum  string `json:"checksum"`
}

type syncManifest struct {
	// Skills keyed by skill id — a skill keeps its identity across
	// versions, which is exactly what makes "the version changed"
	// expressible.
	Skills map[string]syncedSkill `json:"skills"`
}

// syncAction is what the plan decided for one skill.
type syncAction int

const (
	actionUnchanged syncAction = iota
	actionWrite                // not present locally
	actionUpdate               // present, but the binding points at a different version
	actionPrune                // written before, no longer effective for this project
)

func (a syncAction) String() string {
	switch a {
	case actionWrite:
		return "write"
	case actionUpdate:
		return "update"
	case actionPrune:
		return "prune"
	default:
		return "unchanged"
	}
}

// syncStep is one planned action. Name and Dir are only known up front for
// entries the manifest already describes; for a write they are filled in
// after the download, because the skill's name lives on the package.
type syncStep struct {
	Action    syncAction
	SkillID   uuid.UUID
	VersionID uuid.UUID
	Name      string
	Dir       string
}

// planSync decides what to do, without touching the network or the disk —
// which is what makes it testable, and why it takes bindings rather than
// content. Everything expensive happens afterwards, only for the steps that
// need it.
func planSync(effective []skills.SkillBinding, manifest syncManifest) []syncStep {
	steps := make([]syncStep, 0, len(effective))
	seen := make(map[string]bool, len(effective))

	for _, b := range effective {
		skillID := b.SkillID.String()
		seen[skillID] = true

		entry, known := manifest.Skills[skillID]
		switch {
		case !known:
			steps = append(steps, syncStep{Action: actionWrite, SkillID: b.SkillID, VersionID: b.SkillVersionID})
		case entry.VersionID != b.SkillVersionID.String():
			steps = append(steps, syncStep{
				Action: actionUpdate, SkillID: b.SkillID, VersionID: b.SkillVersionID,
				Name: entry.Name, Dir: entry.Dir,
			})
		default:
			steps = append(steps, syncStep{
				Action: actionUnchanged, SkillID: b.SkillID, VersionID: b.SkillVersionID,
				Name: entry.Name, Dir: entry.Dir,
			})
		}
	}

	// Anything this command wrote before that the project no longer binds.
	// Only ever entries from the manifest: a file this command did not put
	// there is not its business to remove.
	for skillID, entry := range manifest.Skills {
		if seen[skillID] {
			continue
		}
		id, err := uuid.Parse(skillID)
		if err != nil {
			continue // a hand-edited manifest; leave the directory alone
		}
		steps = append(steps, syncStep{Action: actionPrune, SkillID: id, Name: entry.Name, Dir: entry.Dir})
	}

	// Stable output: a sync that prints its plan in a different order every
	// run is hard to diff and hard to trust.
	sort.Slice(steps, func(i, j int) bool {
		if steps[i].Action != steps[j].Action {
			return steps[i].Action < steps[j].Action
		}
		return steps[i].SkillID.String() < steps[j].SkillID.String()
	})
	return steps
}

// skillDirName turns a registry-supplied skill name into a single safe path
// component.
//
// This is a security boundary, not cosmetics: skill names come from a
// registry that other people publish into, and this command writes to the
// caller's filesystem. A name like "../../.ssh" or "a/b" must never become
// a path. Everything outside a conservative allowlist collapses to "-", and
// a name that survives as empty, "." or ".." is replaced by the skill id —
// which is always a safe component.
//
// The skill id's first octet is appended whenever sanitizing changed the
// name, so two different skills that sanitize to the same thing cannot
// fight over one directory.
func skillDirName(name string, skillID uuid.UUID) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	cleaned := strings.Trim(b.String(), "-.")
	if cleaned == "" || cleaned == "." || cleaned == ".." {
		return "skill-" + skillID.String()
	}
	if cleaned != name {
		return cleaned + "-" + skillID.String()[:8]
	}
	return cleaned
}

func newSyncSkillsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync-skills",
		Short: "Write a project's effective skills into a local skill directory (default .claude/skills)",
		Long: "Resolves the skills effective for a project and writes each one as <dir>/<skill>/SKILL.md.\n\n" +
			"Only skills whose version changed are downloaded — a SkillVersion is immutable, so the version id\n" +
			"recorded in the directory's manifest answers \"do I already have this\" without fetching anything.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			sc, err := setupSkillContext(cmd)
			if err != nil {
				return err
			}
			defer sc.pool.Close()

			dir, _ := cmd.Flags().GetString("dir")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			prune, _ := cmd.Flags().GetBool("prune")
			out := cmd.OutOrStdout()

			effective, err := sc.svc.ListEffective(cmd.Context(), sc.principalID, sc.project.ID)
			if err != nil {
				return err
			}
			manifest, err := readSyncManifest(dir)
			if err != nil {
				return err
			}

			steps := planSync(effective, manifest)
			changed := 0
			for _, step := range steps {
				switch step.Action {
				case actionUnchanged:
					_, _ = fmt.Fprintf(out, "  unchanged  %s\n", step.Name)

				case actionWrite, actionUpdate:
					changed++
					// Downloaded only now, and only for these: the whole
					// point of planning first.
					v, err := sc.svc.GetVersion(cmd.Context(), sc.principalID, step.VersionID)
					if err != nil {
						return fmt.Errorf("fetch skill %s: %w", step.SkillID, err)
					}
					target := skillDirName(v.Skill.Name, v.Skill.ID)
					_, _ = fmt.Fprintf(out, "  %-9s  %s -> %s/%s (v%d)\n",
						step.Action, v.Skill.Name, target, skillFileName, v.Version.Version)
					if dryRun {
						continue
					}
					if err := writeSkillFile(dir, target, v.Content); err != nil {
						return err
					}
					manifest.Skills[step.SkillID.String()] = syncedSkill{
						SkillID: step.SkillID.String(), VersionID: step.VersionID.String(),
						Name: v.Skill.Name, Dir: target, Checksum: v.Version.Checksum,
					}

				case actionPrune:
					if !prune {
						_, _ = fmt.Fprintf(out, "  stale      %s (no longer effective; pass --prune to remove)\n", step.Name)
						continue
					}
					changed++
					_, _ = fmt.Fprintf(out, "  prune      %s (%s)\n", step.Name, step.Dir)
					if dryRun {
						continue
					}
					if err := removeSkillDir(dir, step.Dir); err != nil {
						return err
					}
					delete(manifest.Skills, step.SkillID.String())
				}
			}

			if dryRun {
				_, _ = fmt.Fprintf(out, "\ndry run — %d skill(s) would change, nothing was written\n", changed)
				return nil
			}
			if err := writeSyncManifest(dir, manifest); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(out, "\n%d skill(s) in %s, %d changed\n", len(manifest.Skills), dir, changed)
			return nil
		},
	}
	cmd.Flags().String("dir", defaultSkillDir, "directory to write skills into")
	cmd.Flags().Bool("dry-run", false, "print what would change without writing anything")
	cmd.Flags().Bool("prune", false, "remove skills this command wrote that the project no longer binds")
	skillFlags(cmd)
	return cmd
}

func readSyncManifest(dir string) (syncManifest, error) {
	m := syncManifest{Skills: map[string]syncedSkill{}}
	raw, err := os.ReadFile(filepath.Join(dir, syncManifestName)) //nolint:gosec // operator-supplied --dir, not untrusted input
	if errors.Is(err, fs.ErrNotExist) {
		return m, nil // first sync into this directory
	}
	if err != nil {
		return m, fmt.Errorf("read %s: %w", syncManifestName, err)
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return m, fmt.Errorf("parse %s (delete it to resync from scratch): %w", syncManifestName, err)
	}
	if m.Skills == nil {
		m.Skills = map[string]syncedSkill{}
	}
	return m, nil
}

func writeSyncManifest(dir string, m syncManifest) error {
	if err := os.MkdirAll(dir, 0o750); err != nil { //nolint:gosec // operator-supplied --dir
		return fmt.Errorf("create %s: %w", dir, err)
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, syncManifestName), append(raw, '\n'), 0o600)
}

func writeSkillFile(dir, target string, content []byte) error {
	skillDir := filepath.Join(dir, target)
	if err := os.MkdirAll(skillDir, 0o750); err != nil { //nolint:gosec // target is sanitized by skillDirName
		return fmt.Errorf("create %s: %w", skillDir, err)
	}
	return os.WriteFile(filepath.Join(skillDir, skillFileName), content, 0o600)
}

func removeSkillDir(dir, target string) error {
	// Defence in depth against a hand-edited manifest: whatever the entry
	// claims, only ever remove a single component directly inside dir.
	if target == "" || target != filepath.Base(target) || target == "." || target == ".." {
		return fmt.Errorf("refusing to remove %q: the manifest names something that is not a plain directory inside %s", target, dir)
	}
	return os.RemoveAll(filepath.Join(dir, target))
}
