package mcp

import (
	"context"
	"errors"
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

type myOrganizationResult struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

// organizationMemberResult carries the person, not just their id: a list of
// UUIDs answers "who is in this organization" with "I am not going to tell
// you", and a model reading it has no way to look them up.
type organizationMemberResult struct {
	UserID      string `json:"user_id"`
	Role        string `json:"role"`
	Email       string `json:"email,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	CreatedAt   string `json:"created_at"`
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

	// The two reads this surface was missing. Without them an agent could
	// create an organization and add people to it but never find out which
	// organizations it belongs to or who is already in one — writes without
	// the matching reads.
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "organization.list",
		Description: "List every organization you belong to, with your role. A service principal belongs to none — that is the correct answer for one, not a refusal.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, []myOrganizationResult, error) {
		callerID, err := callerUserID(ctx)
		if errors.Is(err, organizations.ErrForbidden) {
			// A service principal: no organizations, which is a fact rather
			// than a refusal.
			return nil, []myOrganizationResult{}, nil
		}
		if err != nil {
			return nil, nil, err
		}
		memberships, err := deps.Organizations.ListMine(ctx, principalID, callerID)
		if err != nil {
			return nil, nil, err
		}
		out := make([]myOrganizationResult, len(memberships))
		for i, m := range memberships {
			out[i] = myOrganizationResult{
				ID: m.Organization.ID.String(), Name: m.Organization.Name, Role: string(m.Role),
			}
		}
		return nil, out, nil
	})

	type listMembersArgs struct {
		OrganizationID string `json:"organization_id" jsonschema:"organization UUID"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "organization.list_members",
		Description: "List an organization's members. Any member may read this; who added them is in the audit trail, which is not.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args listMembersArgs) (*sdkmcp.CallToolResult, []organizationMemberResult, error) {
		orgID, err := uuid.Parse(args.OrganizationID)
		if err != nil {
			return nil, nil, err
		}
		callerID, err := callerUserID(ctx)
		if err != nil {
			return nil, nil, err
		}
		members, err := deps.Organizations.ListMembers(ctx, principalID, callerID, orgID)
		if err != nil {
			return nil, nil, err
		}
		out := make([]organizationMemberResult, len(members))
		for i, m := range members {
			out[i] = organizationMemberResult{
				UserID: m.UserID.String(), Role: string(m.Role),
				Email: m.Email, DisplayName: m.DisplayName,
				CreatedAt: m.CreatedAt.Format(time.RFC3339),
			}
		}
		return nil, out, nil
	})
}
