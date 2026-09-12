package contextpack_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/artifacts"
	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/contextpack"
	"github.com/koduj-dev/context-commander/internal/crypto"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/dbtest"
	"github.com/koduj-dev/context-commander/internal/events"
	"github.com/koduj-dev/context-commander/internal/gates"
	"github.com/koduj-dev/context-commander/internal/handoffs"
	"github.com/koduj-dev/context-commander/internal/installation"
	"github.com/koduj-dev/context-commander/internal/organizations"
	"github.com/koduj-dev/context-commander/internal/paging"
	"github.com/koduj-dev/context-commander/internal/projects"
	"github.com/koduj-dev/context-commander/internal/skills"
	"github.com/koduj-dev/context-commander/internal/storage/filesystem"
)

// fixture wires every service the pack composes — the same set
// adapters/cli/serve.go builds. A pack test that stubbed any of them would
// prove only that the composition compiles; the point of this package is
// that the real domains compose into something an agent can use.
type fixture struct {
	t         *testing.T
	ctx       context.Context
	ownerPool *db.Pool
	appPool   *db.Pool

	pack      *contextpack.Service
	artifacts *artifacts.Service
	skills    *skills.Service
	handoffs  *handoffs.Service

	projectID   uuid.UUID
	principalID uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	dsns := dbtest.New(t)

	ownerPool, err := db.NewPool(ctx, dsns.Owner)
	if err != nil {
		t.Fatalf("connect as cc_owner: %v", err)
	}
	appPool, err := db.NewPool(ctx, dsns.App)
	if err != nil {
		t.Fatalf("connect as cc_app: %v", err)
	}

	rootKeyB64, err := crypto.GenerateRootKeyBase64()
	if err != nil {
		t.Fatalf("GenerateRootKeyBase64: %v", err)
	}
	t.Setenv("CC_ROOT_KEY", rootKeyB64)
	root, err := crypto.NewEnvRootKeyProvider("CC_ROOT_KEY")
	if err != nil {
		t.Fatalf("NewEnvRootKeyProvider: %v", err)
	}
	cryptoProvider := crypto.NewProvider(root)

	store, err := filesystem.New(t.TempDir())
	if err != nil {
		t.Fatalf("filesystem.New: %v", err)
	}

	projStore := projects.NewStore()
	authz := auth.NewAuthzService(appPool, projStore)
	artifactsSvc := artifacts.NewService(appPool, cryptoProvider, store, authz, gates.NewEngine(appPool))
	skillsSvc := skills.NewService(appPool, authz, organizations.NewStore(), projStore, store, installation.NewStore())
	handoffsSvc := handoffs.NewService(appPool, cryptoProvider, authz)
	eventsSvc := events.NewService(appPool, authz)

	f := &fixture{
		t: t, ctx: ctx, ownerPool: ownerPool, appPool: appPool,
		pack:      contextpack.NewService(appPool, projStore, authz, artifactsSvc, skillsSvc, handoffsSvc, eventsSvc),
		artifacts: artifactsSvc, skills: skillsSvc, handoffs: handoffsSvc,
	}
	f.principalID, f.projectID = f.seedProject(projects.RoleContributor)
	return f
}

