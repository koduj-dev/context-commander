package installation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/identity"
	"github.com/koduj-dev/context-commander/internal/paging"
)

// ErrForbidden is returned when a caller lacks the requested Capability —
// see internal/organizations.ErrForbidden for the same reasoning (distinct
// from ErrNotFound so an adapter can choose whether to leak existence).
var ErrForbidden = errors.New("installation: forbidden")

// Service is installation-admin's authorization chokepoint (docs/adr/0014):
// every method checks admin status itself rather than trusting a caller to
// have checked already. Every method takes both principalID (to set RLS
// session context) and callerUserID (installation_admins is keyed by User,
// never Principal — a ServicePrincipal can never be an installation admin,
// docs/adr/0017) — the same shape internal/organizations.Service uses and
// for the same reason: the two are not derivable from each other without a
// lookup the caller already has (adapters/http/organizations.go's
// resolveCallerUserID, reused as-is by the installation adapters).
type Service struct {
	pool     *db.Pool
	store    *Store
	identity *identity.Store
}

func NewService(pool *db.Pool, store *Store, identityStore *identity.Store) *Service {
	return &Service{pool: pool, store: store, identity: identityStore}
}

func (s *Service) checkCapability(ctx context.Context, principalID, callerUserID uuid.UUID, cap Capability) error {
	var allowed bool
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		isAdmin, err := s.store.IsAdmin(ctx, tx, callerUserID)
		if err != nil {
			return err
		}
		allowed = isAdmin && RoleCan(RoleAdmin, cap)
		return nil
	})
	if err != nil {
		return fmt.Errorf("installation: authorization check: %w", err)
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

// IsAdmin reports whether callerUserID is themselves an installation admin
// — no capability check, mirroring internal/organizations.Service.ListMine's
// reasoning: this is the caller's own status, not access to someone else's
// data, so there is nothing to gate. GET /v1/me's only caller: it needs
// this to decide whether to show the Administration nav entry at all.
func (s *Service) IsAdmin(ctx context.Context, principalID, callerUserID uuid.UUID) (bool, error) {
	var isAdmin bool
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		isAdmin, err = s.store.IsAdmin(ctx, tx, callerUserID)
		return err
	})
	if err != nil {
		return false, fmt.Errorf("installation: is admin: %w", err)
	}
	return isAdmin, nil
}

// ListAdmins returns every installation admin, after checking callerUserID
// holds CapAdminRead (granted to any admin — this is a read of the admin
// list itself, not an escalation path: no method in this package lets one
// admin grant another over HTTP/MCP, see docs/adr/0017).
func (s *Service) ListAdmins(ctx context.Context, principalID, callerUserID uuid.UUID) ([]Admin, error) {
	if err := s.checkCapability(ctx, principalID, callerUserID, CapAdminRead); err != nil {
		return nil, err
	}
	var admins []Admin
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		admins, err = s.store.List(ctx, tx)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("installation: list admins: %w", err)
	}
	return admins, nil
}

// CreateUser creates a User and its Principal after checking callerUserID
// holds CapUserCreate — the HTTP/MCP-reachable counterpart to `ccd user
// create`, which internal/identity's own doc comment already anticipated
// (docs/adr/0009's "a future adapters/http admin endpoint").
// passwordHash, when non-nil, is set in the SAME transaction as the user and
// its principal. Creating the account and then setting the password in a
// second call could leave a user who cannot sign in and a screen that said
// they could — and the person it was created for would be the one to find out.
//
// A password requires CapCredentialManage on top of CapUserCreate. Setting
// someone's password is the power to sign in as them, which is exactly what
// issuing them a token already is; both capabilities sit on the single ADMIN
// role today, so this changes nothing in practice. It is written down for the
// day a narrower role exists, because "may create accounts" and "may become
// any account" are not the same permission.
func (s *Service) CreateUser(
	ctx context.Context, principalID, callerUserID uuid.UUID,
	email, displayName string, passwordHash *string,
) (identity.User, error) {
	if err := s.checkCapability(ctx, principalID, callerUserID, CapUserCreate); err != nil {
		return identity.User{}, err
	}
	if passwordHash != nil {
		if err := s.checkCapability(ctx, principalID, callerUserID, CapCredentialManage); err != nil {
			return identity.User{}, err
		}
	}
	var u identity.User
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		u, err = s.identity.CreateUser(ctx, tx, email, displayName)
		if err != nil {
			return err
		}
		if _, err := s.identity.CreatePrincipalForUser(ctx, tx, u.ID); err != nil {
			return err
		}
		if passwordHash != nil {
			return s.identity.SetPassword(ctx, tx, u.ID, *passwordHash)
		}
		return nil
	})
	if err != nil {
		return identity.User{}, fmt.Errorf("installation: create user: %w", err)
	}
	return u, nil
}

