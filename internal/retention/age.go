package retention

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
)

// AgeRules says how long records are kept, in days; 0 keeps them forever,
// which is the default for every kind (docs/adr/0039). What a team must keep
// is the operator's decision, not this package's.
type AgeRules struct {
	// ArchivedMemoryDays: archived and superseded memories, by when they
	// last changed. Active memories are never removed by age.
	ArchivedMemoryDays int
	// HistoryDays: conversations whose newest object (or, empty, the
	// conversation itself) is older; with their sessions and objects.
	HistoryDays int
	// AuditDays: audit trail rows, by their time.
	AuditDays int
}

// AgeReport counts what an age run removed, or would remove.
type AgeReport struct {
	Memories, Conversations, AuditRows int64
}

// RunAge removes records older than the rules allow, except in projects
// under legal hold. With dryRun it only counts. Each kind is one
// transaction.
func (p *Purger) RunAge(ctx context.Context, rules AgeRules, dryRun bool) (AgeReport, error) {
	var r AgeReport
	now := p.now()
	cutoff := func(days int) time.Time { return now.Add(-time.Duration(days) * 24 * time.Hour) }
	run := func(count, del string, args []any, n *int64) error {
		return db.WithoutPrincipal(ctx, p.pool, func(tx pgx.Tx) error {
			if dryRun {
				return tx.QueryRow(ctx, count, args...).Scan(n)
			}
			tag, err := tx.Exec(ctx, del, args...)
			*n = tag.RowsAffected()
			return err
		})
	}

	if rules.ArchivedMemoryDays > 0 {
		where := `FROM memories m JOIN projects pr ON pr.id = m.project_id
		          WHERE m.status IN ('archived', 'superseded') AND m.updated_at < $1 AND NOT pr.legal_hold`
		if err := run(`SELECT count(*) `+where,
			`DELETE FROM memories WHERE id IN (SELECT m.id `+where+`)`,
			[]any{cutoff(rules.ArchivedMemoryDays)}, &r.Memories); err != nil {
			return r, fmt.Errorf("retention: memories by age: %w", err)
		}
	}

	if rules.HistoryDays > 0 {
		err := db.WithoutPrincipal(ctx, p.pool, func(tx pgx.Tx) error {
			// A temporary table, so the three deletes and the count agree
			// on the same set of conversations.
			if _, err := tx.Exec(ctx,
				`CREATE TEMP TABLE old_conversations ON COMMIT DROP AS
				 SELECT c.id FROM conversations c JOIN projects pr ON pr.id = c.project_id
				 WHERE NOT pr.legal_hold
				   AND COALESCE((SELECT max(o.created_at) FROM history_objects o WHERE o.conversation_id = c.id), c.created_at) < $1`,
				cutoff(rules.HistoryDays)); err != nil {
				return err
			}
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM old_conversations`).Scan(&r.Conversations); err != nil {
				return err
			}
			if dryRun || r.Conversations == 0 {
				return nil
			}
			for _, stmt := range []string{
				`DELETE FROM history_objects WHERE conversation_id IN (SELECT id FROM old_conversations)`,
				`DELETE FROM external_sessions WHERE conversation_id IN (SELECT id FROM old_conversations)`,
				`DELETE FROM conversations WHERE id IN (SELECT id FROM old_conversations)`,
			} {
				if _, err := tx.Exec(ctx, stmt); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return r, fmt.Errorf("retention: history by age: %w", err)
		}
	}

	if rules.AuditDays > 0 {
		where := `FROM audit_log a LEFT JOIN projects pr ON pr.id = a.project_id
		          WHERE a.ts < $1 AND NOT COALESCE(pr.legal_hold, false)`
		err := db.WithoutPrincipal(ctx, p.pool, func(tx pgx.Tx) error {
			if dryRun {
				return tx.QueryRow(ctx, `SELECT count(*) `+where, cutoff(rules.AuditDays)).Scan(&r.AuditRows)
			}
			if _, err := tx.Exec(ctx, `DELETE FROM encrypted_audit_payload WHERE audit_log_id IN (SELECT a.id `+where+`)`, cutoff(rules.AuditDays)); err != nil {
				return err
			}
			tag, err := tx.Exec(ctx, `DELETE FROM audit_log WHERE id IN (SELECT a.id `+where+`)`, cutoff(rules.AuditDays))
			r.AuditRows = tag.RowsAffected()
			return err
		})
		if err != nil {
			return r, fmt.Errorf("retention: audit by age: %w", err)
		}
	}
	return r, nil
}
