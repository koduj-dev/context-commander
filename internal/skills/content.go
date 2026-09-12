package skills

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/projects"
	"github.com/koduj-dev/context-commander/internal/storage"
)

// MaxContentSize bounds a skill package in both directions — what Publish
// accepts and what GetVersion will buffer. Skill content is read whole into
// memory (there is no streaming/chunked format in v1, same as
// internal/artifacts), so without a cap a single publish decides how much
// memory every later read allocates.
//
// 10 MB is far above any real skill package — these are prompt/instruction
// bundles, not binaries — and far below the 50 MB artifact limit, which
// exists for a domain that genuinely carries payloads.
const MaxContentSize = 10 * 1024 * 1024

var (
	// ErrTooLarge is returned by Publish for content over MaxContentSize.
	ErrTooLarge = errors.New("skills: content exceeds the 10 MB limit")

	// ErrChecksumMismatch means the bytes in the object store no longer
	// hash to the checksum recorded when the version was published — a
	// SkillVersion is immutable, so this is corruption or tampering in the
	// storage backend, never a legitimate state. It is returned instead of
	// the content: handing an agent skill instructions that don't match
	// what was reviewed and pinned is the exact supply-chain failure
	// docs/adr/0012 is about.
	ErrChecksumMismatch = errors.New("skills: content checksum mismatch")
)

// VersionWithContent is what skill.get returns: the version's metadata, the
// Skill identity it belongs to (an agent needs the name to know what it is
// loading, not just a UUID), and the packaged bytes themselves.
type VersionWithContent struct {
	Skill   Skill
	Version SkillVersion
	Content []byte
}

// GetVersion returns a SkillVersion's metadata and its packaged content.
//
// This closes the hole NEXT.md called "the single most glaring one in the
// current surface": Install/ListEffective hand back SkillVersion UUIDs, and
// until now nothing in any adapter could turn one into the bytes it names —
// an agent could install a skill it was then unable to load.
//
// Authorization has two steps, because the anchor project cannot carry
// both. projectID is an anchor the caller must hold CapSkillRead on — the
// same shape Publish uses (docs/adr/0014) — but the caller picks it, so on
// its own it proves nothing about the skill being read. `skills` and
// `skill_versions` carry no RLS (migrations/000007), so without a second
// check any principal holding CapSkillRead on any project could read the
// content of ANY version whose UUID it knew, including one published inside
// a project it was not a member of. That was true when this method first
// shipped; canReadVersion is what closes it.
//
// The rule: you may read a skill's content if you can see a binding for it,
// or if you are a member of the project it was published from.
//
// That deliberately does not decide what the skill registry IS — whether
// packages are instance-wide, organization-scoped, or explicitly promoted
// is a product question (docs/adr/0012 scopes the *binding*, not the
// package). It keeps every flow that works today: an installation-scope
// binding is visible to everyone, so an installation-wide skill stays
// installation-wide; a project's effective skills resolve through bindings
// it can see; and publishing then reading your own package back works
// because you are a member of the project you published from.
func (s *Service) GetVersion(ctx context.Context, principalID, projectID, skillVersionID uuid.UUID) (VersionWithContent, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapSkillRead); err != nil {
		return VersionWithContent{}, err
	}

	var out VersionWithContent
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		// author/origin are nullable in migrations/000007 but plain strings
		// on Skill — COALESCE rather than scanning through *string, since
		// "no author recorded" and "author recorded as empty" are not a
		// distinction this domain makes anywhere else either.
		err := tx.QueryRow(ctx,
			`SELECT v.id, v.skill_id, v.version, v.checksum, v.storage_key, v.files, v.declared_capabilities,
			        v.trust_status, v.signature, v.created_at, v.published_in_project, v.published_by,
			        s.id, s.name, COALESCE(s.author, ''), COALESCE(s.origin, ''), s.created_at
			 FROM skill_versions v JOIN skills s ON s.id = v.skill_id
			 WHERE v.id = $1`,
			skillVersionID,
		).Scan(
			&out.Version.ID, &out.Version.SkillID, &out.Version.Version, &out.Version.Checksum, &out.Version.StorageKey,
			&out.Version.Files, &out.Version.DeclaredCapabilities, &out.Version.TrustStatus, &out.Version.Signature, &out.Version.CreatedAt,
			&out.Version.PublishedInProject, &out.Version.PublishedBy,
			&out.Skill.ID, &out.Skill.Name, &out.Skill.Author, &out.Skill.Origin, &out.Skill.CreatedAt,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("skills: get version: %w", err)
		}
		return s.canReadVersion(ctx, tx, principalID, out.Version)
	})
	if err != nil {
		return VersionWithContent{}, err
	}

	// Outside the transaction: the object store doesn't participate in
	// Postgres transactions, and holding a connection open across a
	// network fetch is what exhausts a small pool (same reasoning as
	// internal/artifacts.Service.Read).
	content, err := s.readContent(ctx, out.Version.StorageKey)
	if err != nil {
		return VersionWithContent{}, err
	}

	// Immutable row + immutable object: the recorded checksum must still
	// describe the bytes. Verified on every read rather than trusted,
	// because the object store is the one half of this pair that a
	// Postgres constraint cannot protect.
	sum := sha256.Sum256(content)
	if hex.EncodeToString(sum[:]) != out.Version.Checksum {
		return VersionWithContent{}, fmt.Errorf("%w: skill_version %s", ErrChecksumMismatch, skillVersionID)
	}

	out.Content = content
	return out, nil
}