// seedProject inserts a principal and a project it holds role on, returning
// both IDs. Seeding goes through the owner pool (RLS-exempt) because the
// behavior under test is the pack's, not the fixture's.
func (f *fixture) seedProject(role projects.Role) (principalID, projectID uuid.UUID) {
	f.t.Helper()
	principalID, projectID = uuid.New(), uuid.New()

	err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		userID := uuid.New()
		if _, err := tx.Exec(f.ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`,
			userID, userID.String()+"@example.test", "Fixture User"); err != nil {
			return err
		}
		if _, err := tx.Exec(f.ctx, `INSERT INTO principals (id, kind, user_id) VALUES ($1, 'user', $2)`,
			principalID, userID); err != nil {
			return err
		}
		namespaceID := uuid.New()
		if _, err := tx.Exec(f.ctx,
			`INSERT INTO namespaces (id, kind, slug, owner_user_id) VALUES ($1, 'personal', $2, $3)`,
			namespaceID, "ns-"+namespaceID.String()[:8], userID); err != nil {
			return err
		}
		if _, err := tx.Exec(f.ctx, `INSERT INTO projects (id, namespace_id, slug, name) VALUES ($1, $2, 'cc', 'Context Commander')`,
			projectID, namespaceID); err != nil {
			return err
		}
		_, err := tx.Exec(f.ctx, `INSERT INTO project_members (project_id, principal_id, role) VALUES ($1, $2, $3)`,
			projectID, principalID, role)
		return err
	})
	if err != nil {
		f.t.Fatalf("seed project: %v", err)
	}
	return principalID, projectID
}

// addPrincipal puts a second principal on the fixture's project with the
// given role — the second client in a handoff.
func (f *fixture) addPrincipal(role projects.Role) uuid.UUID {
	f.t.Helper()
	principalID := uuid.New()
	err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		userID := uuid.New()
		if _, err := tx.Exec(f.ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`,
			userID, userID.String()+"@example.test", "Second"); err != nil {
			return err
		}
		if _, err := tx.Exec(f.ctx, `INSERT INTO principals (id, kind, user_id) VALUES ($1, 'user', $2)`, principalID, userID); err != nil {
			return err
		}
		_, err := tx.Exec(f.ctx, `INSERT INTO project_members (project_id, principal_id, role) VALUES ($1, $2, $3)`,
			f.projectID, principalID, role)
		return err
	})
	if err != nil {
		f.t.Fatalf("add principal: %v", err)
	}
	return principalID
}

func (f *fixture) writeArtifact(path, content string) uuid.UUID {
	f.t.Helper()
	v, err := f.artifacts.Write(f.ctx, f.principalID, nil, f.projectID, path, "text/markdown", []byte(content), 0)
	if err != nil {
		f.t.Fatalf("write artifact %q: %v", path, err)
	}
	return v.ArtifactID
}

func (f *fixture) publishAndInstallSkill(name, content string) {
	f.t.Helper()
	v, err := f.skills.Publish(f.ctx, f.principalID, f.projectID, nil, name, "acme", "local", []byte(content), nil)
	if err != nil {
		f.t.Fatalf("publish skill %q: %v", name, err)
	}
	userID := f.userIDOf(f.principalID)
	if _, err := f.skills.Install(f.ctx, f.principalID, userID, f.projectID,
		skills.ScopeProject, &f.projectID, v.SkillID, v.ID, false, true); err != nil {
		f.t.Fatalf("install skill %q: %v", name, err)
	}
}

func (f *fixture) userIDOf(principalID uuid.UUID) uuid.UUID {
	f.t.Helper()
	var userID uuid.UUID
	err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, `SELECT user_id FROM principals WHERE id = $1`, principalID).Scan(&userID)
	})
	if err != nil {
		f.t.Fatalf("resolve user id: %v", err)
	}
	return userID
}

