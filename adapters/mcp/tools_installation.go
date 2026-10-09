package mcp

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/identity"
	"github.com/koduj-dev/context-commander/internal/installation"
)

type userResult struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	CreatedAt   string `json:"created_at"`
}

type servicePrincipalResult struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	CreatedAt   string `json:"created_at"`
}

type issuedTokenResult struct {
	Token        string  `json:"token"`
	CredentialID string  `json:"credential_id"`
	ClientType   string  `json:"client_type"`
	ExpiresAt    *string `json:"expires_at,omitempty"`
}

type credentialResult struct {
	ID         string  `json:"id"`
	ClientType string  `json:"client_type"`
	CreatedAt  string  `json:"created_at"`
	LastUsedAt *string `json:"last_used_at,omitempty"`
	ExpiresAt  *string `json:"expires_at,omitempty"`
	RevokedAt  *string `json:"revoked_at,omitempty"`
}

func toCredentialResult(c identity.ClientCredential) credentialResult {
	r := credentialResult{ID: c.ID.String(), ClientType: c.ClientType, CreatedAt: c.CreatedAt.Format(time.RFC3339)}
	if c.LastUsedAt != nil {
		s := c.LastUsedAt.Format(time.RFC3339)
		r.LastUsedAt = &s
	}
	if c.ExpiresAt != nil {
		s := c.ExpiresAt.Format(time.RFC3339)
		r.ExpiresAt = &s
	}
	if c.RevokedAt != nil {
		s := c.RevokedAt.Format(time.RFC3339)
		r.RevokedAt = &s
	}
	return r
}

type adminResult struct {
	UserID    string  `json:"user_id"`
	Role      string  `json:"role"`
	GrantedBy *string `json:"granted_by,omitempty"`
	GrantedAt string  `json:"granted_at"`
}