func (s *Service) readContent(ctx context.Context, storageKey string) ([]byte, error) {
	rc, err := s.store.Get(ctx, storageKey)
	if errors.Is(err, storage.ErrNotFound) {
		// The row exists but its content doesn't. Publish uploads before
		// inserting, so this is not a mid-publish race — it is content
		// that went missing underneath an immutable row.
		return nil, fmt.Errorf("%w: content for %s is missing from the object store", ErrNotFound, storageKey)
	}
	if err != nil {
		return nil, fmt.Errorf("skills: fetch content: %w", err)
	}
	defer func() { _ = rc.Close() }()

	// MaxContentSize+1 so an object that grew past the cap after being
	// published (or one written by something other than Publish) is
	// reported rather than silently truncated to a package that would look
	// valid but hash differently.
	content, err := io.ReadAll(io.LimitReader(rc, MaxContentSize+1))
	if err != nil {
		return nil, fmt.Errorf("skills: read content: %w", err)
	}
	if len(content) > MaxContentSize {
		return nil, ErrTooLarge
	}
	return content, nil
}

// canReadVersion decides whether principalID may read this version's bytes.
//
// Returns ErrNotFound rather than ErrForbidden for an unreadable version,
// following the same reasoning projects.Resolve uses for a non-member:
// telling a stranger that a skill exists but is not theirs hands out more
// than "you can't have this".
func (s *Service) canReadVersion(ctx context.Context, tx pgx.Tx, principalID uuid.UUID, v SkillVersion) error {
	// A version published before migration 000020 has no recorded
	// provenance, and there is nothing to infer it from. Those are read as
	// installation-wide — the terms they were published under — rather than
	// made permanently unreadable.
	if v.PublishedInProject == nil {
		return nil
	}

	// Visible through a binding. The subquery is not filtered by scope here
	// because RLS already is: migrations/000007's skill_bindings policy
	// shows installation-scope bindings to everyone, organization-scope to
	// that organization's members, and project-scope to that project's
	// members. Asking "does a binding for this skill exist" under the
	// caller's own principal therefore asks exactly "can this caller see
	// one".
	var boundVisibly bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM skill_bindings WHERE skill_id = $1)`, v.SkillID,
	).Scan(&boundVisibly); err != nil {
		return fmt.Errorf("skills: check visible bindings: %w", err)
	}
	if boundVisibly {
		return nil
	}

	// Or a member of the project it was published from — which is what lets
	// a publisher read back a package nobody has installed yet.
	if _, err := s.projects.GetMembership(ctx, tx, *v.PublishedInProject, principalID); err != nil {
		if errors.Is(err, projects.ErrNotFound) {
			return ErrNotFound
		}
		return fmt.Errorf("skills: check publishing project membership: %w", err)
	}
	return nil
}
