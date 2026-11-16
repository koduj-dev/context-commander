package contextpack_test

import (
	"strings"
	"testing"

	"github.com/koduj-dev/context-commander/internal/contextpack"
)

func paths(p contextpack.Pack) []string {
	out := make([]string, len(p.Artifacts))
	for i, a := range p.Artifacts {
		out[i] = a.LogicalPath
	}
	return out
}

func noteFor(p contextpack.Pack, ref string) string {
	for _, n := range p.Budget.Truncated {
		if n.Ref == ref {
			return n.Reason
		}
	}
	return ""
}

// A manifest replaces the convention: its entries in its order, a prefix
// expanded, max_bytes cutting a long file instead of dropping it, the
// manifest's budget as the default, and the manifest itself never carried.
func TestManifestSelectsAndBudgets(t *testing.T) {
	f := newFixture(t)
	f.writeArtifact("context/notes.md", "convention would pick this")
	f.writeArtifact("docs/architecture.md", strings.Repeat("é", 3000)) // 6000 bytes
	f.writeArtifact("docs/adr/0001.md", "first decision")
	f.writeArtifact("docs/adr/0002.md", "second decision")
	f.writeArtifact("context/overview.md", "what the project is")
	f.writeArtifact(contextpack.ManifestPath, `{
	  "version": 1,
	  "budget": 50000,
	  "read": [
	    {"path": "context/overview.md", "why": "start here"},
	    {"path": "docs/architecture.md", "max_bytes": 1001},
	    {"prefix": "docs/adr/"},
	    {"path": "docs/missing.md"}
	  ]
	}`)

	pack, err := f.pack.Bootstrap(f.ctx, f.principalID, f.projectID, contextpack.Options{})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(paths(pack), ",")
	if !strings.HasPrefix(got, "context/overview.md,docs/architecture.md,") ||
		!strings.Contains(got, "docs/adr/0001.md") || !strings.Contains(got, "docs/adr/0002.md") ||
		strings.Contains(got, "context/notes.md") || strings.Contains(got, "manifest.json") {
		t.Fatalf("selected %s", got)
	}
	if pack.Artifacts[0].Why != "start here" {
		t.Errorf("why = %q", pack.Artifacts[0].Why)
	}
	arch := pack.Artifacts[1]
	if !arch.Truncated || len(arch.Content) != 1000 || arch.SizeBytes != 6000 {
		t.Errorf("architecture: truncated %v, %d bytes of %d", arch.Truncated, len(arch.Content), arch.SizeBytes)
	}
	if pack.Budget.LimitBytes != 50000 {
		t.Errorf("budget %d, want the manifest's 50000", pack.Budget.LimitBytes)
	}
	if noteFor(pack, "docs/missing.md") == "" {
		t.Errorf("a missing entry is not reported: %+v", pack.Budget.Truncated)
	}

	// The caller's own budget still wins.
	pack, err = f.pack.Bootstrap(f.ctx, f.principalID, f.projectID, contextpack.Options{Budget: 4096})
	if err != nil {
		t.Fatal(err)
	}
	if pack.Budget.LimitBytes != 4096 {
		t.Errorf("budget %d, want the caller's 4096", pack.Budget.LimitBytes)
	}
}

// A manifest that cannot be used is reported, and the convention takes over.
func TestABrokenManifestFallsBackToTheConvention(t *testing.T) {
	f := newFixture(t)
	f.writeArtifact("context/overview.md", "what the project is")
	f.writeArtifact(contextpack.ManifestPath, `{"version": 1, "read": [{"path": "a.md", "max_byte": 10}]}`)

	pack, err := f.pack.Bootstrap(f.ctx, f.principalID, f.projectID, contextpack.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(paths(pack), ","); got != "context/overview.md" {
		t.Errorf("selected %s", got)
	}
	if reason := noteFor(pack, contextpack.ManifestPath); !strings.Contains(reason, "manifest ignored") || !strings.Contains(reason, "max_byte") {
		t.Errorf("note %q", reason)
	}
}

func TestParseManifest(t *testing.T) {
	for _, bad := range []string{
		`{"version": 2, "read": []}`,
		`{"version": 1, "read": [{"path": "a", "prefix": "b"}]}`,
		`{"version": 1, "read": [{}]}`,
		`{"version": 1, "read": [{"path": "a", "max_bytes": -1}]}`,
		`{"version": 1, "budget": -5, "read": []}`,
		`not json`,
	} {
		if _, err := contextpack.ParseManifest([]byte(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	if _, err := contextpack.ParseManifest([]byte(`{"version": 1, "read": [{"prefix": "docs/"}]}`)); err != nil {
		t.Errorf("refused a valid manifest: %v", err)
	}
}
