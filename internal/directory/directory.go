package directory

import (
	"context"
	"errors"
)

var (
	// ErrNoSuchUser means the directory holds nobody matching the
	// identifier. Separate from ErrInvalidCredentials so the caller can
	// decide whether to try another backend; it must never reach a caller
	// over HTTP as a distinct answer, or the login form becomes a directory
	// lookup for anyone who asks.
	ErrNoSuchUser = errors.New("directory: no such user")

	// ErrInvalidCredentials means the directory knows the person and
	// rejected the password.
	ErrInvalidCredentials = errors.New("directory: invalid credentials")

	// ErrAmbiguous means the search matched more than one entry. It is a
	// configuration fault, not a failed login: a filter that can match two
	// people would otherwise let whichever one the server happened to return
	// first be authenticated as the other.
	ErrAmbiguous = errors.New("directory: identifier matched more than one entry")

	// ErrUnavailable means the directory could not be reached or refused the
	// service account. It must not be reported as a failed login — telling
	// someone their password is wrong when the directory is down sends them
	// to reset a password that was never the problem.
	ErrUnavailable = errors.New("directory: unavailable")

	// ErrNoEmail means the entry authenticated but carries no mail
	// attribute. See Identity.Email for why that is refused rather than
	// worked around.
	ErrNoEmail = errors.New("directory: entry has no email address")
)

// Identity is who the directory says someone is.
type Identity struct {
	// Provider names the configured directory this came from. It is half of
	// the key in user_identities, because a subject is only unique within
	// the directory that issued it.
	Provider string

	// Subject is the directory's own stable identifier — entryUUID for LDAP,
	// the `sub` claim for OIDC. Never the email address: an email is a label
	// a person can change and an administrator can reassign to someone else,
	// and keying on it is how one person silently inherits another's
	// projects.
	Subject string

	// Email, required. users.email is NOT NULL and the whole interface names
	// people by it, so an entry without one cannot be provisioned — and the
	// alternative, synthesising something like uid@directory.local, would put
	// an address into the product that belongs to nobody and that nothing can
	// ever send mail to. Refusing with a message an administrator can act on
	// is the honest failure.
	Email string

	// DisplayName, best-effort. Falls back to the identifier when the
	// directory offers nothing better, because a person with no name on
	// screen is worse than a person whose name is their login.
	DisplayName string
}

// Authenticator is what a password-checking directory backend implements.
// OIDC is a redirect flow rather than a password check and implements
// RedirectAuthenticator instead, but everything after the Identity is
// returned is shared.
type Authenticator interface {
	// Provider returns the configured name, for user_identities and for the
	// sign-in screen's method list.
	Provider() string

	// Authenticate verifies an identifier and password.
	Authenticate(ctx context.Context, identifier, password string) (Identity, error)
}

// RedirectAuthenticator is a provider the browser is sent to and returns
// from — OpenID Connect. The caller owns the AuthRequest between the two
// halves (a cookie, for the HTTP adapter) and checks its state; the provider
// verifies everything it was handed back.
type RedirectAuthenticator interface {
	// Provider returns the configured name, for user_identities and the
	// sign-in URL.
	Provider() string

	// Label is what the sign-in button says.
	Label() string

	// AllowsSignup reports whether a first sign-in may create a local
	// account, rather than only reach one that exists (docs/adr/0020).
	AllowsSignup() bool

	// AuthCodeURL is where to send the browser for this attempt.
	AuthCodeURL(req AuthRequest) (string, error)

	// Exchange redeems the code the provider returned and reports who
	// signed in.
	Exchange(ctx context.Context, code string, req AuthRequest) (Identity, error)
}

var _ RedirectAuthenticator = (*OIDC)(nil)
