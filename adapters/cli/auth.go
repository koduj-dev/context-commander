package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

// authenticate parses and verifies a PAT (docs/adr/0009-pat-format-and-bootstrap.md):
// an O(1) lookup by the credential ID embedded in the token, then a
// constant-time comparison of the secret part against the stored hash —
// never a scan-and-compare over every credential.
func authenticate(ctx context.Context, pool *db.Pool, token string) (principalID uuid.UUID, clientID uuid.UUID, err error) {
	credentialID, secret, err := identity.ParseToken(token)
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("cli: %w", err)
	}

	store := identity.NewStore()
	var cred identity.ClientCredential
	err = db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		var err error
		cred, err = store.GetCredential(ctx, tx, credentialID)
		return err
	})
	if errors.Is(err, identity.ErrNotFound) {
		return uuid.Nil, uuid.Nil, errors.New("cli: invalid token")
	}
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("cli: authenticate: %w", err)
	}

	if !identity.VerifySecret(secret, cred.TokenHash) {
		return uuid.Nil, uuid.Nil, errors.New("cli: invalid token")
	}
	if !cred.Active(time.Now()) {
		return uuid.Nil, uuid.Nil, errors.New("cli: token is revoked or expired")
	}

	// Best-effort — a failure to record last_used_at must not block the
	// actual request.
	_ = db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		return store.TouchCredentialLastUsed(ctx, tx, credentialID)
	})

	return cred.PrincipalID, credentialID, nil
}
