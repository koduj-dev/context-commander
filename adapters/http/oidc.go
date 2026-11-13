package http

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/directory"
	"github.com/koduj-dev/context-commander/internal/identity"
	"github.com/koduj-dev/context-commander/internal/members"
	"github.com/koduj-dev/context-commander/internal/security"
	"github.com/koduj-dev/context-commander/internal/signin"
)

// An OIDC sign-in attempt travels from the redirect out to the callback in a
// cookie: which provider, when it started, and the state, nonce and PKCE
// verifier that bind the answer to this browser and this attempt.
//
// A cookie rather than a server-side table because the attempt belongs to the
// browser, lives for minutes and has no business in a backup. HttpOnly, so the
// page cannot read the verifier.
//
// Login CSRF through a sibling subdomain is what the cookie's NAME stops, not
// its MAC. The attack: the attacker calls /start, signs in at the provider as
// themselves, keeps the code unspent, and plants their own attempt cookie —
// genuine, correctly signed — in the victim's browser from a subdomain, then
// sends the victim to the callback. Every check passes and the victim is
// working inside the attacker's account. The defence is the __Host- prefix:
// a browser accepts a __Host- cookie only from this exact host, over a secure
// connection, with Path=/ and no Domain, so no sibling and no parent domain
// can set one. That forces Path=/ (the cookie rides along with every request
// here; it is HttpOnly and a few hundred bytes) and requires Secure.
//
// Under --insecure-allow-cleartext a browser refuses __Host- cookies, so the
// attempt is a plain cc_oidc_ cookie scoped to /v1/auth/oidc, and this one
// defence is absent — the same trade the session cookie makes there. With the
// prefix in force, a plain-named cookie is never read: it is exactly what a
// sibling could have planted.
//
// Signed with an HMAC under a key derived from the root key for this purpose
// alone (crypto.PurposeOIDCAttemptCookie, docs/adr/0023), over the name and
// the payload. That stops an attempt being forged or edited — a chosen
// verifier, a provider swapped, a lifetime extended — and stops a signed
// attempt being renamed out of the __Host- namespace or into another
// attempt's slot. The MAC also covers the start time, so the lifetime is
// enforced here and not only by the browser's obedience to Max-Age.
//
// One cookie per attempt, named after its state, so a second /start — another
// tab, a double click, a cross-site link — does not overwrite the first and
// turn its callback into "expired". At most oidcMaxAttempts are kept; the
// oldest goes first.
const (
	oidcCookieBase = "cc_oidc_"
	// What a sibling subdomain cannot set; see above.
	oidcCookieHostPrefix = "__Host-"
	// Only for the cleartext name: a __Host- cookie must be Path=/.
	oidcCookieCleartextPath = "/v1/auth/oidc"
	// Long enough to type a password and pass a second factor at the
	// provider; short enough that an abandoned attempt is gone by lunch.
	oidcAttemptLifetime = 10 * time.Minute
	// Enough for a few tabs; small enough that the cookies a browser sends
	// to the callback stay a few hundred bytes each, and that a page which
	// triggers /start in a loop cannot grow the request header without end.
	oidcMaxAttempts = 3
)