// TestBootstrapAssemblesEverything is the shape of the whole Phase 1 claim:
// one call returns identity, role, the work to continue, the instructions
// to follow, the files to read, and where to pick up the event stream.
func TestBootstrapAssemblesEverything(t *testing.T) {
	f := newFixture(t)

	notesID := f.writeArtifact("notes/decision.md", "we chose keyset pagination")
	f.writeArtifact(contextpack.ConventionalArtifactPrefix+"overview.md", "this project migrates ES mappings")
	f.writeArtifact("unrelated/scratch.md", "should not be selected")
	f.publishAndInstallSkill("code-review", "# Code review\nCheck error paths.")

	if _, err := f.handoffs.Create(f.ctx, f.principalID, nil, f.projectID, nil, handoffs.Body{
		Summary:             "mappings half migrated",
		CompletedWork:       "moved products index",
		NextActions:         "move orders index",
		ReferencedArtifacts: []uuid.UUID{notesID},
	}); err != nil {
		t.Fatalf("create handoff: %v", err)
	}

	pack, err := f.pack.Bootstrap(f.ctx, f.principalID, f.projectID, contextpack.Options{})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	if !strings.HasSuffix(pack.Project.Path, "/cc") {
		t.Fatalf("project path = %q, want it to end in /cc", pack.Project.Path)
	}
	if pack.Role != string(projects.RoleContributor) {
		t.Fatalf("role = %q, want CONTRIBUTOR", pack.Role)
	}
	if len(pack.Capabilities) == 0 {
		t.Fatal("capabilities are empty; an agent cannot tell what it may attempt")
	}

	if pack.Handoff == nil {
		t.Fatal("pack has no handoff — the one thing it exists to deliver")
	}
	if pack.Handoff.NextActions != "move orders index" {
		t.Fatalf("handoff next actions = %q, want the decrypted body", pack.Handoff.NextActions)
	}
	if pack.Handoff.Status != handoffs.StatusOpen {
		t.Fatalf("handoff status = %q, want open", pack.Handoff.Status)
	}

	// Skills carry content, not just a binding UUID.
	if len(pack.Skills) != 1 {
		t.Fatalf("skills = %d, want 1", len(pack.Skills))
	}
	if !strings.Contains(pack.Skills[0].Content, "Check error paths") {
		t.Fatalf("skill content = %q, want the published instructions", pack.Skills[0].Content)
	}

	// Selection: the handoff's referenced artifact plus the convention
	// prefix — and nothing else.
	gotPaths := map[string]string{}
	for _, a := range pack.Artifacts {
		gotPaths[a.LogicalPath] = a.Content
	}
	if _, ok := gotPaths["notes/decision.md"]; !ok {
		t.Fatalf("referenced artifact missing from pack; got %v", keys(gotPaths))
	}
	if _, ok := gotPaths[contextpack.ConventionalArtifactPrefix+"overview.md"]; !ok {
		t.Fatalf("convention-selected artifact missing from pack; got %v", keys(gotPaths))
	}
	if _, ok := gotPaths["unrelated/scratch.md"]; ok {
		t.Fatal("pack included an artifact that is neither referenced nor under the convention prefix")
	}

	// The event cursor is what turns a snapshot into something an agent can
	// follow. It must be present whenever anything has happened at all.
	if len(pack.Events.Recent) == 0 {
		t.Fatal("no recent events, though writes and a handoff happened")
	}
	if pack.Events.NextCursor == "" {
		t.Fatal("no event cursor; the agent has no way to continue the stream")
	}

	if len(pack.Budget.Truncated) != 0 {
		t.Fatalf("nothing should have been truncated at the default budget, got %v", pack.Budget.Truncated)
	}
	if pack.Budget.UsedBytes == 0 {
		t.Fatal("budget reports zero bytes used despite carrying content")
	}
}

// TestBootstrapReportsWhatItOmitted is the honesty requirement: an agent
// that cannot tell "there is nothing else" from "there was more and you
// didn't get it" will act confidently on a partial picture.
func TestBootstrapReportsWhatItOmitted(t *testing.T) {
	f := newFixture(t)

	big := strings.Repeat("x", 4096)
	f.writeArtifact(contextpack.ConventionalArtifactPrefix+"big.md", big)

	pack, err := f.pack.Bootstrap(f.ctx, f.principalID, f.projectID, contextpack.Options{Budget: 512})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	if len(pack.Artifacts) != 0 {
		t.Fatalf("artifacts = %d, want 0 — 4096 bytes cannot fit a 512-byte budget", len(pack.Artifacts))
	}
	if len(pack.Budget.Truncated) != 1 {
		t.Fatalf("truncation notes = %d, want 1: %v", len(pack.Budget.Truncated), pack.Budget.Truncated)
	}
	note := pack.Budget.Truncated[0]
	if note.Kind != "artifact" || note.Ref != contextpack.ConventionalArtifactPrefix+"big.md" {
		t.Fatalf("truncation note = %+v, want it to name the omitted artifact", note)
	}
	if note.SizeBytes != int64(len(big)) {
		t.Fatalf("truncation note size = %d, want %d — the caller needs to know what it would cost", note.SizeBytes, len(big))
	}
	// Omitted whole, never cut: half an artifact reads exactly like a
	// complete one.
	if pack.Budget.UsedBytes > pack.Budget.LimitBytes {
		t.Fatalf("used %d bytes against a %d limit", pack.Budget.UsedBytes, pack.Budget.LimitBytes)
	}
}

