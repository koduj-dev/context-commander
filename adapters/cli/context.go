package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	"github.com/koduj-dev/context-commander/internal/artifacts"
	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/contextpack"
	"github.com/koduj-dev/context-commander/internal/crypto"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/events"
	"github.com/koduj-dev/context-commander/internal/gates"
	"github.com/koduj-dev/context-commander/internal/handoffs"
	"github.com/koduj-dev/context-commander/internal/installation"
	"github.com/koduj-dev/context-commander/internal/organizations"
	"github.com/koduj-dev/context-commander/internal/projects"
	"github.com/koduj-dev/context-commander/internal/skills"
	"github.com/koduj-dev/context-commander/internal/storage/filesystem"
)

// newContextCmd exposes the aggregated bootstrap read model on the CLI.
//
// It exists here as well as over MCP for one specific reason: when an agent
// reports that its context looks wrong, the fastest way to find out whether
// the server or the client is at fault is to ask the server the same
// question by hand and read the answer.
func newContextCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "context",
		Short: "Print the assembled starting context for a project (the same read model MCP's context.bootstrap returns)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, principalID, project, pool, err := setupContextService(cmd)
			if err != nil {
				return err
			}
			defer pool.Close()

			budget, _ := cmd.Flags().GetInt("budget")
			eventLimit, _ := cmd.Flags().GetInt("event-limit")
			artifactPaths, _ := cmd.Flags().GetStringArray("artifact-path")
			skipSkills, _ := cmd.Flags().GetBool("skip-skills")
			skipArtifacts, _ := cmd.Flags().GetBool("skip-artifacts")

			pack, err := svc.Bootstrap(cmd.Context(), principalID, project.ID, contextpack.Options{
				Budget:        budget,
				EventLimit:    eventLimit,
				ArtifactPaths: artifactPaths,
				SkipSkills:    skipSkills,
				SkipArtifacts: skipArtifacts,
			})
			if err != nil {
				return err
			}

			if raw, _ := cmd.Flags().GetBool("json"); raw {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(pack)
			}
			printPack(cmd, pack)
			return nil
		},
	}
	cmd.Flags().Int("budget", 0, fmt.Sprintf("max bytes of artifact and skill content to include (default %d, max %d)", contextpack.DefaultBudget, contextpack.MaxBudget))
	cmd.Flags().Int("event-limit", 0, fmt.Sprintf("how many recent events to include (default %d)", contextpack.DefaultEventLimit))
	cmd.Flags().StringArray("artifact-path", nil, "read exactly these artifact logical paths instead of the default selection (repeatable)")
	cmd.Flags().Bool("skip-skills", false, "omit skill content")
	cmd.Flags().Bool("skip-artifacts", false, "omit artifact content")
	cmd.Flags().Bool("json", false, "print the pack as JSON — the same shape the HTTP and MCP adapters return")
	projectAndStorageFlags(cmd)
	return cmd
}