// SetUserPassword sets or replaces a user's local password.
//
// It exists because an administrator who creates an account had no way to let
// anyone into it: the first-run setup code is gone once the installation has a
// user, `ccd user set-password` needs a shell on the server, and handing over
// a personal access token makes the new person inherit a credential the
// administrator also holds.
//
// CapCredentialManage, for the reason given on CreateUser: this is the power
// to sign in as somebody. It is not a new power — the same capability already
// issues a token for any principal — and saying so is the point, because a
// reader should not have to work out whether this widened anything.
//
// There is deliberately no "must change at next sign-in" flag. That needs a
// column, a check on every authenticated request and a screen of its own, and
// half of it is worse than none: a flag nothing enforces reads as a promise
// the product does not keep. Recorded in NEXT.md instead.
func (s *Service) SetUserPassword(
	ctx context.Context, principalID, callerUserID, targetUserID uuid.UUID, passwordHash string,
) error {
	if err := s.checkCapability(ctx, principalID, callerUserID, CapCredentialManage); err != nil {
		return err
	}
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		// Checked rather than assumed: SetPassword is an upsert keyed on
		// user_id with a foreign key, so a stray id would fail on the
		// constraint with a message about a relation nobody asked about.
		if _, err := s.identity.GetUser(ctx, tx, targetUserID); err != nil {
			return err
		}
		return s.identity.SetPassword(ctx, tx, targetUserID, passwordHash)
	})
	if err != nil {
		return fmt.Errorf("installation: set user password: %w", err)
	}
	return nil
}

// CreateServicePrincipal registers a ServicePrincipal (an agent) and its
// Principal after checking callerUserID holds CapServicePrincipalCreate —
// internal/identity.Store.CreateServicePrincipal/CreatePrincipalForService
// existed with zero adapter callers before this method (NEXT.md's audit
// note: "agents cannot be registered through any interface today, not even
// CLI").
func (s *Service) CreateServicePrincipal(ctx context.Context, principalID, callerUserID uuid.UUID, name, description string) (identity.ServicePrincipal, error) {
	if err := s.checkCapability(ctx, principalID, callerUserID, CapServicePrincipalCreate); err != nil {
		return identity.ServicePrincipal{}, err
	}
	var sp identity.ServicePrincipal
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		// callerUserID is already resolved and capability-checked above, and
		// was being thrown away: the agent recorded no trace of who
		// registered it (migrations/000026).
		sp, err = s.identity.CreateServicePrincipal(ctx, tx, name, description, &callerUserID)
		if err != nil {
			return err
		}
		_, err = s.identity.CreatePrincipalForService(ctx, tx, sp.ID)
		return err
	})
	if err != nil {
		return identity.ServicePrincipal{}, fmt.Errorf("installation: create service principal: %w", err)
	}
	return sp, nil
}

// IssueCredentialFor issues a PAT for targetPrincipalID — any principal,
// not just the caller — after checking callerUserID holds
// CapCredentialManage. Distinct from adapters/http/tokens.go's self-only
// issueToken: that one never accepts a principal id from the request body
// at all (internal/identity has no authz layer of its own, ADR-0009's
// consequences); this is the deliberately separate, capability-gated
// feature that comment calls out as unbuilt.
func (s *Service) IssueCredentialFor(
	ctx context.Context, principalID, callerUserID, targetPrincipalID uuid.UUID, clientType string, expiresAt *time.Time,
) (string, identity.ClientCredential, error) {
	if err := s.checkCapability(ctx, principalID, callerUserID, CapCredentialManage); err != nil {
		return "", identity.ClientCredential{}, err
	}
	var token string
	var cred identity.ClientCredential
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		token, cred, err = s.identity.IssueCredential(ctx, tx, targetPrincipalID, clientType, expiresAt)
		return err
	})
	if err != nil {
		return "", identity.ClientCredential{}, fmt.Errorf("installation: issue credential: %w", err)
	}
	return token, cred, nil
}

// ListCredentialsFor lists every PAT belonging to targetPrincipalID, after
// checking callerUserID holds CapCredentialManage.
func (s *Service) ListCredentialsFor(ctx context.Context, principalID, callerUserID, targetPrincipalID uuid.UUID) ([]identity.ClientCredential, error) {
	if err := s.checkCapability(ctx, principalID, callerUserID, CapCredentialManage); err != nil {
		return nil, err
	}
	var creds []identity.ClientCredential
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		// Unfiltered on purpose, unlike the token screen: this is the admin's
		// credential view, and browser sessions are exactly what an incident
		// asks about (docs/adr/0019).
		creds, err = s.identity.ListCredentialsForPrincipal(ctx, tx, targetPrincipalID)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("installation: list credentials: %w", err)
	}
	return creds, nil
}