// TestBootstrapExplicitPathsOverrideConvention: a client that knows what it
// needs says so, and the convention does not add to it.
func TestBootstrapExplicitPathsOverrideConvention(t *testing.T) {
	f := newFixture(t)
	f.writeArtifact(contextpack.ConventionalArtifactPrefix+"overview.md", "conventional")
	f.writeArtifact("specific.md", "asked for")

	pack, err := f.pack.Bootstrap(f.ctx, f.principalID, f.projectID, contextpack.Options{
		ArtifactPaths: []string{"specific.md"},
	})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if len(pack.Artifacts) != 1 || pack.Artifacts[0].LogicalPath != "specific.md" {
		t.Fatalf("artifacts = %v, want exactly [specific.md]", pack.Artifacts)
	}
}

// TestBootstrapMissingPathIsANoteNotAFailure: a client's standing list of
// "the files I usually need" outliving one of them is normal. Losing the
// whole pack over it is not a useful response.
func TestBootstrapMissingPathIsANoteNotAFailure(t *testing.T) {
	f := newFixture(t)
	f.writeArtifact("exists.md", "here")

	pack, err := f.pack.Bootstrap(f.ctx, f.principalID, f.projectID, contextpack.Options{
		ArtifactPaths: []string{"exists.md", "gone.md"},
	})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if len(pack.Artifacts) != 1 {
		t.Fatalf("artifacts = %d, want the one that exists", len(pack.Artifacts))
	}
	found := false
	for _, n := range pack.Budget.Truncated {
		if n.Ref == "gone.md" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the missing path was silently dropped; notes = %v", pack.Budget.Truncated)
	}
}

