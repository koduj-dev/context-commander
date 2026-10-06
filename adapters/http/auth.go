package http

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/identity"
	"github.com/koduj-dev/context-commander/internal/installation"
	"github.com/koduj-dev/context-commander/internal/security"
)

// sessionCookie carries a browser session's token. HttpOnly, so the SPA
// cannot read it and neither can anything injected into the SPA — the reason
// for moving off localStorage at all (docs/adr/0019).
const sessionCookie = "cc_session"

// csrfHeader is what makes cookie authentication safe without a token pair.
//
// A cross-site <form> cannot set a custom header, and a cross-origin
// fetch/XHR that tries is stopped by the CORS preflight, which no middleware
// here answers — the SPA is served from the same origin as the API by the
// same binary, so it never needs CORS and nothing else is granted it.
// SameSite=Lax below is the second layer, not the only one.
const csrfHeader = "X-Requested-With"
const csrfHeaderValue = "cc-web"

// requireBrowserRequest enforces the CSRF header on anything that relies on
// the cookie. Bearer callers are unaffected: a PAT in an Authorization header
// is never attached by a browser on someone else's behalf.
func requireBrowserRequest(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get(csrfHeader) == csrfHeaderValue {
		return true
	}
	writeError(w, http.StatusForbidden, "forbidden",
		"this endpoint requires the "+csrfHeader+": "+csrfHeaderValue+" header")
	return false
}

func (d Deps) setSessionCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		// Secure unless the operator explicitly chose cleartext. Setting it
		// unconditionally would mean the cookie is silently dropped on a
		// plain-HTTP dev instance and sign-in would appear to succeed and
		// then not work, which is a worse failure than the one it prevents.
		Secure: d.SessionCookieSecure,
		// Lax, not Strict: Strict would drop the cookie when someone follows
		// a link to this app from anywhere else, so they would arrive signed
		// out every time. Lax still withholds it from cross-site POSTs.
		SameSite: http.SameSiteLaxMode,
	})
}

func (d Deps) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   d.SessionCookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ---------------------------------------------------------------------------
// What this installation offers
// ---------------------------------------------------------------------------

type authMethod struct {
	Kind  string `json:"kind"`
	ID    string `json:"id,omitempty"`
	Label string `json:"label,omitempty"`
}

// listAuthMethods tells the sign-in screen which methods exist, so it renders
// those and nothing else. The UI's standing rule is never to offer a control
// for a capability the backend lacks, and the sign-in screen was the one
// place breaking it — it explained a CLI command instead of signing anyone in.
func (d Deps) listAuthMethods(w http.ResponseWriter, _ *http.Request) {
	// Password and token are unconditional today. Directory providers append
	// here as they are configured, which is why this is a list and not three
	// booleans.
	methods := []authMethod{
		{Kind: "password"},
		{Kind: "token"},
	}
	writeJSON(w, http.StatusOK, map[string]any{"methods": methods})
}

// ---------------------------------------------------------------------------
// Password sign-in
// ---------------------------------------------------------------------------

type signInRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (d Deps) signInWithPassword(w http.ResponseWriter, r *http.Request) {
	if !requireBrowserRequest(w, r) {
		return
	}
	var req signInRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		return
	}

	// Per identifier, before any work is done: the per-credential limiter
	// cannot see this at all, because a failed guess belongs to no credential.
	if !d.Limiter.AllowSignIn(req.Email) {
		d.recordSignInFailure(r, "sign-in attempts for this identifier exceeded their rate limit", security.KindRateLimited)
		w.Header().Set("Retry-After", "30")
		writeError(w, http.StatusTooManyRequests, "rate_limited",
			"too many sign-in attempts; wait a moment and try again")
		return
	}

	// argon2id holds 64 MiB for the duration of one verification, so the
	// number running at once is bounded separately and much lower than
	// token authentication's gate.
	release, ok := d.Limiter.AcquirePasswordHash()
	if !ok {
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusServiceUnavailable, "overloaded",
			"too many sign-ins in progress; retry shortly")
		return
	}
	principal, err := identity.AuthenticatePassword(r.Context(), d.Pool, req.Email, req.Password)
	release()

	if err != nil {
		if !errors.Is(err, identity.ErrInvalidCredentials) {
			internalError(w, r, err, "could not complete sign-in")
			return
		}
		d.recordSignInFailure(r, "password sign-in failed", security.KindAuthenticationFailed)
		// One message for every cause — see identity.ErrInvalidCredentials.
		writeError(w, http.StatusUnauthorized, "unauthorized", "incorrect email or password")
		return
	}

	d.issueSessionResponse(w, r, principal.ID)
}