type oidcAttempt struct {
	Provider string `json:"p"`
	// Started is a Unix time, under the MAC.
	Started int64 `json:"t"`
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

// oidcCookieName is the cookie an attempt with this state lives in. A hash,
// not the state itself: a cookie name has a narrower alphabet than a state a
// provider echoes back, and the name needs only to be distinct.
func (d Deps) oidcCookieName(state string) string {
	sum := sha256.Sum256([]byte(state))
	return d.oidcCookiePrefix() + hex.EncodeToString(sum[:8])
}

// oidcCookiePrefix is __Host-cc_oidc_ whenever cookies are Secure, and
// cc_oidc_ only under --insecure-allow-cleartext.
func (d Deps) oidcCookiePrefix() string {
	if d.SessionCookieSecure {
		return oidcCookieHostPrefix + oidcCookieBase
	}
	return oidcCookieBase
}

func (d Deps) oidcCookiePath() string {
	if d.SessionCookieSecure {
		return "/"
	}
	return oidcCookieCleartextPath
}

// oidcMAC covers the cookie's name as well as its value, so a validly signed
// attempt cannot be moved under another attempt's name.
func (d Deps) oidcMAC(name, payload string) []byte {
	m := hmac.New(sha256.New, d.OIDCCookieKey)
	m.Write([]byte(name))
	m.Write([]byte{0})
	m.Write([]byte(payload))
	return m.Sum(nil)
}

func (d Deps) encodeOIDCAttempt(name string, a oidcAttempt) (string, error) {
	raw, err := json.Marshal(a)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(raw)
	return payload + "." + base64.RawURLEncoding.EncodeToString(d.oidcMAC(name, payload)), nil
}

// errOIDCCookieForged is a cookie whose MAC does not verify — planted, or
// edited. Distinct from one that has merely expired, because only this one
// is worth a security event.
var errOIDCCookieForged = errors.New("oidc attempt cookie failed its signature check")

var errOIDCCookieExpired = errors.New("oidc attempt cookie is expired or incomplete")

func (d Deps) decodeOIDCAttempt(c *http.Cookie) (oidcAttempt, error) {
	payload, sig, ok := strings.Cut(c.Value, ".")
	if !ok || len(d.OIDCCookieKey) == 0 {
		return oidcAttempt{}, errOIDCCookieForged
	}
	mac, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(mac, d.oidcMAC(c.Name, payload)) {
		return oidcAttempt{}, errOIDCCookieForged
	}
	// Verified before parsed: nothing an attacker wrote reaches the decoder.
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return oidcAttempt{}, errOIDCCookieForged
	}
	var a oidcAttempt
	if err := json.Unmarshal(raw, &a); err != nil {
		return oidcAttempt{}, errOIDCCookieForged
	}
	age := time.Since(time.Unix(a.Started, 0))
	if age > oidcAttemptLifetime || age < -time.Minute ||
		a.Provider == "" || a.State == "" || a.Nonce == "" || a.Verifier == "" {
		return oidcAttempt{}, errOIDCCookieExpired
	}
	return a, nil
}

func (d Deps) setOIDCCookie(w http.ResponseWriter, name, value string) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure is conditional for the reason setSessionCookie gives
		Name:     name,
		Value:    value,
		Path:     d.oidcCookiePath(),
		MaxAge:   int(oidcAttemptLifetime / time.Second),
		HttpOnly: true,
		Secure:   d.SessionCookieSecure,
		// Lax, and it has to be: the callback is a top-level GET arriving
		// from the provider's site, which is exactly the navigation Lax
		// still sends a cookie on and Strict does not.
		SameSite: http.SameSiteLaxMode,
	})
}

