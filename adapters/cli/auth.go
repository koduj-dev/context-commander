package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/identity"
)

const tokenEnv = "CC_TOKEN"

func tokenFlag(cmd *cobra.Command) {
	cmd.Flags().String("token", "", fmt.Sprintf("PAT issued by 'ccd token create' (defaults to $%s)", tokenEnv))
}

func resolveToken(cmd *cobra.Command) (string, error) {
	if v, _ := cmd.Flags().GetString("token"); v != "" {
		return v, nil
	}
	if v := os.Getenv(tokenEnv); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("no token: pass --token or set $%s (see 'ccd token create')", tokenEnv)
}

// authenticate is a thin CLI-flavored wrapper over identity.Authenticate —
// see that function's doc for why every adapter shares it instead of
// verifying tokens itself.
func authenticate(ctx context.Context, pool *db.Pool, token string) (principalID uuid.UUID, clientID uuid.UUID, err error) {
	cred, err := identity.Authenticate(ctx, pool, token)
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("cli: %w", err)
	}
	return cred.PrincipalID, cred.ID, nil
}