// recordSignInFailure notes a refused sign-in without naming the address that
// was tried. The security log is read by an administrator looking for an
// attack, and an address typed into a login form is as likely to be a
// colleague's typo as an attacker's guess — recording every one builds a list
// of email addresses that is more useful to a reader of the log than to its
// owner.
func (d Deps) recordSignInFailure(r *http.Request, reason string, kind security.Kind) {
	d.Security.Record(r.Context(), security.Event{
		Kind:      kind,
		Reason:    reason,
		RequestID: middleware.GetReqID(r.Context()),
	})
}

// issueSessionResponse mints a session, sets the cookie and describes who the
// caller now is. Shared by password sign-in and by first-run setup, which
// must leave the new administrator signed in rather than at a login form.
func (d Deps) issueSessionResponse(w http.ResponseWriter, r *http.Request, principalID uuid.UUID) {
	token, cred, err := identity.IssueSession(r.Context(), d.Pool, principalID)
	if err != nil {
		internalError(w, r, err, "could not start a session")
		return
	}

	expires := time.Now().Add(identity.SessionLifetime)
	if cred.ExpiresAt != nil {
		expires = *cred.ExpiresAt
	}
	d.setSessionCookie(w, token, expires)

	// The token is deliberately NOT in the body. It is in the cookie, which
	// the page cannot read, and putting a copy in the response would hand it
	// straight back to the JavaScript the cookie exists to keep it from.
	writeJSON(w, http.StatusOK, map[string]any{
		"expires_at": expires.UTC().Format(time.RFC3339),
	})
}

// signOut revokes the session the cookie names and clears it.
//
// Public, and forgiving: an expired or unrecognised cookie still gets cleared
// and still returns success. The alternative — 401 on sign-out — leaves the
// browser holding a cookie it was trying to get rid of.
func (d Deps) signOut(w http.ResponseWriter, r *http.Request) {
	if !requireBrowserRequest(w, r) {
		return
	}
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		// Authenticate rather than trusting the id in the token: the
		// credential id half of a PAT is public, so revoking by it alone
		// would let anyone end anyone else's session by guessing an id.
		if cred, err := identity.Authenticate(r.Context(), d.Pool, c.Value); err == nil {
			_ = identity.EndSession(r.Context(), d.Pool, cred.ID)
		}
	}
	d.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// First run
// ---------------------------------------------------------------------------

func (d Deps) getSetupStatus(w http.ResponseWriter, r *http.Request) {
	needed, err := d.setupNeeded(r)
	if err != nil {
		internalError(w, r, err, "could not read installation state")
		return
	}
	// One bit, about an installation that has no users to protect yet.
	writeJSON(w, http.StatusOK, map[string]any{"needs_setup": needed})
}

func (d Deps) setupNeeded(r *http.Request) (bool, error) {
	var hasUser bool
	err := db.WithoutPrincipal(r.Context(), d.Pool, func(tx pgx.Tx) error {
		var err error
		hasUser, err = identity.NewStore().HasAnyUser(r.Context(), tx)
		return err
	})
	if err != nil {
		return false, err
	}
	return !hasUser, nil
}

