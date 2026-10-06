package http

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/identity"
	"github.com/koduj-dev/context-commander/internal/projects"
	"github.com/koduj-dev/context-commander/internal/security"
)

// authenticateMiddleware resolves the caller's Principal from a bearer PAT,
// via the same identity.Authenticate every adapter uses (docs/adr/0009) —
// this package never parses or verifies a token itself, and nothing about
// identity is trusted from the client beyond the token.
func (d Deps) authenticateMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := middleware.GetReqID(r.Context())

		token, fromCookie, ok := credentialFrom(r)
		if !ok {
			// Not recorded: a request with no credential at all is far more
			// often a health check, a crawler or a browser hitting the API
			// by accident than an attack, and drowning the security record
			// in those would make it useless for the cases that matter.
			writeError(w, http.StatusUnauthorized, "unauthorized",
				"missing or malformed Authorization: Bearer <token> header, or session cookie")
			return
		}

		// A cookie is attached by the browser whoever caused the request, so
		// a request authenticated by one must prove it came from this app's
		// own code (docs/adr/0019). A bearer token is never attached on
		// anyone's behalf and needs no such proof.
		//
		// Checked here rather than per handler: a CSRF defence that each
		// endpoint has to remember to call is a CSRF defence that the next
		// endpoint will not have.
		if fromCookie && !requireBrowserRequest(w, r) {
			return
		}

		// The credential lookup below is a database round trip that an
		// unauthenticated caller can trigger, so it is the one piece of
		// work worth bounding globally (internal/ratelimit).
		release, ok := d.Limiter.AcquireAuth()
		if !ok {
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusServiceUnavailable, "overloaded", "too many authentication attempts in flight; retry shortly")
			return
		}
		cred, err := identity.Authenticate(r.Context(), d.Pool, token)
		release()
		if err != nil {
			d.Security.Record(r.Context(), security.Event{
				Kind:         security.KindAuthenticationFailed,
				CredentialID: credentialIDFromToken(token),
				Reason:       "presented token did not resolve to an active credential",
				RequestID:    requestID,
			})
			writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or expired token")
			return
		}

		if !d.Limiter.AllowCredential(cred.ID) {
			d.Security.Record(r.Context(), security.Event{
				Kind:         security.KindRateLimited,
				PrincipalID:  &cred.PrincipalID,
				CredentialID: &cred.ID,
				Reason:       "credential exceeded its request rate limit",
				RequestID:    requestID,
			})
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusTooManyRequests, "rate_limited", "this credential is making requests too quickly")
			return
		}

		ctx := context.WithValue(r.Context(), ctxKeyPrincipalID, cred.PrincipalID)
		ctx = context.WithValue(ctx, ctxKeyClientID, cred.ID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// credentialIDFromToken recovers just the credential id from a token that
// failed to authenticate, so the security record can say WHICH credential
// was presented. The secret half is never touched: it is not parsed out, not
// logged, and not stored. A token too malformed to yield an id yields nil,
// which is itself the useful signal that someone is sending noise rather
// than a stale credential.
func credentialIDFromToken(token string) *uuid.UUID {
	id, _, err := identity.ParseToken(token)
	if err != nil {
		return nil
	}
	return &id
}

// resolveProjectMiddleware resolves {namespace}/{slug} into a Project and
// puts it in the request context. Resolve itself is RLS-backed
// (internal/projects) — a non-member gets ErrNotFound here, not a 403, so
// project existence isn't leaked to outsiders.
func (d Deps) resolveProjectMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := chi.URLParam(r, "namespace") + "/" + chi.URLParam(r, "slug")
		principalID := principalIDFrom(r.Context())

		var project projects.Project
		err := db.WithPrincipal(r.Context(), d.Pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
			var err error
			project, err = d.Projects.Resolve(r.Context(), tx, path)
			return err
		})
		if err != nil {
			writeDomainError(w, err)
			return
		}

		ctx := context.WithValue(r.Context(), ctxKeyProject, project)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// credentialFrom finds the caller's token: an Authorization bearer header
// first, then the session cookie.
//
// Bearer wins when both are present. That is the case of an agent or a script
// calling the API from a browser context that also happens to hold a session,
// and the explicit header is the more deliberate of the two — a cookie is
// sent whether or not the caller meant to.
//
// fromCookie is returned rather than inferred later, because the CSRF
// requirement applies to exactly one of these two and getting that backwards
// either breaks every agent or protects nothing.
func credentialFrom(r *http.Request) (token string, fromCookie, ok bool) {
	if bearer, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); found && bearer != "" {
		return bearer, false, true
	}
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		return c.Value, true, true
	}
	return "", false, false
}