// registerInstallationTools mirrors registerOrganizationTools' shape
// (docs/adr/0017): every tool resolves the caller's userID first
// (installation_admins is keyed by User, never Principal), then lets
// deps.Installation's own capability check decide ALLOW/DENY. There is
// deliberately no tool to grant/revoke installation-admin rights — see
// adapters/http/installation.go's package doc for why that stays a CLI/
// --database-url-only action.
func registerInstallationTools(server *sdkmcp.Server, deps Deps, principalID, _ uuid.UUID) {
	// callerUserID resolves the human User behind the authenticated
	// principal — installation_admins is keyed by User, never Principal
	// (docs/adr/0017, mirroring docs/adr/0006's organization reasoning) —
	// same shape as registerOrganizationTools' callerUserID closure.
	callerUserID := func(ctx context.Context) (uuid.UUID, error) {
		var userID uuid.UUID
		err := db.WithPrincipal(ctx, deps.Pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
			p, err := identity.NewStore().GetPrincipal(ctx, tx, principalID)
			if err != nil {
				return err
			}
			if p.UserID == nil {
				return installation.ErrForbidden
			}
			userID = *p.UserID
			return nil
		})
		return userID, err
	}

	type createUserArgs struct {
		Email       string `json:"email" jsonschema:"new user's email"`
		DisplayName string `json:"display_name" jsonschema:"new user's display name"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "installation.create_user",
		Description: "Create a user. Requires installation-admin rights (docs/adr/0017).",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args createUserArgs) (*sdkmcp.CallToolResult, userResult, error) {
		userID, err := callerUserID(ctx)
		if err != nil {
			return nil, userResult{}, err
		}
		u, err := // nil: MCP has no password surface, deliberately. A model creating
			// accounts that can immediately be signed in to is a larger decision
			// than adding a field (docs/adr/0019).
			deps.Installation.CreateUser(ctx, principalID, userID, args.Email, args.DisplayName, nil)
		if err != nil {
			return nil, userResult{}, err
		}
		return nil, userResult{ID: u.ID.String(), Email: u.Email, DisplayName: u.DisplayName, CreatedAt: u.CreatedAt.Format(time.RFC3339)}, nil
	})

	type createServicePrincipalArgs struct {
		Name        string `json:"name" jsonschema:"service principal (agent) name"`
		Description string `json:"description,omitempty" jsonschema:"optional description"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "installation.create_service_principal",
		Description: "Register a service principal (agent). Requires installation-admin rights.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args createServicePrincipalArgs) (*sdkmcp.CallToolResult, servicePrincipalResult, error) {
		userID, err := callerUserID(ctx)
		if err != nil {
			return nil, servicePrincipalResult{}, err
		}
		sp, err := deps.Installation.CreateServicePrincipal(ctx, principalID, userID, args.Name, args.Description)
		if err != nil {
			return nil, servicePrincipalResult{}, err
		}
		return nil, servicePrincipalResult{ID: sp.ID.String(), Name: sp.Name, Description: sp.Description, CreatedAt: sp.CreatedAt.Format(time.RFC3339)}, nil
	})

	type issueTokenForArgs struct {
		PrincipalID      string `json:"principal_id" jsonschema:"UUID of the principal to issue a token for"`
		ClientType       string `json:"client_type" jsonschema:"e.g. claude-code, chatgpt, cli"`
		ExpiresInSeconds *int64 `json:"expires_in_seconds,omitempty" jsonschema:"optional expiry, in seconds"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "installation.issue_token",
		Description: "Issue a PAT for any principal. Requires installation-admin rights. The token is shown once and cannot be recovered afterwards.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args issueTokenForArgs) (*sdkmcp.CallToolResult, issuedTokenResult, error) {
		targetID, err := uuid.Parse(args.PrincipalID)
		if err != nil {
			return nil, issuedTokenResult{}, err
		}
		userID, err := callerUserID(ctx)
		if err != nil {
			return nil, issuedTokenResult{}, err
		}
		var expiresAt *time.Time
		if args.ExpiresInSeconds != nil {
			t := time.Now().Add(time.Duration(*args.ExpiresInSeconds) * time.Second)
			expiresAt = &t
		}
		token, cred, err := deps.Installation.IssueCredentialFor(ctx, principalID, userID, targetID, args.ClientType, expiresAt)
		if err != nil {
			return nil, issuedTokenResult{}, err
		}
		res := issuedTokenResult{Token: token, CredentialID: cred.ID.String(), ClientType: cred.ClientType}
		if cred.ExpiresAt != nil {
			s := cred.ExpiresAt.Format(time.RFC3339)
			res.ExpiresAt = &s
		}
		return nil, res, nil
	})

	type listTokensForArgs struct {
		PrincipalID string `json:"principal_id" jsonschema:"UUID of the principal whose tokens to list"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "installation.list_tokens",
		Description: "List any principal's tokens (metadata only, never the secret). Requires installation-admin rights.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args listTokensForArgs) (*sdkmcp.CallToolResult, []credentialResult, error) {
		targetID, err := uuid.Parse(args.PrincipalID)
		if err != nil {
			return nil, nil, err
		}
		userID, err := callerUserID(ctx)
		if err != nil {
			return nil, nil, err
		}
		creds, err := deps.Installation.ListCredentialsFor(ctx, principalID, userID, targetID)
		if err != nil {
			return nil, nil, err
		}
		out := make([]credentialResult, len(creds))
		for i, c := range creds {
			out[i] = toCredentialResult(c)
		}
		return nil, out, nil
	})

	type revokeTokenForArgs struct {
		CredentialID string `json:"credential_id" jsonschema:"UUID of the credential to revoke"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "installation.revoke_token",
		Description: "Revoke any principal's token by credential id. Requires installation-admin rights.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args revokeTokenForArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		credID, err := uuid.Parse(args.CredentialID)
		if err != nil {
			return nil, struct{}{}, err
		}
		userID, err := callerUserID(ctx)
		if err != nil {
			return nil, struct{}{}, err
		}
		if err := deps.Installation.RevokeCredentialFor(ctx, principalID, userID, credID); err != nil {
			return nil, struct{}{}, err
		}
		return nil, struct{}{}, nil
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "installation.list_admins",
		Description: "List installation admins. Requires installation-admin rights.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, []adminResult, error) {
		userID, err := callerUserID(ctx)
		if err != nil {
			return nil, nil, err
		}
		admins, err := deps.Installation.ListAdmins(ctx, principalID, userID)
		if err != nil {
			return nil, nil, err
		}
		out := make([]adminResult, len(admins))
		for i, a := range admins {
			r := adminResult{UserID: a.UserID.String(), Role: string(a.Role), GrantedAt: a.GrantedAt.Format(time.RFC3339)}
			if a.GrantedBy != nil {
				s := a.GrantedBy.String()
				r.GrantedBy = &s
			}
			out[i] = r
		}
		return nil, out, nil
	})

	registerDirectoryTools(server, deps, principalID, callerUserID)
}

// registerDirectoryTools adds the reads that let an agent name a person or
// another agent instead of demanding a UUID it has no way to obtain.
//
// Both are installation-admin only, exactly like their HTTP counterparts:
// the directory says who exists on this installation, which is not something
// every credential should be able to enumerate.
func registerDirectoryTools(server *sdkmcp.Server, deps Deps, principalID uuid.UUID, callerUserID func(context.Context) (uuid.UUID, error)) {
	type listArgs struct {
		Limit  int    `json:"limit,omitempty" jsonschema:"maximum rows to return"`
		Cursor string `json:"cursor,omitempty" jsonschema:"continue from a previous next_cursor; ordered oldest first"`
	}

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "installation.list_users",
		Description: "List the people on this installation. Each carries both ids: `id` names them in an organization, `principal_id` names them in a project. Requires installation-admin rights.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args listArgs) (*sdkmcp.CallToolResult, pagedResult[directoryUserResult], error) {
		userID, err := callerUserID(ctx)
		if err != nil {
			return nil, pagedResult[directoryUserResult]{}, err
		}
		page, err := deps.Installation.ListUsers(ctx, principalID, userID, pagingOptions(args.Limit, args.Cursor))
		if err != nil {
			return nil, pagedResult[directoryUserResult]{}, err
		}
		out := make([]directoryUserResult, len(page.Items))
		for i, u := range page.Items {
			out[i] = directoryUserResult{
				ID: u.ID.String(), PrincipalID: u.PrincipalID.String(),
				Email: u.Email, DisplayName: u.DisplayName,
				CreatedAt: u.CreatedAt.Format(time.RFC3339),
			}
		}
		return nil, paged(out, page.NextCursor), nil
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "installation.list_service_principals",
		Description: "List the agents registered on this installation. `principal_id` is what a token is issued for and what a project membership names; `created_by_email` is who registered the agent, which is provenance and grants it nothing. Requires installation-admin rights.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args listArgs) (*sdkmcp.CallToolResult, pagedResult[directoryAgentResult], error) {
		userID, err := callerUserID(ctx)
		if err != nil {
			return nil, pagedResult[directoryAgentResult]{}, err
		}
		page, err := deps.Installation.ListServicePrincipals(ctx, principalID, userID, pagingOptions(args.Limit, args.Cursor))
		if err != nil {
			return nil, pagedResult[directoryAgentResult]{}, err
		}
		out := make([]directoryAgentResult, len(page.Items))
		for i, a := range page.Items {
			out[i] = directoryAgentResult{
				ID: a.ID.String(), PrincipalID: a.PrincipalID.String(),
				Name: a.Name, Description: a.Description,
				CreatedAt:      a.CreatedAt.Format(time.RFC3339),
				CreatedByEmail: a.CreatedByEmail, CreatedByName: a.CreatedByName,
			}
			if a.CreatedBy != nil {
				out[i].CreatedBy = a.CreatedBy.String()
			}
		}
		return nil, paged(out, page.NextCursor), nil
	})
}

// The two ids are not interchangeable — see identity.DirectoryUser.
type directoryUserResult struct {
	ID          string `json:"id"`
	PrincipalID string `json:"principal_id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	CreatedAt   string `json:"created_at"`
}

type directoryAgentResult struct {
	ID          string `json:"id"`
	PrincipalID string `json:"principal_id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	CreatedAt   string `json:"created_at"`
	// Who registered this agent, resolved to a person (migrations/000026).
	// Provenance, not authority: the agent's access comes from its own
	// project memberships (docs/adr/0004). Omitted when nobody is recorded.
	CreatedBy      string `json:"created_by,omitempty"`
	CreatedByEmail string `json:"created_by_email,omitempty"`
	CreatedByName  string `json:"created_by_name,omitempty"`
}
