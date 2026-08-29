package mcp

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/identity"
	"github.com/koduj-dev/context-commander/internal/organizations"
)

type organizationResult struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

type membershipResult struct {
	OrganizationID string `json:"organization_id"`
	UserID         string `json:"user_id"`
	Role           string `json:"role"`
}

func registerOrganizationTools(server *sdkmcp.Server, deps Deps, principalID, _ uuid.UUID) {
	// callerUserID resolves the human User behind the authenticated
	// principal — organization_members is keyed by User, never Principal
	// (docs/adr/0006: service principals are never organization members).
	callerUserID := func(ctx context.Context) (uuid.UUID, error) {
		var userID uuid.UUID
		err := db.WithPrincipal(ctx, deps.Pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
			p, err := identity.NewStore().GetPrincipal(ctx, tx, principalID)
			if err != nil {
				return err
			}
			if p.UserID == nil {
				return organizations.ErrForbidden
			}
			userID = *p.UserID
			return nil
		})
		return userID, err
	}

	type createArgs struct {
		Name string `json:"name" jsonschema:"organization name"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "organization.create",
		Description: "Create an organization; the caller becomes its first OWNER.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args createArgs) (*sdkmcp.CallToolResult, organizationResult, error) {
		userID, err := callerUserID(ctx)
		if err != nil {
			return nil, organizationResult{}, err
		}
		org, err := deps.Organizations.Create(ctx, principalID, userID, args.Name)
		if err != nil {
			return nil, organizationResult{}, err
		}
		return nil, organizationResult{ID: org.ID.String(), Name: org.Name, CreatedAt: org.CreatedAt.Format(time.RFC3339)}, nil
	})

	type addMemberArgs struct {
		OrganizationID string `json:"organization_id" jsonschema:"organization UUID"`
		UserID         string `json:"user_id" jsonschema:"UUID of the user to add"`
		Role           string `json:"role" jsonschema:"OWNER, ADMIN, or MEMBER"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "organization.add_member",
		Description: "Add a member to an organization. Requires OWNER or ADMIN on that organization.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args addMemberArgs) (*sdkmcp.CallToolResult, membershipResult, error) {
		orgID, err := uuid.Parse(args.OrganizationID)
		if err != nil {
			return nil, membershipResult{}, err
		}
		newUserID, err := uuid.Parse(args.UserID)
		if err != nil {
			return nil, membershipResult{}, err
		}
		callerID, err := callerUserID(ctx)
		if err != nil {
			return nil, membershipResult{}, err
		}
		m, err := deps.Organizations.AddMember(ctx, principalID, callerID, orgID, newUserID, organizations.Role(args.Role))
		if err != nil {
			return nil, membershipResult{}, err
		}
		return nil, membershipResult{OrganizationID: m.OrganizationID.String(), UserID: m.UserID.String(), Role: string(m.Role)}, nil
	})
}
