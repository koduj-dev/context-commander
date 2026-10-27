package http

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/koduj-dev/context-commander/internal/directory"
	"github.com/koduj-dev/context-commander/internal/identity"
	"github.com/koduj-dev/context-commander/internal/security"
)

// oidcCookie carries one sign-in attempt from the redirect out to the
// callback: which provider, and the state, nonce and PKCE verifier that bind
// the answer to this browser and this attempt.
//
// A cookie rather than a server-side table because the attempt belongs to the
// browser, lives for minutes and must not outlive a restart any more than it
// must survive in a backup. HttpOnly, so the page cannot read the verifier;
// scoped to /v1/auth/oidc, so it rides along with nothing else.
//
// It is not signed. Everything in it is a random secret the callback compares
// against what the provider returns, so a forged cookie needs a matching state
// from a real sign-in — which means an attacker able to plant cookies on this
// origin could sign a victim's browser into the attacker's own account (login
// CSRF). Planting a cookie already requires control of this site or a sibling
// subdomain, and that is the deployment's boundary, not this flow's.
const (
	oidcCookie     = "cc_oidc"
	oidcCookiePath = "/v1/auth/oidc"
	// Long enough to type a password and pass a second factor at the
	// provider; short enough that an abandoned attempt is gone by lunch.
	oidcAttemptLifetime = 10 * time.Minute
)

type oidcAttempt struct {
	Provider string `json:"p"`
	directory.AuthRequest
}

func (d Deps) oidcProvider(name string) directory.RedirectAuthenticator {
	for _, p := range d.OIDC {
		if p.Provider() == name {
			return p
		}
	}
	return nil
}

// signInFailed sends the browser back to the sign-in screen with a reason it
// can show. A redirect, not a JSON error: both halves of this flow are
// top-level navigations, and a JSON body would leave the person staring at
// raw text on the provider's way back.
//
// The code is one of a fixed set the UI translates; nothing from the provider
// or the error is ever placed in the URL.
func signInFailed(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, "/?sign_in_error="+url.QueryEscape(code), http.StatusFound)
}

// startOIDCSignIn begins a sign-in: it draws the attempt's secrets, keeps them
// in the cc_oidc cookie, and sends the browser to the provider.
func (d Deps) startOIDCSignIn(w http.ResponseWriter, r *http.Request) {
	p := d.oidcProvider(chi.URLParam(r, "provider"))
	if p == nil {
		writeError(w, http.StatusNotFound, "not_found", "no such sign-in provider")
		return
	}

	// Anonymous, and its first call may be discovery — an outbound request.
	// Bounded by the same gate as the callback and token authentication, so
	// a flood cannot pile up goroutines waiting on a slow provider.
	release, ok := d.Limiter.AcquireAuth()
	if !ok {
		signInFailed(w, r, "oidc_unavailable")
		return
	}
	attempt := oidcAttempt{Provider: p.Provider(), AuthRequest: directory.NewAuthRequest()}
	target, err := p.AuthCodeURL(attempt.AuthRequest)
	release()
	if err != nil {
		slog.ErrorContext(r.Context(), "oidc sign-in could not start",
			"err", err, "provider", p.Provider(), "request_id", middleware.GetReqID(r.Context()))
		signInFailed(w, r, "oidc_unavailable")
		return
	}

	raw, err := json.Marshal(attempt)
	if err != nil {
		internalError(w, r, err, "could not start sign-in")
		return
	}
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure is conditional for the reason setSessionCookie gives
		Name:     oidcCookie,
		Value:    base64.RawURLEncoding.EncodeToString(raw),
		Path:     oidcCookiePath,
		MaxAge:   int(oidcAttemptLifetime / time.Second),
		HttpOnly: true,
		Secure:   d.SessionCookieSecure,
		// Lax, and it has to be: the callback is a top-level GET arriving
		// from the provider's site, which is exactly the navigation Lax
		// still sends a cookie on and Strict does not.
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, target, http.StatusFound)
}