func TestBootstrapWithoutMembershipIsForbidden(t *testing.T) {
	f := newFixture(t)

	// A principal that exists but holds no role on this project.
	outsider := uuid.New()
	err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		userID := uuid.New()
		if _, err := tx.Exec(f.ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`,
			userID, userID.String()+"@example.test", "Outsider"); err != nil {
			return err
		}
		_, err := tx.Exec(f.ctx, `INSERT INTO principals (id, kind, user_id) VALUES ($1, 'user', $2)`, outsider, userID)
		return err
	})
	if err != nil {
		t.Fatalf("seed outsider: %v", err)
	}

	if _, err := f.pack.Bootstrap(f.ctx, outsider, f.projectID, contextpack.Options{}); !errors.Is(err, contextpack.ErrForbidden) {
		t.Fatalf("Bootstrap as non-member = %v, want ErrForbidden", err)
	}
}

// TestBootstrapSkipSections: a client resuming inside the same session
// wants the handoff and the events, not another copy of the skills it
// loaded an hour ago.
func TestBootstrapSkipSections(t *testing.T) {
	f := newFixture(t)
	f.writeArtifact(contextpack.ConventionalArtifactPrefix+"overview.md", "content")
	f.publishAndInstallSkill("code-review", "instructions")

	pack, err := f.pack.Bootstrap(f.ctx, f.principalID, f.projectID, contextpack.Options{
		SkipSkills: true, SkipArtifacts: true,
	})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if len(pack.Skills) != 0 || len(pack.Artifacts) != 0 {
		t.Fatalf("skipped sections came back: %d skills, %d artifacts", len(pack.Skills), len(pack.Artifacts))
	}
	if pack.Budget.UsedBytes != 0 {
		t.Fatalf("used %d bytes with every content section skipped", pack.Budget.UsedBytes)
	}
}

// TestBootstrapShowsClaimedStatus: the second agent must be able to tell
// "take this" from "someone else already did".
func TestBootstrapShowsClaimedStatus(t *testing.T) {
	f := newFixture(t)
	second := f.addPrincipal(projects.RoleContributor)

	h, err := f.handoffs.Create(f.ctx, f.principalID, nil, f.projectID, nil, handoffs.Body{Summary: "work"})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	if _, err := f.handoffs.Claim(f.ctx, second, nil, f.projectID, h.ID); err != nil {
		t.Fatalf("claim: %v", err)
	}

	pack, err := f.pack.Bootstrap(f.ctx, f.principalID, f.projectID, contextpack.Options{})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if pack.Handoff == nil || pack.Handoff.Status != handoffs.StatusClaimed {
		t.Fatalf("handoff = %+v, want status claimed", pack.Handoff)
	}
	if pack.Handoff.ClaimedAt == nil {
		t.Fatal("claimed handoff has no claimed_at in the pack")
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestConventionSelectionSurvivesNewerUnrelatedWrites is the regression
// test for the review finding that convention-selected artifacts could
// vanish behind unrelated activity.
//
// The selection used to take one page of a listing ordered by updated_at
// across *every* artifact in the project and filter the prefix afterwards.
// A must-read file nobody had touched lately therefore fell off the end of
// that page — silently, with nothing in budget.truncated to say so. Here
// the must-read file is written first and then buried under more unrelated
// writes than a page holds.
func TestConventionSelectionSurvivesNewerUnrelatedWrites(t *testing.T) {
	f := newFixture(t)

	f.writeArtifact(contextpack.ConventionalArtifactPrefix+"instructions.md", "read this first")

	// One more than the default page size of the general listing, so the
	// must-read file cannot be on its first page under any ordering by time.
	for i := range paging.DefaultLimit + 1 {
		f.writeArtifact(fmt.Sprintf("scratch/note-%03d.md", i), "unrelated")
	}

	pack, err := f.pack.Bootstrap(f.ctx, f.principalID, f.projectID, contextpack.Options{})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	var found bool
	for _, a := range pack.Artifacts {
		if a.LogicalPath == contextpack.ConventionalArtifactPrefix+"instructions.md" {
			found = true
			if a.Content != "read this first" {
				t.Fatalf("instructions content = %q", a.Content)
			}
		}
		if strings.HasPrefix(a.LogicalPath, "scratch/") {
			t.Fatalf("pack selected %q, which is outside the convention prefix", a.LogicalPath)
		}
	}
	if !found {
		t.Fatalf("the must-read artifact was dropped behind %d newer unrelated writes; pack carried %d artifacts",
			paging.DefaultLimit+1, len(pack.Artifacts))
	}
}

// TestConventionSelectionReportsOverflow: the cap now counts only artifacts
// under the prefix, and reaching it has to be stated rather than absorbed —
// a pack that quietly stops at a round number is indistinguishable from a
// complete one.
func TestConventionSelectionReportsOverflow(t *testing.T) {
	f := newFixture(t)

	for i := range contextpack.MaxConventionalArtifacts + 1 {
		f.writeArtifact(fmt.Sprintf("%sdoc-%03d.md", contextpack.ConventionalArtifactPrefix, i), "x")
	}

	pack, err := f.pack.Bootstrap(f.ctx, f.principalID, f.projectID, contextpack.Options{})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if len(pack.Artifacts) != contextpack.MaxConventionalArtifacts {
		t.Fatalf("artifacts = %d, want the cap of %d", len(pack.Artifacts), contextpack.MaxConventionalArtifacts)
	}

	var noted bool
	for _, n := range pack.Budget.Truncated {
		if n.Kind == "artifact" && n.Ref == contextpack.ConventionalArtifactPrefix {
			noted = true
		}
	}
	if !noted {
		t.Fatalf("hitting the convention cap was not reported; notes = %v", pack.Budget.Truncated)
	}
}

// TestPackCarriesClaimAttribution is the regression test for the review
// finding that the pack reported a handoff as claimed without saying by
// whom — leaving a client whose own claim response was lost unable to
// recognize its own work.
func TestPackCarriesClaimAttribution(t *testing.T) {
	f := newFixture(t)
	second := f.addPrincipal(projects.RoleContributor)

	h, err := f.handoffs.Create(f.ctx, f.principalID, nil, f.projectID, nil, handoffs.Body{Summary: "work"})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	if _, err := f.handoffs.Claim(f.ctx, second, nil, f.projectID, h.ID); err != nil {
		t.Fatalf("claim: %v", err)
	}

	pack, err := f.pack.Bootstrap(f.ctx, f.principalID, f.projectID, contextpack.Options{})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if pack.Handoff == nil {
		t.Fatal("no handoff in pack")
	}
	if pack.Handoff.ClaimedByPrincipal == nil {
		t.Fatal("pack reports the handoff as claimed but not by whom")
	}
	if *pack.Handoff.ClaimedByPrincipal != second {
		t.Fatalf("claimed_by_principal = %s, want %s", pack.Handoff.ClaimedByPrincipal, second)
	}
}