type setupRequest struct {
	Code        string `json:"code"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
}

// completeSetup creates the first user, its principal, its password and its
// installation-admin grant, then signs it in.
//
// Three independent refusals, because this endpoint creates an installation
// admin and nothing above it can authorize the call: the installation must
// have no users, the setup code must match, and the code is single-use. The
// first is the one that still holds if the process is restarted by someone
// who then reads the new code out of the log — by then there is a user, and
// this endpoint is closed for good.
func (d Deps) completeSetup(w http.ResponseWriter, r *http.Request) {
	if !requireBrowserRequest(w, r) {
		return
	}
	var req setupRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		return
	}

	needed, err := d.setupNeeded(r)
	if err != nil {
		internalError(w, r, err, "could not read installation state")
		return
	}
	if !needed {
		writeError(w, http.StatusConflict, "already_set_up",
			"this installation already has users; sign in instead")
		return
	}

	if strings.TrimSpace(req.Email) == "" || strings.TrimSpace(req.DisplayName) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "email and display_name are required")
		return
	}
	if err := identity.ValidatePassword(req.Password); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", passwordTooShortMessage())
		return
	}

	if !d.FirstRun.Consume(req.Code) {
		d.recordSignInFailure(r, "first-run setup presented an incorrect or already-used code", security.KindAuthenticationFailed)
		writeError(w, http.StatusUnauthorized, "unauthorized",
			"that setup code is not valid; the current one is printed in the server's log")
		return
	}

	hash, err := identity.HashPassword(req.Password)
	if err != nil {
		internalError(w, r, err, "could not store the password")
		return
	}

	// One transaction: a half-created first administrator — a user with no
	// principal, or an admin with no password — would leave the installation
	// in a state its own setup endpoint has just refused to run again.
	var principalID uuid.UUID
	store := identity.NewStore()
	admins := installation.NewStore()
	err = db.WithoutPrincipal(r.Context(), d.Pool, func(tx pgx.Tx) error {
		user, err := store.CreateUser(r.Context(), tx, strings.TrimSpace(req.Email), strings.TrimSpace(req.DisplayName))
		if err != nil {
			return err
		}
		principal, err := store.CreatePrincipalForUser(r.Context(), tx, user.ID)
		if err != nil {
			return err
		}
		principalID = principal.ID
		if err := store.SetPassword(r.Context(), tx, user.ID, hash); err != nil {
			return err
		}
		// grantedBy is nil: there is nobody above this grant, which is the
		// same bootstrap case `ccd bootstrap` records (docs/adr/0017).
		_, err = admins.Grant(r.Context(), tx, user.ID, nil)
		return err
	})
	if err != nil {
		if errors.Is(err, identity.ErrAlreadyExists) {
			writeError(w, http.StatusConflict, "conflict", "a user with this email already exists")
			return
		}
		internalError(w, r, err, "could not create the first administrator")
		return
	}

	d.issueSessionResponse(w, r, principalID)
}

// ---------------------------------------------------------------------------
// Signing in with a personal access token
// ---------------------------------------------------------------------------

type tokenSignInRequest struct {
	Token string `json:"token"`
}

// signInWithToken exchanges a PAT for a session.
//
// This is what lets the browser stop storing a credential at all. Before, the
// UI kept the pasted PAT in localStorage and attached it to every request —
// so an XSS anywhere in the SPA lifted a long-lived, often non-expiring token
// that the victim had no reason to suspect was gone. Now the PAT is presented
// once, exchanged, and forgotten; what remains is a 12-hour session in a
// cookie the page cannot read.
//
// It stays available alongside passwords because an operator who has just
// installed this may hold a PAT from `ccd token create` and no password yet.
func (d Deps) signInWithToken(w http.ResponseWriter, r *http.Request) {
	if !requireBrowserRequest(w, r) {
		return
	}
	var req tokenSignInRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		return
	}

	// Keyed on the token's credential id — the public half of a PAT — so
	// guessing attempts against one credential are bounded. A token too
	// malformed to yield an id is limited under its own empty key, which is
	// correct: that is noise, and noise gets one bucket.
	limitKey := ""
	if id, _, err := identity.ParseToken(req.Token); err == nil {
		limitKey = id.String()
	}
	if !d.Limiter.AllowSignIn(limitKey) {
		w.Header().Set("Retry-After", "30")
		writeError(w, http.StatusTooManyRequests, "rate_limited",
			"too many sign-in attempts; wait a moment and try again")
		return
	}

	release, ok := d.Limiter.AcquireAuth()
	if !ok {
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusServiceUnavailable, "overloaded",
			"too many authentication attempts in flight; retry shortly")
		return
	}
	cred, err := identity.Authenticate(r.Context(), d.Pool, req.Token)
	release()
	if err != nil {
		d.recordSignInFailure(r, "token sign-in presented a token that did not resolve to an active credential", security.KindAuthenticationFailed)
		writeError(w, http.StatusUnauthorized, "unauthorized", "that token is not valid")
		return
	}

	// A session is issued rather than the PAT being reused as one: the PAT
	// may never expire, and a browser session must.
	d.issueSessionResponse(w, r, cred.PrincipalID)
}

// ---------------------------------------------------------------------------
// Managing your own password and sessions
// ---------------------------------------------------------------------------

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// changeOwnPassword sets or replaces the caller's own password.
//
// The current password is required when one exists and not when it does not —
// which is how someone who signed in with a PAT sets their first password
// without an administrator. It is not a hole: the caller already holds a
// valid credential for this account, which is a stronger proof than knowing
// the old password.
func (d Deps) changeOwnPassword(w http.ResponseWriter, r *http.Request) {
	var req changePasswordRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		return
	}
	if err := identity.ValidatePassword(req.NewPassword); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", passwordTooShortMessage())
		return
	}

	principalID := principalIDFrom(r.Context())
	store := identity.NewStore()

	var (
		userID       uuid.UUID
		existingHash string
		hasPassword  bool
	)
	err := db.WithoutPrincipal(r.Context(), d.Pool, func(tx pgx.Tx) error {
		principal, err := store.GetPrincipal(r.Context(), tx, principalID)
		if err != nil {
			return err
		}
		if principal.UserID == nil {
			return identity.ErrNotFound
		}
		userID = *principal.UserID
		existingHash, err = store.GetPasswordHash(r.Context(), tx, userID)
		if errors.Is(err, identity.ErrNoLocalPassword) {
			return nil
		}
		hasPassword = err == nil
		return err
	})
	if err != nil && !errors.Is(err, identity.ErrNoLocalPassword) {
		if errors.Is(err, identity.ErrNotFound) {
			// A service principal has no password to change, and saying so
			// plainly beats a 500 — an agent is not a person with a login.
			writeError(w, http.StatusBadRequest, "invalid_request",
				"only a user account can have a password")
			return
		}
		internalError(w, r, err, "could not read the current password")
		return
	}

	if hasPassword {
		release, ok := d.Limiter.AcquirePasswordHash()
		if !ok {
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusServiceUnavailable, "overloaded", "too busy to verify a password; retry shortly")
			return
		}
		valid := identity.VerifyPassword(req.CurrentPassword, existingHash)
		release()
		if !valid {
			d.recordSignInFailure(r, "password change presented an incorrect current password", security.KindAuthenticationFailed)
			writeError(w, http.StatusForbidden, "forbidden", "the current password is not correct")
			return
		}
	}

	hash, err := identity.HashPassword(req.NewPassword)
	if err != nil {
		internalError(w, r, err, "could not store the password")
		return
	}
	err = db.WithoutPrincipal(r.Context(), d.Pool, func(tx pgx.Tx) error {
		return store.SetPassword(r.Context(), tx, userID, hash)
	})
	if err != nil {
		internalError(w, r, err, "could not store the password")
		return
	}

	// Deliberately NOT revoking the caller's other sessions. Changing a
	// password because it may be known to someone else and ending every
	// session are two different intentions, and the session list below is
	// where the second one belongs — doing it silently here would sign
	// someone out of their other machine for choosing a longer password.
	w.WriteHeader(http.StatusNoContent)
}

type sessionResponse struct {
	ID         string  `json:"id"`
	CreatedAt  string  `json:"created_at"`
	LastUsedAt *string `json:"last_used_at"`
	ExpiresAt  *string `json:"expires_at"`
	Current    bool    `json:"current"`
}

// listOwnSessions shows where this account is signed in. `current` marks the
// session making the request, so the screen can say which row is "this
// browser" rather than inviting someone to guess and sign themselves out.
func (d Deps) listOwnSessions(w http.ResponseWriter, r *http.Request) {
	principalID := principalIDFrom(r.Context())
	currentID := clientIDFrom(r.Context())

	var creds []identity.ClientCredential
	err := db.WithoutPrincipal(r.Context(), d.Pool, func(tx pgx.Tx) error {
		var err error
		creds, err = identity.NewStore().ListSessionsForPrincipal(r.Context(), tx, principalID)
		return err
	})
	if err != nil {
		internalError(w, r, err, "could not list sessions")
		return
	}

	out := make([]sessionResponse, 0, len(creds))
	for _, c := range creds {
		// Revoked and expired rows are history, not places you are signed
		// in; keeping them would make the list grow forever and say nothing.
		if !c.Active(time.Now()) {
			continue
		}
		out = append(out, sessionResponse{
			ID:         c.ID.String(),
			CreatedAt:  c.CreatedAt.UTC().Format(time.RFC3339),
			LastUsedAt: formatTimePtr(c.LastUsedAt),
			ExpiresAt:  formatTimePtr(c.ExpiresAt),
			Current:    currentID != nil && *currentID == c.ID,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func formatTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

// revokeOwnSession ends one of the caller's own sessions, including the
// current one — which is simply signing out, and is allowed for that reason.
func (d Deps) revokeOwnSession(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "session id must be a UUID")
		return
	}
	principalID := principalIDFrom(r.Context())

	// Ownership is in the WHERE clause, so someone else's session id is
	// indistinguishable from one that does not exist — the same shape
	// RevokeCredential uses for PATs.
	err = db.WithoutPrincipal(r.Context(), d.Pool, func(tx pgx.Tx) error {
		return identity.NewStore().RevokeCredential(r.Context(), tx, id, principalID)
	})
	if errors.Is(err, identity.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "no such session")
		return
	}
	if err != nil {
		internalError(w, r, err, "could not end that session")
		return
	}
	// If that was the current session, the cookie it came from is now dead;
	// clearing it saves the browser one guaranteed-401 round trip.
	if cur := clientIDFrom(r.Context()); cur != nil && *cur == id {
		d.clearSessionCookie(w)
	}
	w.WriteHeader(http.StatusNoContent)
}

// passwordTooShortMessage states the rule without the Go error's package
// prefix. A domain error reads "identity: password too short", which is
// correct in a log and wrong in a form — the person reading it did not ask
// which package refused them.
func passwordTooShortMessage() string {
	return fmt.Sprintf("the password must be at least %d characters", identity.MinPasswordLength)
}
