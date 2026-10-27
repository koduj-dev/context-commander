package installation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/paging"
)

// ErrInvalidLimit is a storage limit that is neither a non-negative number
// of bytes nor "unlimited".
var ErrInvalidLimit = errors.New("installation: a storage limit is a non-negative number of bytes, or unlimited")

// NamespaceStorage is one namespace's storage: who owns it, how much it holds
// and what it may hold (docs/adr/0021). Counts only — nothing here names a
// project or a path inside the namespace.
type NamespaceStorage struct {
	NamespaceID uuid.UUID
	Slug        string
	// Kind is "personal" or "organization".
	Kind        string
	DisplayName string
	CreatedAt   time.Time
	UsedBytes   int64

	// LimitBytes is an admin's exception, nil when the default applies.
	LimitBytes *int64
	// Unlimited is an admin's exception that removes the limit entirely.
	Unlimited bool
	// EffectiveLimit is what is enforced: the exception, else the server
	// default; nil when nothing limits the namespace.
	EffectiveLimit *int64
}

// StorageLimit is what an admin sets on a namespace. Both zero values put
// the namespace back on the server default.
type StorageLimit struct {
	Bytes     *int64
	Unlimited bool
}

// ListStorage pages through every namespace's storage, oldest namespace
// first. Gated on CapAdminRead: it is installation-wide state in counts, the
// same kind of read as Stats.
func (s *Service) ListStorage(ctx context.Context, principalID, callerUserID uuid.UUID, opts paging.Options) (paging.Page[NamespaceStorage], error) {
	if err := s.checkCapability(ctx, principalID, callerUserID, CapAdminRead); err != nil {
		return paging.Page[NamespaceStorage]{}, err
	}
	cursorTime, cursorID, hasCursor, err := opts.DecodeTimeUUID()
	if err != nil {
		return paging.Page[NamespaceStorage]{}, err
	}
	limit := opts.ClampedLimit()
	var afterID *uuid.UUID
	if hasCursor {
		afterID = &cursorID
	}

	out := make([]NamespaceStorage, 0, limit)
	err = db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT * FROM cc_namespace_storage_page($1, $2, $3)`, cursorTime, afterID, limit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var n NamespaceStorage
			if err := rows.Scan(&n.NamespaceID, &n.Slug, &n.Kind, &n.DisplayName, &n.CreatedAt,
				&n.UsedBytes, &n.LimitBytes, &n.Unlimited, &n.EffectiveLimit); err != nil {
				return err
			}
			out = append(out, n)
		}
		return rows.Err()
	})
	if err != nil {
		return paging.Page[NamespaceStorage]{}, fmt.Errorf("installation: list storage: %w", err)
	}

	page, hasMore := paging.More(out, limit)
	next := ""
	if hasMore {
		last := page[len(page)-1]
		next = paging.EncodeTimeUUID(last.CreatedAt, last.NamespaceID)
	}
	return paging.Page[NamespaceStorage]{Items: page, NextCursor: next}, nil
}

// SetStorageLimit sets an admin's exception on one namespace. Lowering a
// limit below what the namespace already holds is allowed: it refuses the
// namespace's next write, never its deletes.
func (s *Service) SetStorageLimit(ctx context.Context, principalID, callerUserID, namespaceID uuid.UUID, l StorageLimit) error {
	if err := s.checkCapability(ctx, principalID, callerUserID, CapStorageManage); err != nil {
		return err
	}
	if (l.Bytes != nil && *l.Bytes < 0) || (l.Bytes != nil && l.Unlimited) {
		return ErrInvalidLimit
	}
	var found bool
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT cc_set_namespace_quota($1, $2, $3, $4)`,
			namespaceID, l.Bytes, l.Unlimited, callerUserID).Scan(&found)
	})
	if err != nil {
		return fmt.Errorf("installation: set storage limit: %w", err)
	}
	if !found {
		return ErrNotFound
	}
	return nil
}

// MyStorage is the caller's own namespace and their organizations', with
// what each holds and may hold. No capability check, for the reason IsAdmin
// has none: it is the caller's own state. Exceptions are not shown as such —
// a member sees the limit that applies, not how an admin arrived at it.
func (s *Service) MyStorage(ctx context.Context, principalID uuid.UUID) ([]NamespaceStorage, error) {
	var out []NamespaceStorage
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT * FROM cc_my_storage()`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var n NamespaceStorage
			if err := rows.Scan(&n.NamespaceID, &n.Slug, &n.Kind, &n.DisplayName, &n.UsedBytes, &n.EffectiveLimit); err != nil {
				return err
			}
			out = append(out, n)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("installation: my storage: %w", err)
	}
	return out, nil
}
