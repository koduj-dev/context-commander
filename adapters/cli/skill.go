package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/installation"
	"github.com/koduj-dev/context-commander/internal/organizations"
	"github.com/koduj-dev/context-commander/internal/projects"
	"github.com/koduj-dev/context-commander/internal/skills"
	"github.com/koduj-dev/context-commander/internal/storage/filesystem"
)

func newSkillCmd() *cobra.Command {
	skillCmd := &cobra.Command{
		Use:   "skill",
		Short: "Publish, install, and list skills (docs/adr/0012-skill-registry-supply-chain.md)",
	}
	skillCmd.AddCommand(newSkillPublishCmd(), newSkillGetCmd(), newSkillSearchCmd(), newSkillVersionsCmd(), newSkillPromoteCmd(), newSkillInstallCmd(), newSkillOverrideCmd(), newSkillPinCmd(true), newSkillPinCmd(false), newSkillListEffectiveCmd())
	return skillCmd
}

// skillContext mirrors artifactContext (adapters/cli/artifact.go) — same
// authenticate-then-resolve-project-then-build-Service wiring, no business
// logic of its own.
type skillContext struct {
	svc         *skills.Service
	principalID uuid.UUID
	userID      uuid.UUID
	project     projects.Project
	pool        *db.Pool
}

func setupSkillContext(cmd *cobra.Command) (*skillContext, error) {
	dsn, err := resolveDatabaseURL(cmd)
	if err != nil {
		return nil, err
	}
	token, err := resolveToken(cmd)
	if err != nil {
		return nil, err
	}
	projectPath, _ := cmd.Flags().GetString("project")
	if projectPath == "" {
		return nil, fmt.Errorf("--project is required")
	}

	pool, err := db.NewPool(cmd.Context(), dsn)
	if err != nil {
		return nil, err
	}

	principalID, _, err := authenticate(cmd.Context(), pool, token)
	if err != nil {
		return nil, err
	}

	projStore := projects.NewStore()
	var project projects.Project
	var userID uuid.UUID
	err = db.WithPrincipal(cmd.Context(), pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		project, err = projStore.Resolve(cmd.Context(), tx, projectPath)
		if err != nil {
			return err
		}
		var nullableUserID *uuid.UUID
		if err := tx.QueryRow(cmd.Context(), `SELECT user_id FROM principals WHERE id = $1`, principalID).Scan(&nullableUserID); err != nil {
			return err
		}
		if nullableUserID != nil {
			userID = *nullableUserID
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("resolve project %q: %w", projectPath, err)
	}

	storageDir, _ := cmd.Flags().GetString("storage-dir")
	if storageDir == "" {
		storageDir = os.Getenv(storageDirEnv)
	}
	if storageDir == "" {
		storageDir = defaultStorageDir
	}
	if err := os.MkdirAll(storageDir, 0o750); err != nil { //nolint:gosec // operator-supplied --storage-dir/$CC_STORAGE_DIR, not untrusted network input
		return nil, fmt.Errorf("create storage dir %q: %w", storageDir, err)
	}
	store, err := filesystem.New(storageDir)
	if err != nil {
		return nil, err
	}

	authz := auth.NewAuthzService(pool, projStore)
	svc := skills.NewService(pool, authz, organizations.NewStore(), projStore, store, installation.NewStore())

	return &skillContext{svc: svc, principalID: principalID, userID: userID, project: project, pool: pool}, nil
}

func skillFlags(cmd *cobra.Command) {
	cmd.Flags().String("project", "", `project path "namespace/slug" — used to authorize the operation (required)`)
	cmd.Flags().String("storage-dir", "", fmt.Sprintf("filesystem ObjectStore root (defaults to $%s, or %q)", storageDirEnv, defaultStorageDir))
	databaseURLFlag(cmd)
	tokenFlag(cmd)
}

func newSkillPublishCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Publish a new Skill or SkillVersion — always a pure insert, never an update to existing content",
		RunE: func(cmd *cobra.Command, _ []string) error {
			sc, err := setupSkillContext(cmd)
			if err != nil {
				return err
			}
			defer sc.pool.Close()

			var skillID *uuid.UUID
			if raw, _ := cmd.Flags().GetString("skill-id"); raw != "" {
				id, err := uuid.Parse(raw)
				if err != nil {
					return fmt.Errorf("--skill-id: %w", err)
				}
				skillID = &id
			}
			name, _ := cmd.Flags().GetString("name")
			author, _ := cmd.Flags().GetString("author")
			origin, _ := cmd.Flags().GetString("origin")
			file, _ := cmd.Flags().GetString("file")

			var content []byte
			if file == "" || file == "-" {
				content, err = io.ReadAll(cmd.InOrStdin())
			} else {
				content, err = os.ReadFile(file) //nolint:gosec // operator-supplied CLI path, not untrusted input
			}
			if err != nil {
				return fmt.Errorf("read content: %w", err)
			}

			v, err := sc.svc.Publish(cmd.Context(), sc.principalID, sc.project.ID, skillID, name, author, origin, content, nil)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "published skill_id=%s version=%d id=%s checksum=%s\n", v.SkillID, v.Version, v.ID, v.Checksum)
			return nil
		},
	}
	cmd.Flags().String("skill-id", "", "existing skill UUID to publish a new version under (omit to create a new skill)")
	cmd.Flags().String("name", "", "skill name (required when --skill-id is omitted)")
	cmd.Flags().String("author", "", "skill author")
	cmd.Flags().String("origin", "", "skill origin")
	cmd.Flags().String("file", "", "file to read content from (default: stdin)")
	skillFlags(cmd)
	return cmd
}

func newSkillInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install",
		Short: `Create a SkillBinding — "project X uses skill Y" (or organization/installation)`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			sc, err := setupSkillContext(cmd)
			if err != nil {
				return err
			}
			defer sc.pool.Close()

			scope, _ := cmd.Flags().GetString("scope")
			skillID, err := uuid.Parse(mustFlag(cmd, "skill-id"))
			if err != nil {
				return fmt.Errorf("--skill-id: %w", err)
			}
			versionID, err := uuid.Parse(mustFlag(cmd, "skill-version-id"))
			if err != nil {
				return fmt.Errorf("--skill-version-id: %w", err)
			}
			var scopeRef *uuid.UUID
			if raw, _ := cmd.Flags().GetString("scope-ref"); raw != "" {
				id, err := uuid.Parse(raw)
				if err != nil {
					return fmt.Errorf("--scope-ref: %w", err)
				}
				scopeRef = &id
			}
			managed, _ := cmd.Flags().GetBool("managed")
			overridable, _ := cmd.Flags().GetBool("overridable")

			b, err := sc.svc.Install(cmd.Context(), sc.principalID, sc.userID, sc.project.ID, skills.Scope(scope), scopeRef, skillID, versionID, managed, overridable)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "installed binding_id=%s scope=%s\n", b.ID, b.Scope)
			return nil
		},
	}
	cmd.Flags().String("scope", "project", `"installation" | "organization" | "project"`)
	cmd.Flags().String("scope-ref", "", "organization or project UUID the binding applies to (omit for installation scope)")
	cmd.Flags().String("skill-id", "", "skill UUID (required)")
	cmd.Flags().String("skill-version-id", "", "skill version UUID to pin to (required)")
	cmd.Flags().Bool("managed", false, "")
	cmd.Flags().Bool("overridable", true, "whether a narrower scope may shadow this binding")
	skillFlags(cmd)
	return cmd
}

func newSkillOverrideCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "override",
		Short: "Create a project-scope binding shadowing an organization/installation binding of the same skill",
		RunE: func(cmd *cobra.Command, _ []string) error {
			sc, err := setupSkillContext(cmd)
			if err != nil {
				return err
			}
			defer sc.pool.Close()

			skillID, err := uuid.Parse(mustFlag(cmd, "skill-id"))
			if err != nil {
				return fmt.Errorf("--skill-id: %w", err)
			}
			versionID, err := uuid.Parse(mustFlag(cmd, "skill-version-id"))
			if err != nil {
				return fmt.Errorf("--skill-version-id: %w", err)
			}
			managed, _ := cmd.Flags().GetBool("managed")
			overridable, _ := cmd.Flags().GetBool("overridable")

			b, err := sc.svc.Override(cmd.Context(), sc.principalID, sc.project.ID, skillID, versionID, managed, overridable)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "override binding_id=%s\n", b.ID)
			return nil
		},
	}
	cmd.Flags().String("skill-id", "", "skill UUID (required)")
	cmd.Flags().String("skill-version-id", "", "skill version UUID to pin to (required)")
	cmd.Flags().Bool("managed", false, "")
	cmd.Flags().Bool("overridable", true, "whether a still-narrower scope may shadow this override")
	skillFlags(cmd)
	return cmd
}

func newSkillPinCmd(pinned bool) *cobra.Command {
	use, short := "pin", "Pin a project-scope SkillBinding"
	if !pinned {
		use, short = "unpin", "Unpin a project-scope SkillBinding"
	}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, _ []string) error {
			sc, err := setupSkillContext(cmd)
			if err != nil {
				return err
			}
			defer sc.pool.Close()

			bindingID, err := uuid.Parse(mustFlag(cmd, "binding-id"))
			if err != nil {
				return fmt.Errorf("--binding-id: %w", err)
			}
			if pinned {
				err = sc.svc.Pin(cmd.Context(), sc.principalID, sc.project.ID, bindingID)
			} else {
				err = sc.svc.Unpin(cmd.Context(), sc.principalID, sc.project.ID, bindingID)
			}
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s binding_id=%s\n", use+"ned", bindingID)
			return nil
		},
	}
	cmd.Flags().String("binding-id", "", "project-scope SkillBinding UUID (required)")
	skillFlags(cmd)
	return cmd
}

func newSkillListEffectiveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list-effective",
		Short: "List the effective skill bindings for a project (installation + organization + project scope)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			sc, err := setupSkillContext(cmd)
			if err != nil {
				return err
			}
			defer sc.pool.Close()

			list, err := sc.svc.ListEffective(cmd.Context(), sc.principalID, sc.project.ID)
			if err != nil {
				return err
			}
			if len(list) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "(no effective skill bindings)")
				return nil
			}
			for _, b := range list {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%-14s skill=%s version=%s binding=%s\n", b.Scope, b.SkillID, b.SkillVersionID, b.ID)
			}
			return nil
		},
	}
	skillFlags(cmd)
	return cmd
}

// mustFlag reads a required string flag, returning "" if unset — callers
// pass it straight to uuid.Parse, whose error on "" ("invalid UUID length")
// is a clear enough message for a missing required CLI flag.
func mustFlag(cmd *cobra.Command, name string) string {
	v, _ := cmd.Flags().GetString(name)
	return v
}

// newSkillGetCmd is the read half `ccd skill install` never had: until now
// the CLI could publish and install a skill and had no way to print one
// back out.
func newSkillGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <skill-version-id>",
		Short: "Print a skill version's content (verified against its published checksum)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sc, err := setupSkillContext(cmd)
			if err != nil {
				return err
			}
			defer sc.pool.Close()

			skillVersionID, err := uuid.Parse(args[0])
			if err != nil {
				return fmt.Errorf("invalid skill version id: %w", err)
			}
			v, err := sc.svc.GetVersion(cmd.Context(), sc.principalID, sc.project.ID, skillVersionID)
			if err != nil {
				return err
			}

			// Metadata to stderr, content to stdout — so `ccd skill get ID >
			// skill.md` writes the package and nothing else, while a human
			// running it in a terminal still sees what they just fetched.
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "skill: %s (version %d, %s, trust=%s)\n",
				v.Skill.Name, v.Version.Version, v.Version.ID, v.Version.TrustStatus)
			_, _ = cmd.OutOrStdout().Write(v.Content)
			return nil
		},
	}
	skillFlags(cmd)
	return cmd
}

// newSkillSearchCmd browses the registry — the operator-side view of the
// same catalog an agent searches through MCP.
func newSkillSearchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search [query]",
		Short: "Search the skill registry by name (shows what you can see: your projects, organizations, and the installation shelf)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sc, err := setupSkillContext(cmd)
			if err != nil {
				return err
			}
			defer sc.pool.Close()

			var query string
			if len(args) == 1 {
				query = args[0]
			}
			page, err := sc.svc.Search(cmd.Context(), sc.principalID, sc.project.ID, query, pagingOptionsFrom(cmd))
			if err != nil {
				return err
			}
			if len(page.Items) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "(no matching skills)")
				return nil
			}
			for _, e := range page.Items {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%-30s v%-4d %-13s %s  %s\n",
					e.Name, e.LatestVersion, e.Scope, e.LatestVersionID, e.LatestChecksum[:12])
			}
			printNextCursor(cmd, page.NextCursor)
			return nil
		},
	}
	skillFlags(cmd)
	pagingFlags(cmd)
	return cmd
}

func newSkillVersionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "versions <skill-id>",
		Short: "List a skill's published versions, newest first",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sc, err := setupSkillContext(cmd)
			if err != nil {
				return err
			}
			defer sc.pool.Close()

			skillID, err := uuid.Parse(args[0])
			if err != nil {
				return fmt.Errorf("invalid skill id: %w", err)
			}
			page, err := sc.svc.ListVersions(cmd.Context(), sc.principalID, sc.project.ID, skillID, pagingOptionsFrom(cmd))
			if err != nil {
				return err
			}
			if len(page.Items) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "(no versions — unknown skill, or not visible to you)")
				return nil
			}
			for _, v := range page.Items {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "v%-4d %s  %s  %s\n", v.Version, v.ID, v.Checksum[:12], v.TrustStatus)
			}
			printNextCursor(cmd, page.NextCursor)
			return nil
		},
	}
	skillFlags(cmd)
	pagingFlags(cmd)
	return cmd
}

func newSkillPromoteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "promote <skill-id>",
		Short: "Widen where a skill lives so more people can find it (project -> organization -> installation)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sc, err := setupSkillContext(cmd)
			if err != nil {
				return err
			}
			defer sc.pool.Close()

			skillID, err := uuid.Parse(args[0])
			if err != nil {
				return fmt.Errorf("invalid skill id: %w", err)
			}
			scope, _ := cmd.Flags().GetString("scope")
			var scopeRef *uuid.UUID
			if raw, _ := cmd.Flags().GetString("scope-ref"); raw != "" {
				id, err := uuid.Parse(raw)
				if err != nil {
					return fmt.Errorf("--scope-ref: %w", err)
				}
				scopeRef = &id
			}

			sk, err := sc.svc.Promote(cmd.Context(), sc.principalID, sc.userID, sc.project.ID, skillID, skills.Scope(scope), scopeRef)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "skill %s now lives at %s scope\n", sk.ID, sk.Scope)
			return nil
		},
	}
	cmd.Flags().String("scope", "installation", `target scope: "organization" or "installation"`)
	cmd.Flags().String("scope-ref", "", "organization UUID (omit for installation scope)")
	skillFlags(cmd)
	return cmd
}
