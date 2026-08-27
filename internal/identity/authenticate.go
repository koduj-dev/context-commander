package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
)

var ErrInvalidToken = errors.New("identity: invalid token")

// Authenticate is the one place a presented PAT turns into a ClientCredential
// — every adapter (CLI, HTTP, MCP) calls this, not its own copy, so the
// verification steps (O(1) lookup, constant-time compare, active check)
// can't drift between adapters (docs/adr/0009-pat-format-and-bootstrap.md,
// same "domain owns the chokepoint" reasoning as
// docs/adr/0014-authz-check-in-domain-service.md for authorization).
func Authenticate(ctx context.Context, pool *db.Pool, token string) (ClientCredential, error) {
	credentialID, secret, err := ParseToken(token)
	if err != nil {
		return ClientCredential{}, ErrInvalidToken
	}

	store := NewStore()
	var cred ClientCredential
	err = db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		var err error
		cred, err = store.GetCredential(ctx, tx, credentialID)
		return err
	})
	if errors.Is(err, ErrNotFound) {
		return ClientCredential{}, ErrInvalidToken
	}
	if err != nil {
		return ClientCredential{}, fmt.Errorf("identity: authenticate: %w", err)
	}

	if !VerifySecret(secret, cred.TokenHash) {
		return ClientCredential{}, ErrInvalidToken
	}
	if !cred.Active(time.Now()) {
		return ClientCredential{}, ErrInvalidToken
	}

	// Best-effort — a failure to record last_used_at must not block the
	// actual request.
	_ = db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		return store.TouchCredentialLastUsed(ctx, tx, credentialID)
	})

	return cred, nil
}
