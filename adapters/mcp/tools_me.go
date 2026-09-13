package mcp

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/identity"
)

// meResult is who the caller is acting as. `principal_id` is the id every
// project-scoped tool means when it says "principal", so it is the useful
// half — an agent that knows it can answer "am I already a member of this
// project" from project.list_members without asking anyone.
type meResult struct {
	PrincipalID string `json:"principal_id"`
	Kind        string `json:"kind"` // "user" | "service"
	Email       string `json:"email,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Name        string `json:"name,omitempty"` // a ServicePrincipal's name
	// IsInstallationAdmin decides whether the installation.* tools will
	// answer at all. Knowing it up front is the difference between a client
	// offering an operation and a client discovering a refusal.
	IsInstallationAdmin bool `json:"is_installation_admin"`
}

// registerMeTools answers "who am I". Every other tool on this surface acts
// on behalf of the caller without ever telling it who that is, which leaves
// an agent unable to recognise its own writes in an audit trail or to tell a
// refusal from a mistake.
func registerMeTools(server *sdkmcp.Server, deps Deps, principalID uuid.UUID) {
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "me",
		Description: "Who this token acts as, and whether it administers the installation. `principal_id` is what project memberships and audit entries name.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, meResult, error) {
		var out meResult
		var userID *uuid.UUID

		err := db.WithPrincipal(ctx, deps.Pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
			store := identity.NewStore()
			p, err := store.GetPrincipal(ctx, tx, principalID)
			if err != nil {
				return err
			}
			out.PrincipalID = p.ID.String()
			out.Kind = string(p.Kind)

			switch p.Kind {
			case identity.PrincipalKindUser:
				u, err := store.GetUser(ctx, tx, *p.UserID)
				if err != nil {
					return err
				}
				out.Email, out.DisplayName, userID = u.Email, u.DisplayName, p.UserID
			case identity.PrincipalKindService:
				sp, err := store.GetServicePrincipal(ctx, tx, *p.ServicePrincipalID)
				if err != nil {
					return err
				}
				out.Name = sp.Name
			}
			return nil
		})
		if err != nil {
			return nil, meResult{}, err
		}

		// A separate transaction, not nested: the same reasoning as
		// adapters/http's getMe — nesting risks exhausting a small pool.
		if userID != nil {
			isAdmin, err := deps.Installation.IsAdmin(ctx, principalID, *userID)
			if err != nil {
				return nil, meResult{}, err
			}
			out.IsInstallationAdmin = isAdmin
		}
		return nil, out, nil
	})
}
