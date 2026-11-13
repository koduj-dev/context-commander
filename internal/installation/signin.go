package installation

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/signin"
)

// SignInRules lists the sign-in allowlist (docs/adr/0024). Gated on
// CapSignInManage rather than CapAdminRead: the list is short, and the
// people allowed to read it are the people who decide it.
func (s *Service) SignInRules(ctx context.Context, principalID, callerUserID uuid.UUID) ([]signin.Rule, error) {
	if err := s.checkCapability(ctx, principalID, callerUserID, CapSignInManage); err != nil {
		return nil, err
	}
	var rules []signin.Rule
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		rules, err = signin.NewStore().List(ctx, tx)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("installation: sign-in rules: %w", err)
	}
	return rules, nil
}

// SignInOrganizations lists every organization by name, for choosing what a
// rule grants.
func (s *Service) SignInOrganizations(ctx context.Context, principalID, callerUserID uuid.UUID) ([]signin.Organization, error) {
	if err := s.checkCapability(ctx, principalID, callerUserID, CapSignInManage); err != nil {
		return nil, err
	}
	var orgs []signin.Organization
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		orgs, err = signin.NewStore().Organizations(ctx, tx)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("installation: sign-in organizations: %w", err)
	}
	return orgs, nil
}

// CreateSignInRule adds a domain or an address to the allowlist, with the
// organizations a new account matching it joins. raw is what the
// administrator typed; signin.Normalize decides which kind it is.
func (s *Service) CreateSignInRule(ctx context.Context, principalID, callerUserID uuid.UUID, raw string, organizationIDs []uuid.UUID) (signin.Rule, error) {
	if err := s.checkCapability(ctx, principalID, callerUserID, CapSignInManage); err != nil {
		return signin.Rule{}, err
	}
	kind, value, err := signin.Normalize(raw)
	if err != nil {
		return signin.Rule{}, err
	}
	var rule signin.Rule
	err = db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		store := signin.NewStore()
		id, err := store.Create(ctx, tx, kind, value, organizationIDs, callerUserID)
		if err != nil {
			return err
		}
		rule, err = findRule(ctx, tx, store, id)
		return err
	})
	if err != nil {
		return signin.Rule{}, fmt.Errorf("installation: create sign-in rule: %w", err)
	}
	return rule, nil
}

// SetSignInRuleOrganizations replaces what a rule grants. It changes nothing
// for accounts that already exist.
func (s *Service) SetSignInRuleOrganizations(ctx context.Context, principalID, callerUserID, ruleID uuid.UUID, organizationIDs []uuid.UUID) (signin.Rule, error) {
	if err := s.checkCapability(ctx, principalID, callerUserID, CapSignInManage); err != nil {
		return signin.Rule{}, err
	}
	var rule signin.Rule
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		store := signin.NewStore()
		if err := store.SetOrganizations(ctx, tx, ruleID, organizationIDs); err != nil {
			return err
		}
		var err error
		rule, err = findRule(ctx, tx, store, ruleID)
		return err
	})
	if err != nil {
		return signin.Rule{}, fmt.Errorf("installation: set sign-in rule organizations: %w", err)
	}
	return rule, nil
}

// DeleteSignInRule removes a rule. People it admitted keep their accounts
// and memberships, but cannot sign in through OIDC again unless another rule
// or --oidc-allowed-domains still admits them.
func (s *Service) DeleteSignInRule(ctx context.Context, principalID, callerUserID, ruleID uuid.UUID) error {
	if err := s.checkCapability(ctx, principalID, callerUserID, CapSignInManage); err != nil {
		return err
	}
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		return signin.NewStore().Delete(ctx, tx, ruleID)
	})
	if err != nil {
		return fmt.Errorf("installation: delete sign-in rule: %w", err)
	}
	return nil
}

func findRule(ctx context.Context, tx pgx.Tx, store *signin.Store, id uuid.UUID) (signin.Rule, error) {
	rules, err := store.List(ctx, tx)
	if err != nil {
		return signin.Rule{}, err
	}
	for _, r := range rules {
		if r.ID == id {
			return r, nil
		}
	}
	return signin.Rule{}, signin.ErrNotFound
}