// RevokeCredentialFor revokes credentialID regardless of which principal it
// belongs to, after checking callerUserID holds CapCredentialManage — the
// admin counterpart to adapters/http/tokens.go's revokeToken, which only
// ever revokes the caller's own credential (ownership enforced in its
// WHERE clause). Uses identity.Store.RevokeCredentialByID, which has no
// such ownership restriction — that is exactly the point of this method.
func (s *Service) RevokeCredentialFor(ctx context.Context, principalID, callerUserID, credentialID uuid.UUID) error {
	if err := s.checkCapability(ctx, principalID, callerUserID, CapCredentialManage); err != nil {
		return err
	}
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		return s.identity.RevokeCredentialByID(ctx, tx, credentialID)
	})
	if err != nil {
		return fmt.Errorf("installation: revoke credential: %w", err)
	}
	return nil
}

// ListUsers returns one page of the installation's users.
//
// Gated on CapAdminRead rather than on a capability of its own: it is a read
// of installation-wide state, which is exactly what that capability already
// governs. It exists so that naming a person in an interface does not
// require knowing their UUID — every screen that adds a member, looks up a
// credential or grants a role had no way to offer a choice before this.
func (s *Service) ListUsers(ctx context.Context, principalID, callerUserID uuid.UUID, opts paging.Options) (paging.Page[identity.DirectoryUser], error) {
	if err := s.checkCapability(ctx, principalID, callerUserID, CapAdminRead); err != nil {
		return paging.Page[identity.DirectoryUser]{}, err
	}
	var page paging.Page[identity.DirectoryUser]
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		page, err = s.identity.ListUsers(ctx, tx, opts)
		return err
	})
	if err != nil {
		return paging.Page[identity.DirectoryUser]{}, fmt.Errorf("installation: list users: %w", err)
	}
	return page, nil
}

// ListServicePrincipals is ListUsers' counterpart for agents.
func (s *Service) ListServicePrincipals(ctx context.Context, principalID, callerUserID uuid.UUID, opts paging.Options) (paging.Page[identity.DirectoryAgent], error) {
	if err := s.checkCapability(ctx, principalID, callerUserID, CapAdminRead); err != nil {
		return paging.Page[identity.DirectoryAgent]{}, err
	}
	var page paging.Page[identity.DirectoryAgent]
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		page, err = s.identity.ListServicePrincipals(ctx, tx, opts)
		return err
	})
	if err != nil {
		return paging.Page[identity.DirectoryAgent]{}, fmt.Errorf("installation: list service principals: %w", err)
	}
	return page, nil
}

// Stats is what an installation holds, in cardinalities.
//
// Counts and one byte total, never a row-level value — see
// migrations/000024. The distinction matters because it is what lets an
// operator have this at all: an installation admin is not a member of every
// project and cannot read project content, so anything here that named a
// path, a summary or an email would be a widening of that boundary rather
// than a report about it.
type Stats struct {
	Projects          int64
	Organizations     int64
	Users             int64
	ServicePrincipals int64

	Artifacts        int64
	ArtifactVersions int64
	// StoredBytes is the logical size of every artifact version ever
	// written. Not disk usage: the database cannot see the object store, and
	// a number labelled "disk" that came from SQL would be a guess.
	StoredBytes int64

	Skills        int64
	SkillVersions int64
	SkillBindings int64

	HandoffsOpen    int64
	HandoffsClaimed int64
	HandoffsClosed  int64

	Conversations  int64
	HistoryObjects int64

	Events         int64
	AuditRecords   int64
	SecurityEvents int64
	Secrets        int64
	GateBindings   int64

	CredentialsActive  int64
	CredentialsRevoked int64
	CredentialsExpired int64
	// ActiveLast7Days is the number that answers "is this used" rather than
	// "what is stored": credentials that made a request in the last week.
	ActiveLast7Days int64

	OldestProject  *time.Time
	NewestActivity *time.Time
}