func (d Deps) clearOIDCCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: same conditional Secure as above
		Name:     oidcCookie,
		Value:    "",
		Path:     oidcCookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   d.SessionCookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func readOIDCAttempt(r *http.Request) (oidcAttempt, bool) {
	c, err := r.Cookie(oidcCookie)
	if err != nil || c.Value == "" {
		return oidcAttempt{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		return oidcAttempt{}, false
	}
	var a oidcAttempt
	if err := json.Unmarshal(raw, &a); err != nil {
		return oidcAttempt{}, false
	}
	return a, a.Provider != "" && a.State != "" && a.Nonce != "" && a.Verifier != ""
}

// finishOIDCSignIn is where the provider sends the browser back. It checks
// the state against the cookie, redeems the code, provisions the person and
// starts a session — then redirects into the app.
func (d Deps) finishOIDCSignIn(w http.ResponseWriter, r *http.Request) {
	attempt, ok := readOIDCAttempt(r)
	// Cleared whatever happens next, so a browser that comes back twice
	// starts over. That is only as strong as the browser's obedience — a
	// client can resend the cookie — and the guarantee against replay is the
	// provider redeeming each code once (TestOIDC_RejectsAReusedCode).
	d.clearOIDCCookie(w)
	if !ok {
		// No attempt in this browser: it expired, or this callback was not
		// started here. Indistinguishable on purpose, and both mean "start
		// again".
		signInFailed(w, r, "oidc_expired")
		return
	}

	q := r.URL.Query()
	if !attempt.StateMatches(q.Get("state")) {
		d.recordSignInFailure(r, "oidc callback state did not match the attempt this browser started", security.KindAuthenticationFailed)
		signInFailed(w, r, "oidc_expired")
		return
	}
	if e := q.Get("error"); e != "" {
		// The provider declined before issuing a code — most often the person
		// pressed cancel. Its error_description is not repeated anywhere: it
		// is the provider's text, in the provider's language, and may name
		// things about the account.
		if e == "access_denied" {
			signInFailed(w, r, "oidc_cancelled")
		} else {
			// Provider-controlled and unbounded; slog quotes it, and this
			// keeps one request from writing an arbitrarily long log line.
			if len(e) > 64 {
				e = e[:64]
			}
			slog.WarnContext(r.Context(), "oidc provider returned an error", "error", e,
				"provider", attempt.Provider, "request_id", middleware.GetReqID(r.Context()))
			signInFailed(w, r, "oidc_failed")
		}
		return
	}

	p := d.oidcProvider(attempt.Provider)
	if p == nil {
		// Configured when the attempt started, gone after a restart.
		signInFailed(w, r, "oidc_expired")
		return
	}

	// Redeeming the code is an outbound request this endpoint makes on an
	// anonymous caller's behalf, so it shares the gate that bounds token
	// authentication in flight.
	release, ok := d.Limiter.AcquireAuth()
	if !ok {
		signInFailed(w, r, "oidc_unavailable")
		return
	}
	id, err := p.Exchange(r.Context(), q.Get("code"), attempt.AuthRequest)
	release()
	if err != nil {
		d.oidcExchangeFailed(w, r, attempt.Provider, err)
		return
	}

	// Nothing about authorization happens here: a directory proves who
	// someone is, not what they may do (docs/adr/0019).
	principal, err := identity.ProvisionDirectoryUser(r.Context(), d.Pool, identity.DirectoryIdentity{
		Provider:    id.Provider,
		Subject:     id.Subject,
		Email:       id.Email,
		DisplayName: id.DisplayName,
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "could not provision an oidc user",
			"err", err, "provider", attempt.Provider, "request_id", middleware.GetReqID(r.Context()))
		signInFailed(w, r, "oidc_failed")
		return
	}
	if _, err := d.startSession(w, r, principal.ID); err != nil {
		slog.ErrorContext(r.Context(), "could not start a session after oidc sign-in",
			"err", err, "request_id", middleware.GetReqID(r.Context()))
		signInFailed(w, r, "oidc_failed")
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

// oidcExchangeFailed maps a failed exchange to what the person is told.
//
// The distinctions are the ones that change what they should do next, and the
// same ones LDAP makes: an outage is not their fault, and a missing or
// unverified address is something only an administrator of the provider can
// fix. None of them can leak anything — each is only reachable after the
// provider itself has authenticated the person.
func (d Deps) oidcExchangeFailed(w http.ResponseWriter, r *http.Request, provider string, err error) {
	log := func(msg string) {
		slog.WarnContext(r.Context(), msg, "err", err, "provider", provider,
			"request_id", middleware.GetReqID(r.Context()))
	}
	switch {
	case errors.Is(err, directory.ErrUnavailable):
		log("oidc provider could not be reached")
		signInFailed(w, r, "oidc_unavailable")
	case errors.Is(err, directory.ErrNoEmail):
		log("oidc provider released no email")
		signInFailed(w, r, "oidc_no_email")
	case errors.Is(err, directory.ErrEmailNotVerified):
		log("oidc provider released an unverified email")
		signInFailed(w, r, "oidc_email_unverified")
	default:
		// Rejected: a bad code, a token that failed verification, a nonce
		// that did not match. Recorded, because a verification failure is the
		// one outcome here that can be an attack rather than a mishap.
		log("oidc sign-in was rejected")
		d.recordSignInFailure(r, "oidc sign-in was rejected by verification", security.KindAuthenticationFailed)
		signInFailed(w, r, "oidc_failed")
	}
}
