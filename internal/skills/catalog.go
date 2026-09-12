package skills

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/paging"
)

// CatalogEntry is one row of the registry as a browser sees it: the skill's
// identity, where it lives, and enough about its newest version to decide
// whether to fetch it.
//
// LatestChecksum is the field that makes re-checking cheap. It is computed
// over the plaintext package at publish time, so a client that remembers
// the checksum of what it loaded can tell "this changed" from "this is the
// same skill, newer metadata" without downloading anything — and without
// trusting a version number or a timestamp to mean what it looks like.
type CatalogEntry struct {
	SkillID  uuid.UUID
	Name     string
	Author   string
	Origin   string
	Scope    Scope
	ScopeRef *uuid.UUID

	LatestVersionID uuid.UUID
	LatestVersion   int
	LatestChecksum  string
	TrustStatus     string
}

// Search lists the registry, newest-name-first, optionally narrowed to
// names containing query (case-insensitive).
//
// What comes back is exactly what the caller can see: skills living in
// their projects, in their organizations, and on the installation-wide
// shelf (docs/adr/0018). No project is named or needed — scope decides
// visibility, and RLS applies it (see GetVersion for why the decision sits
// in the database here).
//
// Skills with no published version are omitted. A Skill row exists from the
// moment Publish starts, before its first SkillVersion is committed
// (Service.Publish phase 1), and offering an agent a catalog entry it
// cannot download would be worse than not listing it yet.
func (s *Service) Search(
	ctx context.Context, principalID uuid.UUID, query string, opts paging.Options,
) (paging.Page[CatalogEntry], error) {
	cursorName, cursorID, hasCursor, err := decodeCatalogCursor(opts)
	if err != nil {
		return paging.Page[CatalogEntry]{}, err
	}
	limit := opts.ClampedLimit()

	// Matched case-insensitively on a substring, with the LIKE
	// metacharacters escaped: a search for "c++" or "50%" must look for
	// those characters, not for a pattern.
	var pattern *string
	if query != "" {
		p := "%" + escapeLike(strings.ToLower(query)) + "%"
		pattern = &p
	}

	out := make([]CatalogEntry, 0, limit)
	err = db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		// DISTINCT ON picks each skill's highest version in one pass. RLS on
		// `skills` (migrations/000021) does the visibility filtering, and
		// skill_versions inherits it through its own policy, so neither is
		// restated here — restating it would invite the two to drift.
		rows, err := tx.Query(ctx,
			`SELECT DISTINCT ON (s.id)
			        s.id, s.name, COALESCE(s.author, ''), COALESCE(s.origin, ''), s.scope, s.scope_ref,
			        v.id, v.version, v.checksum, v.trust_status
			 FROM skills s JOIN skill_versions v ON v.skill_id = s.id
			 WHERE ($1::text IS NULL OR lower(s.name) LIKE $1 ESCAPE '\')
			   AND (NOT $2::bool OR (lower(s.name), s.id) > (lower($3::text), $4::uuid))
			 ORDER BY s.id, v.version DESC`,
			pattern, hasCursor, cursorName, cursorID)
		if err != nil {
			return fmt.Errorf("skills: search: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var e CatalogEntry
			if err := rows.Scan(&e.SkillID, &e.Name, &e.Author, &e.Origin, &e.Scope, &e.ScopeRef,
				&e.LatestVersionID, &e.LatestVersion, &e.LatestChecksum, &e.TrustStatus); err != nil {
				return fmt.Errorf("skills: scan catalog entry: %w", err)
			}
			out = append(out, e)
		}
		return rows.Err()
	})
	if err != nil {
		return paging.Page[CatalogEntry]{}, err
	}

	// DISTINCT ON forces ORDER BY to start with s.id, so the name ordering
	// a reader expects is applied here rather than in SQL. The catalog is
	// an instance's own registry, not a package index with millions of
	// entries, so sorting one page is the right trade against a second
	// query or a window function.
	sortByNameThenID(out)
	items, hasMore := paging.More(out, limit)
	page := paging.Page[CatalogEntry]{Items: items}
	if hasMore {
		last := items[len(items)-1]
		page.NextCursor = encodeCatalogCursor(last.Name, last.SkillID)
	}
	return page, nil
}