func (d Deps) clearOIDCCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: same conditional Secure as above
		Name:     name,
		Value:    "",
		Path:     d.oidcCookiePath(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   d.SessionCookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

// oidcAttemptCookies are the attempt cookies this server reads: under the
// __Host- prefix when it is in force, and nothing a sibling could have set.
func (d Deps) oidcAttemptCookies(r *http.Request) []*http.Cookie {
	prefix := d.oidcCookiePrefix()
	var out []*http.Cookie
	for _, c := range r.Cookies() {
		if strings.HasPrefix(c.Name, prefix) {
			out = append(out, c)
		}
	}
	return out
}

// pruneOIDCAttempts clears what /start should not leave behind: cookies that
// do not verify or have expired, and the oldest live ones beyond
// oidcMaxAttempts-1, making room for the one about to be set.
func (d Deps) pruneOIDCAttempts(w http.ResponseWriter, r *http.Request) {
	type live struct {
		name    string
		started int64
	}
	var keep []live
	for _, c := range d.oidcAttemptCookies(r) {
		a, err := d.decodeOIDCAttempt(c)
		if err != nil {
			d.clearOIDCCookie(w, c.Name)
			continue
		}
		keep = append(keep, live{c.Name, a.Started})
	}
	sort.Slice(keep, func(i, j int) bool { return keep[i].started < keep[j].started })
	for len(keep) >= oidcMaxAttempts {
		d.clearOIDCCookie(w, keep[0].name)
		keep = keep[1:]
	}
}

// startOIDCSignIn begins a sign-in: it draws the attempt's secrets, keeps them
// in a signed cookie of their own, and sends the browser to the provider.
func (d Deps) startOIDCSignIn(w http.ResponseWriter, r *http.Request) {
	p := d.oidcProvider(chi.URLParam(r, "provider"))
	if p == nil {
		writeError(w, http.StatusNotFound, "not_found", "no such sign-in provider")
		return
	}
	if len(d.OIDCCookieKey) == 0 {
		internalError(w, r, errors.New("oidc is configured without a cookie key"), "could not start sign-in")
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
	attempt := oidcAttempt{Provider: p.Provider(), Started: time.Now().Unix(), AuthRequest: directory.NewAuthRequest()}
	target, err := p.AuthCodeURL(attempt.AuthRequest)
	release()
	if err != nil {
		slog.ErrorContext(r.Context(), "oidc sign-in could not start",
			"err", err, "provider", p.Provider(), "request_id", middleware.GetReqID(r.Context()))
		signInFailed(w, r, "oidc_unavailable")
		return
	}

	name := d.oidcCookieName(attempt.State)
	value, err := d.encodeOIDCAttempt(name, attempt)
	if err != nil {
		internalError(w, r, err, "could not start sign-in")
		return
	}
	d.pruneOIDCAttempts(w, r)
	d.setOIDCCookie(w, name, value)
	http.Redirect(w, r, target, http.StatusFound)
}

// readOIDCAttempt finds the attempt the callback's state names. Only that
// cookie is cleared: the browser's other attempts, in other tabs, stay alive.
//
// forged reports that a cookie was presented under that name and did not
// verify. mismatched reports that no attempt has this state while the browser
// holds others — the shape of a login CSRF, where an attacker's own code and
// state are pushed into a victim's browser mid-sign-in. Neither is said to the
// person; both are recorded.
func (d Deps) readOIDCAttempt(w http.ResponseWriter, r *http.Request, state string) (a oidcAttempt, ok, forged, mismatched bool) {
	if state != "" {
		name := d.oidcCookieName(state)
		if c, err := r.Cookie(name); err == nil {
			// Cleared whatever happens next, so a browser that comes back
			// twice starts over. That is only as strong as the browser's
			// obedience — a client can resend the cookie — and the
			// guarantee against replay is the provider redeeming each code
			// once (TestOIDC_RejectsAReusedCode).
			d.clearOIDCCookie(w, name)
			a, err := d.decodeOIDCAttempt(c)
			switch {
			case errors.Is(err, errOIDCCookieForged):
				return oidcAttempt{}, false, true, false
			case err != nil:
				return oidcAttempt{}, false, false, false
			}
			// The name is a hash of the state; this is the comparison that
			// does not rest on the hash.
			return a, a.StateMatches(state), false, !a.StateMatches(state)
		}
	}
	for _, c := range d.oidcAttemptCookies(r) {
		if _, err := d.decodeOIDCAttempt(c); err == nil {
			return oidcAttempt{}, false, false, true
		}
	}
	return oidcAttempt{}, false, false, false
}

// finishOIDCSignIn is where the provider sends the browser back. It checks
// the state against the cookie, redeems the code, provisions the person and
// starts a session — then redirects into the app.
func (d Deps) finishOIDCSignIn(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	attempt, ok, forged, mismatched := d.readOIDCAttempt(w, r, q.Get("state"))
	switch {
	case forged:
		d.recordSignInFailure(r, "oidc attempt cookie failed its signature check", security.KindAuthenticationFailed)
	case mismatched:
		// No bound of its own: the per-IP limit that would bound this and
		// failed password sign-ins alike waits on a trusted-proxy
		// configuration (internal/ratelimit). A global one would let anyone
		// silence the record for everyone, which a security log must not
		// allow (internal/security, "off-switch, not sampling").
		d.recordSignInFailure(r, "oidc callback state did not match the attempt this browser started", security.KindAuthenticationFailed)
	}
	if !ok {
		// No attempt in this browser for this state: it expired, it was
		// started elsewhere, or it was tampered with. Indistinguishable on
		// purpose, and all of them mean "start again".
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
	id, err := p.Exchange(r.Context(), directory.Callback{Code: q.Get("code"), Issuer: q.Get("iss")}, attempt.AuthRequest)
	release()
	if err != nil {
		d.oidcExchangeFailed(w, r, attempt.Provider, err)
		return
	}

	// The allowlist: the operator's domains and the administrator's rules
	// are one list, applied here because neither half may refuse what the
	// other admits (docs/adr/0024). After Exchange, so only a verified
	// address is ever compared.
	decision, err := d.admit(r, p.Admission(), id)
	if err != nil {
		slog.ErrorContext(r.Context(), "could not read the sign-in rules",
			"err", err, "request_id", middleware.GetReqID(r.Context()))
		signInFailed(w, r, "oidc_failed")
		return
	}
	if !decision.Admitted {
		d.oidcExchangeFailed(w, r, attempt.Provider, fmt.Errorf("%w: subject %s at provider %s",
			directory.ErrDomainNotAllowed, id.Subject, attempt.Provider))
		return
	}

	// Nothing about authorization happens here: a directory proves who
	// someone is, not what they may do (docs/adr/0019). The only grant is
	// the one an administrator's rule names for an account it creates.
	email := id.Email
	principal, err := identity.ProvisionDirectoryUserWith(r.Context(), d.Pool, identity.DirectoryIdentity{
		Provider:    id.Provider,
		Subject:     id.Subject,
		Email:       id.Email,
		DisplayName: id.DisplayName,
		Issuer:      id.Issuer,
	}, identity.ProvisionPolicy{
		CreateMissing: p.AllowsSignup() || decision.ByRule,
		AfterCreate: func(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
			_, err := signin.NewStore().GrantMemberships(ctx, tx, userID, email, decision.DomainTrusted)
			return err
		},
	})
	if errors.Is(err, identity.ErrNoAccount) {
		// Authenticated, and simply not a user here. Safe to say: only
		// reachable after the provider proved who this is, and what they
		// need is an administrator, not another attempt.
		slog.InfoContext(r.Context(), "oidc sign-in for someone with no account, and sign-up is off",
			"provider", attempt.Provider, "request_id", middleware.GetReqID(r.Context()))
		signInFailed(w, r, "oidc_no_account")
		return
	}
	if errors.Is(err, identity.ErrIssuerMismatch) {
		// The provider name now points at a different issuer than the one
		// this person's link was made under. Only an operator can resolve
		// it — by restoring the issuer, or giving the new one its own
		// --oidc-name — so it is said loudly in the log and plainly to the
		// person.
		slog.ErrorContext(r.Context(), "oidc identity is linked under a different issuer than the one configured; "+
			"restore the old --oidc-issuer or give the new issuer its own --oidc-name",
			"provider", attempt.Provider, "issuer", id.Issuer, "request_id", middleware.GetReqID(r.Context()))
		signInFailed(w, r, "oidc_failed")
		return
	}
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
		// Includes signing keys that could not be fetched: an outage, so
		// not recorded as a refused sign-in.
		log("oidc provider could not be reached")
		signInFailed(w, r, "oidc_unavailable")
	case errors.Is(err, directory.ErrNoEmail):
		log("oidc provider released no email")
		signInFailed(w, r, "oidc_no_email")
	case errors.Is(err, directory.ErrEmailNotVerified):
		log("oidc provider released an unverified email")
		signInFailed(w, r, "oidc_email_unverified")
	case errors.Is(err, directory.ErrDomainNotAllowed):
		log("oidc sign-in from a domain this installation does not accept")
		signInFailed(w, r, "oidc_domain_not_allowed")
	default:
		// Rejected: a bad code, a token that failed verification, a nonce
		// that did not match, a response naming another issuer. Recorded,
		// because a verification failure is the one outcome here that can be
		// an attack rather than a mishap.
		log("oidc sign-in was rejected")
		d.recordSignInFailure(r, "oidc sign-in was rejected by verification", security.KindAuthenticationFailed)
		signInFailed(w, r, "oidc_failed")
	}
}

// admit applies the sign-in allowlist to a verified identity.
func (d Deps) admit(r *http.Request, adm directory.Admission, id directory.Identity) (signin.Decision, error) {
	var rules []signin.Rule
	err := db.WithoutPrincipal(r.Context(), d.Pool, func(tx pgx.Tx) error {
		var err error
		rules, err = signin.NewStore().List(r.Context(), tx)
		return err
	})
	if err != nil {
		return signin.Decision{}, err
	}
	invited, err := members.HasPendingInvitation(r.Context(), d.Pool, id.Email)
	if err != nil {
		return signin.Decision{}, err
	}
	return signin.Decide(signin.Input{
		Email:               id.Email,
		HostedDomain:        id.HostedDomain,
		RequireHostedDomain: adm.RequireHostedDomain,
		FlagDomains:         adm.Domains,
		Rules:               rules,
		Invited:             invited,
	}), nil
}