// printPack renders the human-readable summary. Content bodies are
// deliberately NOT printed: a pack can carry a quarter-megabyte of
// artifacts and skills, and dumping it into a terminal buries exactly the
// three lines (role, current handoff, what was truncated) someone running
// this by hand is looking for. --json prints everything.
func printPack(cmd *cobra.Command, p contextpack.Pack) {
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "project: %s (%s)\nrole: %s\n\n", p.Project.Path, p.Project.Name, p.Role)

	if p.Handoff == nil {
		_, _ = fmt.Fprintf(out, "handoff: none open — nothing to take over\n\n")
	} else {
		_, _ = fmt.Fprintf(out, "handoff %s [%s]\n  summary:        %s\n  completed work: %s\n  next actions:   %s\n\n",
			p.Handoff.ID, p.Handoff.Status, p.Handoff.Summary, p.Handoff.CompletedWork, p.Handoff.NextActions)
	}

	_, _ = fmt.Fprintf(out, "skills (%d):\n", len(p.Skills))
	for _, s := range p.Skills {
		_, _ = fmt.Fprintf(out, "  %-30s v%-3d %-12s %d bytes\n", s.Name, s.Version, s.Scope, len(s.Content))
	}

	_, _ = fmt.Fprintf(out, "artifacts (%d):\n", len(p.Artifacts))
	for _, a := range p.Artifacts {
		_, _ = fmt.Fprintf(out, "  %-40s v%-3d %d bytes\n", a.LogicalPath, a.Version, a.SizeBytes)
	}

	_, _ = fmt.Fprintf(out, "events: %d recent", len(p.Events.Recent))
	if p.Events.NextCursor != "" {
		_, _ = fmt.Fprintf(out, ", continue from cursor %s", p.Events.NextCursor)
	}
	_, _ = fmt.Fprintln(out)

	_, _ = fmt.Fprintf(out, "\nbudget: %d of %d bytes used\n", p.Budget.UsedBytes, p.Budget.LimitBytes)
	if len(p.Budget.Truncated) == 0 {
		return
	}
	_, _ = fmt.Fprintln(out, "not included:")
	for _, t := range p.Budget.Truncated {
		_, _ = fmt.Fprintf(out, "  %-9s %-40s %s\n", t.Kind, t.Ref, t.Reason)
	}
}

// setupContextService constructs every service the pack composes. It is the
// one place in this package that wires more than a single domain, because
// the pack is the one operation that reads across all of them — the same
// set adapters/cli/serve.go builds for the server.
func setupContextService(cmd *cobra.Command) (*contextpack.Service, uuid.UUID, projects.Project, *db.Pool, error) {
	dsn, err := resolveDatabaseURL(cmd)
	if err != nil {
		return nil, uuid.Nil, projects.Project{}, nil, err
	}
	token, err := resolveToken(cmd)
	if err != nil {
		return nil, uuid.Nil, projects.Project{}, nil, err
	}
	projectPath, _ := cmd.Flags().GetString("project")
	if projectPath == "" {
		return nil, uuid.Nil, projects.Project{}, nil, fmt.Errorf("--project is required")
	}

	pool, err := db.NewPool(cmd.Context(), dsn)
	if err != nil {
		return nil, uuid.Nil, projects.Project{}, nil, err
	}

	principalID, _, err := authenticate(cmd.Context(), pool, token)
	if err != nil {
		pool.Close()
		return nil, uuid.Nil, projects.Project{}, nil, err
	}

	projStore := projects.NewStore()
	var project projects.Project
	err = db.WithPrincipal(cmd.Context(), pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		project, err = projStore.Resolve(cmd.Context(), tx, projectPath)
		return err
	})
	if err != nil {
		pool.Close()
		return nil, uuid.Nil, projects.Project{}, nil, fmt.Errorf("resolve project %q: %w", projectPath, err)
	}

	storageDir, _ := cmd.Flags().GetString("storage-dir")
	if storageDir == "" {
		storageDir = os.Getenv(storageDirEnv)
	}
	if storageDir == "" {
		storageDir = defaultStorageDir
	}
	store, err := filesystem.New(storageDir)
	if err != nil {
		pool.Close()
		return nil, uuid.Nil, projects.Project{}, nil, err
	}

	root, err := crypto.NewEnvRootKeyProvider(rootKeyEnv)
	if err != nil {
		pool.Close()
		return nil, uuid.Nil, projects.Project{}, nil, err
	}
	cryptoProvider := crypto.NewProvider(root)
	authz := auth.NewAuthzService(pool, projStore)
	svc := contextpack.NewService(
		pool, projStore, authz,
		artifacts.NewService(pool, cryptoProvider, store, authz, gates.NewEngine(pool)),
		skills.NewService(pool, authz, organizations.NewStore(), projStore, store, installation.NewStore()),
		handoffs.NewService(pool, cryptoProvider, authz),
		events.NewService(pool, authz),
	)
	return svc, principalID, project, pool, nil
}
