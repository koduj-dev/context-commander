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

	"github.com/koduj-dev/context-commander/internal/db"
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
// Reading needs no project, and takes no capability check of its own.
//
// Visibility IS the authorization here, and it is Row-Level Security on
// `skills` / `skill_versions` (migrations/000021): a package is visible at
// its own scope — its project, its organization, or the installation-wide
// shelf (docs/adr/0018). A version outside every scope the caller can see is
// not refused, it is simply not there, and this returns ErrNotFound.
//
// An earlier version of this method demanded CapSkillRead on an "anchor"
// project the caller named. That was theatre: every project role holds
// CapSkillRead, so it only ever proved the caller belonged to some project —
// which is not a fact the registry cares about, and which excluded exactly
// the people a self-hosted instance most wants reading its shelf (an
// installation admin who is in no project at all).
//
// That RLS carries the decision is a deliberate exception to "authorize in
// the domain service" (CONTRIBUTING rule 7). Restating the scope predicate
// in Go would mean resolving organization membership, which is keyed by user
// (docs/adr/0006) and not by the principal a read path carries. The policy
// is identical to the one skill_bindings has always used, and this package's
// tests exercise it with RLS both on and off.
//
// The consequence worth stating wherever a caller can act on it: a skill on
// the installation shelf is readable by everyone in the installation, and
// skill content is stored unencrypted either way. Confidential material
// belongs in internal/secrets, never in a package.
func (s *Service) GetVersion(ctx context.Context, principalID, skillVersionID uuid.UUID) (VersionWithContent, error) {
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
		return nil
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
