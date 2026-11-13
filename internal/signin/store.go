package signin

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrNotFound is a rule id that does not exist.
var ErrNotFound = errors.New("signin: rule not found")

// ErrDuplicate is a rule for a domain or address that already has one.
var ErrDuplicate = errors.New("signin: a rule for this already exists")

// ErrUnknownOrganization is an organization id that does not exist.
var ErrUnknownOrganization = errors.New("signin: unknown organization")

// Store reads and writes rules. It checks nothing about the caller: the
// admin check is internal/installation's, and the sign-in path reads rules
// before anyone is signed in at all.
type Store struct{}

func NewStore() *Store { return &Store{} }

// List returns every rule, domains before addresses, each alphabetically.
// Unpaged on purpose: the sign-in path needs all of them, and a list an
// administrator types by hand stays in the tens.
func (s *Store) List(ctx context.Context, tx pgx.Tx) ([]Rule, error) {
	rows, err := tx.Query(ctx,
		`SELECT id, kind, value, created_by, created_at FROM signin_rules ORDER BY kind, value`)
	if err != nil {
		return nil, fmt.Errorf("signin: list rules: %w", err)
	}
	var rules []Rule
	index := map[uuid.UUID]int{}
	for rows.Next() {
		var r Rule
		if err := rows.Scan(&r.ID, &r.Kind, &r.Value, &r.CreatedBy, &r.CreatedAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("signin: list rules: %w", err)
		}
		r.Organizations = []Organization{}
		index[r.ID] = len(rules)
		rules = append(rules, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("signin: list rules: %w", err)
	}
	if len(rules) == 0 {
		return []Rule{}, nil
	}

	names, err := s.organizationNames(ctx, tx)
	if err != nil {
		return nil, err
	}
	grants, err := tx.Query(ctx, `SELECT rule_id, organization_id FROM signin_rule_organizations`)
	if err != nil {
		return nil, fmt.Errorf("signin: list rule organizations: %w", err)
	}
	defer grants.Close()
	for grants.Next() {
		var ruleID, orgID uuid.UUID
		if err := grants.Scan(&ruleID, &orgID); err != nil {
			return nil, fmt.Errorf("signin: list rule organizations: %w", err)
		}
		if i, ok := index[ruleID]; ok {
			rules[i].Organizations = append(rules[i].Organizations, Organization{ID: orgID, Name: names[orgID]})
		}
	}
	return rules, grants.Err()
}

// Organizations lists every organization by name, for the administrator's
// picker. Through a SECURITY DEFINER function: organizations' own RLS shows
// a caller only the ones they belong to (migrations/000036).
func (s *Store) Organizations(ctx context.Context, tx pgx.Tx) ([]Organization, error) {
	rows, err := tx.Query(ctx, `SELECT id, name FROM cc_signin_organizations()`)
	if err != nil {
		return nil, fmt.Errorf("signin: list organizations: %w", err)
	}
	defer rows.Close()
	out := []Organization{}
	for rows.Next() {
		var o Organization
		if err := rows.Scan(&o.ID, &o.Name); err != nil {
			return nil, fmt.Errorf("signin: list organizations: %w", err)
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *Store) organizationNames(ctx context.Context, tx pgx.Tx) (map[uuid.UUID]string, error) {
	orgs, err := s.Organizations(ctx, tx)
	if err != nil {
		return nil, err
	}
	names := make(map[uuid.UUID]string, len(orgs))
	for _, o := range orgs {
		names[o.ID] = o.Name
	}
	return names, nil
}

// Create inserts a rule and its organizations. value must already be
// normalized (Normalize).
func (s *Store) Create(ctx context.Context, tx pgx.Tx, kind Kind, value string, organizationIDs []uuid.UUID, createdBy uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx,
		`INSERT INTO signin_rules (kind, value, created_by) VALUES ($1, $2, $3) RETURNING id`,
		kind, value, createdBy).Scan(&id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return uuid.Nil, ErrDuplicate
		}
		return uuid.Nil, fmt.Errorf("signin: create rule: %w", err)
	}
	if err := s.insertOrganizations(ctx, tx, id, organizationIDs); err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

// SetOrganizations replaces the organizations a rule grants. Accounts that
// already joined through it keep their membership: a grant made at creation
// is membership like any other, and removing it is the organization's call.
func (s *Store) SetOrganizations(ctx context.Context, tx pgx.Tx, ruleID uuid.UUID, organizationIDs []uuid.UUID) error {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM signin_rules WHERE id = $1)`, ruleID).Scan(&exists); err != nil {
		return fmt.Errorf("signin: set rule organizations: %w", err)
	}
	if !exists {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx, `DELETE FROM signin_rule_organizations WHERE rule_id = $1`, ruleID); err != nil {
		return fmt.Errorf("signin: set rule organizations: %w", err)
	}
	return s.insertOrganizations(ctx, tx, ruleID, organizationIDs)
}

func (s *Store) insertOrganizations(ctx context.Context, tx pgx.Tx, ruleID uuid.UUID, organizationIDs []uuid.UUID) error {
	if len(organizationIDs) > maxRuleOrganizations {
		return ErrTooManyOrganizations
	}
	for _, orgID := range organizationIDs {
		_, err := tx.Exec(ctx,
			`INSERT INTO signin_rule_organizations (rule_id, organization_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			ruleID, orgID)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23503" {
				return ErrUnknownOrganization
			}
			return fmt.Errorf("signin: add rule organization: %w", err)
		}
	}
	return nil
}

// Delete removes a rule.
func (s *Store) Delete(ctx context.Context, tx pgx.Tx, ruleID uuid.UUID) error {
	tag, err := tx.Exec(ctx, `DELETE FROM signin_rules WHERE id = $1`, ruleID)
	if err != nil {
		return fmt.Errorf("signin: delete rule: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GrantMemberships makes a just-created user a member of the organizations
// the rules matching email name, and returns them. domainTrusted is
// Decision.DomainTrusted. Called inside provisioning's transaction, so an
// account never exists without the grant its rule promised.
func (s *Store) GrantMemberships(ctx context.Context, tx pgx.Tx, userID uuid.UUID, email string, domainTrusted bool) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `SELECT cc_signin_grant_memberships($1, $2, $3)`, userID, email, domainTrusted)
	if err != nil {
		return nil, fmt.Errorf("signin: grant memberships: %w", err)
	}
	defer rows.Close()
	var orgs []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("signin: grant memberships: %w", err)
		}
		orgs = append(orgs, id)
	}
	return orgs, rows.Err()
}