// ListVersions returns a skill's versions, newest first — what a client
// consults to pin an older version deliberately, or to see what it missed.
func (s *Service) ListVersions(
	ctx context.Context, principalID, skillID uuid.UUID, opts paging.Options,
) (paging.Page[SkillVersion], error) {
	afterVersion, hasCursor, err := opts.DecodeSeq()
	if err != nil {
		return paging.Page[SkillVersion]{}, err
	}
	limit := opts.ClampedLimit()

	out := make([]SkillVersion, 0, limit)
	err = db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		// Newest first, so the cursor walks DOWN from the last version
		// returned rather than up from zero.
		rows, err := tx.Query(ctx,
			`SELECT v.id, v.skill_id, v.version, v.checksum, v.storage_key, v.files, v.declared_capabilities,
			        v.trust_status, v.signature, v.created_at, v.published_in_project, v.published_by
			 FROM skill_versions v
			 WHERE v.skill_id = $1 AND (NOT $2::bool OR v.version < $3::int)
			 ORDER BY v.version DESC LIMIT $4`,
			skillID, hasCursor, afterVersion, limit+1)
		if err != nil {
			return fmt.Errorf("skills: list versions: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var v SkillVersion
			if err := rows.Scan(&v.ID, &v.SkillID, &v.Version, &v.Checksum, &v.StorageKey, &v.Files,
				&v.DeclaredCapabilities, &v.TrustStatus, &v.Signature, &v.CreatedAt,
				&v.PublishedInProject, &v.PublishedBy); err != nil {
				return fmt.Errorf("skills: scan version: %w", err)
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	if err != nil {
		return paging.Page[SkillVersion]{}, err
	}
	if len(out) == 0 {
		// Either the skill does not exist or it is not visible to this
		// caller; those are the same answer on purpose (docs/adr/0018 —
		// the catalog shows what you may see, and says nothing about the
		// rest).
		return paging.Page[SkillVersion]{Items: out}, nil
	}

	items, hasMore := paging.More(out, limit)
	page := paging.Page[SkillVersion]{Items: items}
	if hasMore {
		page.NextCursor = paging.EncodeSeq(int64(items[len(items)-1].Version))
	}
	return page, nil
}

// Promote widens where a skill lives: project -> organization ->
// installation. Putting a skill on the installation shelf is what "publish
// to the marketplace" means here, and it is a separate, authorized act
// rather than a side effect of publishing (docs/adr/0018).
//
// Authorization is checkPublishTarget — the same check Publish uses, asked
// about the TARGET scope: promoting to an organization needs
// CapOrgManageMembers there, and to the installation needs
// installation-admin rights (docs/adr/0017). Where the skill currently
// lives does not grant the right to move it somewhere wider; only the
// destination does.
func (s *Service) Promote(
	ctx context.Context, principalID, userID uuid.UUID,
	skillID uuid.UUID, targetScope Scope, targetScopeRef *uuid.UUID,
) (Skill, error) {
	var skill Skill
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		if err := s.checkPublishTarget(ctx, tx, principalID, userID, targetScope, targetScopeRef); err != nil {
			return err
		}

		var current Scope
		err := tx.QueryRow(ctx, `SELECT scope FROM skills WHERE id = $1`, skillID).Scan(&current)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("skills: read current scope: %w", err)
		}
		if scopeWidth(targetScope) <= scopeWidth(current) {
			return fmt.Errorf("%w: %s is not wider than %s", ErrNotPromotable, targetScope, current)
		}

		tag, err := tx.Exec(ctx, `UPDATE skills SET scope = $1, scope_ref = $2 WHERE id = $3`,
			targetScope, targetScopeRef, skillID)
		if err != nil {
			return fmt.Errorf("skills: promote: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		skill = Skill{ID: skillID, Scope: targetScope, ScopeRef: targetScopeRef}
		return nil
	})
	if err != nil {
		return Skill{}, err
	}
	return skill, nil
}

// scopeWidth orders the scopes by how many principals can see them, which
// is the only ordering Promote cares about.
func scopeWidth(s Scope) int {
	switch s {
	case ScopeInstallation:
		return 3
	case ScopeOrganization:
		return 2
	case ScopeProject:
		return 1
	default:
		return 0
	}
}

// catalogCursorSep separates the two halves of a catalog cursor. A unit
// separator, because a skill name may contain any printable character a
// human would type — including the ":" and "|" the obvious choices would
// have used.
const catalogCursorSep = "\x1f"

func encodeCatalogCursor(name string, id uuid.UUID) string {
	return paging.EncodeString(strings.ToLower(name) + catalogCursorSep + id.String())
}

func decodeCatalogCursor(opts paging.Options) (name string, id uuid.UUID, ok bool, err error) {
	raw, ok, err := opts.DecodeString()
	if err != nil || !ok {
		return "", uuid.Nil, false, err
	}
	name, rawID, found := strings.Cut(raw, catalogCursorSep)
	if !found {
		return "", uuid.Nil, false, fmt.Errorf("%w: malformed catalog cursor", paging.ErrInvalidCursor)
	}
	id, parseErr := uuid.Parse(rawID)
	if parseErr != nil {
		return "", uuid.Nil, false, fmt.Errorf("%w: malformed catalog cursor", paging.ErrInvalidCursor)
	}
	return name, id, true, nil
}

// escapeLike makes a literal string safe inside a LIKE pattern. Backslash
// first, or it would double-escape the escapes added after it.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// sortByNameThenID gives the page the ordering the cursor assumes.
func sortByNameThenID(entries []CatalogEntry) {
	slices.SortFunc(entries, func(a, b CatalogEntry) int {
		if c := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
			return c
		}
		return strings.Compare(a.SkillID.String(), b.SkillID.String())
	})
}