// Stats reports installation-wide usage.
//
// Gated on CapAdminRead, the same capability that governs every other
// installation-wide read here. The counting itself happens in
// cc_installation_stats(), a SECURITY DEFINER function, because almost
// everything worth counting sits behind RLS and this caller is deliberately
// not a member of every project — counting through their own view would
// report "the installation holds 2 artifacts" when the truth is "you can see
// 2", which is worse than reporting nothing.
func (s *Service) Stats(ctx context.Context, principalID, callerUserID uuid.UUID) (Stats, error) {
	if err := s.checkCapability(ctx, principalID, callerUserID, CapAdminRead); err != nil {
		return Stats{}, err
	}

	var st Stats
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT * FROM cc_installation_stats()`).Scan(
			&st.Projects, &st.Organizations, &st.Users, &st.ServicePrincipals,
			&st.Artifacts, &st.ArtifactVersions, &st.StoredBytes,
			&st.Skills, &st.SkillVersions, &st.SkillBindings,
			&st.HandoffsOpen, &st.HandoffsClaimed, &st.HandoffsClosed,
			&st.Conversations, &st.HistoryObjects,
			&st.Events, &st.AuditRecords, &st.SecurityEvents, &st.Secrets, &st.GateBindings,
			&st.CredentialsActive, &st.CredentialsRevoked, &st.CredentialsExpired,
			&st.ActiveLast7Days,
			&st.OldestProject, &st.NewestActivity,
		)
	})
	if err != nil {
		return Stats{}, fmt.Errorf("installation: stats: %w", err)
	}
	return st, nil
}

// Week is one week of installation-wide activity, UTC, starting Monday.
// Counts and durations only — see migrations/000028.
type Week struct {
	Start time.Time

	ActiveUsers  int64
	ActiveAgents int64

	ProjectsCreated int64
	HandoffsCreated int64
	HandoffsClaimed int64
	// HandoffsClaimedByOtherClient is the cross-client takeover: claimed
	// through a different credential than the one that created it.
	HandoffsClaimedByOtherClient int64
	HandoffsClosed               int64
	// MedianToClaim is nil in a week with no claims — not zero, which would
	// read as "instant".
	MedianToClaim *time.Duration

	Events int64
}

// HandoffFlow is how the handoff loop performs over the installation's life.
// Durations are nil until there is something to take a median of.
type HandoffFlow struct {
	Handoffs             int64
	Claimed              int64
	ClaimedByOtherClient int64
	StaleOpen            int64

	MedianToClaim      *time.Duration
	P90ToClaim         *time.Duration
	MedianClaimToClose *time.Duration

	Projects              int64
	ProjectsWithTakeover  int64
	MedianToFirstTakeover *time.Duration
}

// MaxTrendWeeks is the longest series Trends returns; the SQL clamps to the
// same bound, so this exists for callers that want to say so.
const MaxTrendWeeks = 52

// Trends reports activity per week for the last `weeks` weeks (the current
// one included) and the handoff loop's performance overall.
//
// Gated and computed exactly like Stats, for the same reason: the counting
// has to see past RLS, so it happens in SECURITY DEFINER functions that
// return nothing but numbers (migrations/000028).
func (s *Service) Trends(ctx context.Context, principalID, callerUserID uuid.UUID, weeks int) ([]Week, HandoffFlow, error) {
	if err := s.checkCapability(ctx, principalID, callerUserID, CapAdminRead); err != nil {
		return nil, HandoffFlow{}, err
	}
	weeks = min(max(weeks, 1), MaxTrendWeeks)

	var (
		out                               []Week
		flow                              HandoffFlow
		toClaim, p90, toClose, toTakeover *float64
	)
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT * FROM cc_installation_trends($1)`, weeks)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				w      Week
				median *float64
			)
			if err := rows.Scan(&w.Start, &w.ActiveUsers, &w.ActiveAgents,
				&w.ProjectsCreated, &w.HandoffsCreated, &w.HandoffsClaimed,
				&w.HandoffsClaimedByOtherClient, &w.HandoffsClosed, &median, &w.Events); err != nil {
				return err
			}
			w.MedianToClaim = seconds(median)
			out = append(out, w)
		}
		if err := rows.Err(); err != nil {
			return err
		}

		return tx.QueryRow(ctx, `SELECT * FROM cc_installation_handoff_flow()`).Scan(
			&flow.Handoffs, &flow.Claimed, &flow.ClaimedByOtherClient, &flow.StaleOpen,
			&toClaim, &p90, &toClose,
			&flow.Projects, &flow.ProjectsWithTakeover, &toTakeover,
		)
	})
	if err != nil {
		return nil, HandoffFlow{}, fmt.Errorf("installation: trends: %w", err)
	}
	flow.MedianToClaim, flow.P90ToClaim = seconds(toClaim), seconds(p90)
	flow.MedianClaimToClose, flow.MedianToFirstTakeover = seconds(toClose), seconds(toTakeover)
	return out, flow, nil
}

// seconds turns a nullable SQL interval in seconds into a nullable Duration,
// keeping "no value" distinct from zero.
func seconds(v *float64) *time.Duration {
	if v == nil {
		return nil
	}
	d := time.Duration(*v * float64(time.Second))
	return &d
}
