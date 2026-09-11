// Package members is the authorization chokepoint for project membership
// (docs/adr/0014-authz-check-in-domain-service.md): adding a member and
// listing members go through Service, which checks the capability itself,
// so no adapter has to remember to.
//
// It is a package of its own rather than a Service inside internal/projects
// because internal/auth imports internal/projects for projects.Role — a
// projects.Service holding an *auth.AuthzService would be an import cycle.
// Splitting the store (internal/projects, no authz) from the service (here,
// authz) is the smallest resolution that keeps ADR 0014's rule intact.
package members

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/projects"
)

var (
	ErrForbidden = errors.New("members: forbidden")
	// ErrInvalidRole is returned for a role string that is not one of the
	// four roles. Adapters already reject a malformed role before getting
	// here; this is the domain's own guard, so a future caller cannot
	// insert an unrecognized role that RoleCan would then silently map to
	// no capabilities at all.
	ErrInvalidRole = errors.New(`members: role must be one of OWNER, ADMIN, CONTRIBUTOR, READER`)
)

type Service struct {
	pool     *db.Pool
	projects *projects.Store
	authz    *auth.AuthzService
}

func NewService(pool *db.Pool, projectStore *projects.Store, authz *auth.AuthzService) *Service {
	return &Service{pool: pool, projects: projectStore, authz: authz}
}

func (s *Service) checkCapability(ctx context.Context, principalID, projectID uuid.UUID, cap auth.Capability) error {
	allowed, err := s.authz.Can(ctx, principalID, projectID, cap)
	if err != nil {
		return fmt.Errorf("members: authorization check: %w", err)
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

// Add adds targetPrincipalID to projectID with role.
//
// Granting OWNER requires OWNER (auth.CapMemberGrantOwner), not merely
// auth.CapMemberManage: ADMIN holds member.manage but deliberately not
// project.delete, so without this second check an ADMIN could add a
// principal they control as OWNER and exercise ownership through it. The
// check lives here rather than in the HTTP and MCP adapters that used to
// carry a bare CapMemberManage check each, so a third adapter cannot
// reintroduce the hole by forgetting it.
func (s *Service) Add(ctx context.Context, principalID, projectID, targetPrincipalID uuid.UUID, role projects.Role) error {
	switch role {
	case projects.RoleOwner, projects.RoleAdmin, projects.RoleContributor, projects.RoleReader:
	default:
		return ErrInvalidRole
	}

	if err := s.checkCapability(ctx, principalID, projectID, auth.CapMemberManage); err != nil {
		return err
	}
	if role == projects.RoleOwner {
		if err := s.checkCapability(ctx, principalID, projectID, auth.CapMemberGrantOwner); err != nil {
			return err
		}
	}

	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		_, err := s.projects.AddMember(ctx, tx, projectID, targetPrincipalID, role)
		return err
	})
	if err != nil {
		return fmt.Errorf("members: add: %w", err)
	}
	return nil
}

// List returns every member of projectID. CapMemberRead (unlike
// CapMemberManage) is granted to every role including READER, so any member
// can see who else is on the project even if they cannot change the list.
func (s *Service) List(ctx context.Context, principalID, projectID uuid.UUID) ([]projects.Membership, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapMemberRead); err != nil {
		return nil, err
	}

	var list []projects.Membership
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		list, err = s.projects.ListMembers(ctx, tx, projectID)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("members: list: %w", err)
	}
	return list, nil
}
