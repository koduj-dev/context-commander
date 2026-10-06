package directory

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"
)

// LDAPConfig describes one directory. Every field an operator must think
// about is here rather than defaulted silently, with two exceptions noted
// below where a default is the only safe choice.
type LDAPConfig struct {
	// Name is the provider key stored in user_identities and shown on the
	// sign-in screen. "ldap" unless an installation has more than one.
	Name string

	// URL is ldap://host:389 or ldaps://host:636.
	URL string

	// StartTLS upgrades a plain ldap:// connection. Ignored for ldaps://,
	// which is already encrypted.
	StartTLS bool

	// CACertFile is a PEM bundle to verify the directory's certificate
	// against, for the usual case of an internal CA. Empty uses the system
	// pool.
	//
	// There is deliberately no "skip verification" option. An unverified TLS
	// connection to a directory is a connection to whoever answered, and this
	// is the one conversation in the product where a man in the middle learns
	// everybody's password.
	CACertFile string

	// BindDN and BindPassword are the service account used to SEARCH for a
	// person. Empty BindDN means an anonymous search, which some directories
	// allow.
	//
	// It is deliberately not the account anyone is authenticated as: the
	// password check is a second bind, as the entry that was found.
	BindDN       string
	BindPassword string

	// UserBase is the subtree to search, e.g. ou=people,dc=example,dc=test.
	UserBase string

	// UserFilter selects the entry, with %s where the identifier goes. The
	// identifier is escaped before substitution (ldap.EscapeFilter), so a
	// value containing ) or * cannot reshape the filter.
	//
	// Default: (&(objectClass=inetOrgPerson)(|(uid=%s)(mail=%s))) — someone
	// may type either their login or their address, and a login form that
	// insists on knowing which is a login form that argues with people.
	UserFilter string

	// Attribute names, because directories disagree: Active Directory uses
	// sAMAccountName and objectGUID where OpenLDAP uses uid and entryUUID.
	MailAttr        string
	DisplayNameAttr string
	UniqueIDAttr    string

	// Timeout bounds the whole exchange: dial, bind, search, bind again. A
	// directory that hangs must fail a sign-in, not hold the request open.
	Timeout time.Duration
}

// Defaults for the fields where leaving it to the operator buys nothing.
const (
	defaultUserFilter = "(&(objectClass=inetOrgPerson)(|(uid=%s)(mail=%s)))"
	defaultMailAttr   = "mail"
	defaultNameAttr   = "displayName"
	defaultUniqueAttr = "entryUUID"
	defaultTimeout    = 10 * time.Second
	defaultName       = "ldap"
)

// LDAP authenticates against one LDAP or Active Directory server.
type LDAP struct {
	cfg LDAPConfig
	tls *tls.Config
}

// NewLDAP validates the configuration and prepares the TLS settings.
//
// Validation happens at startup rather than on the first sign-in, so a typo in
// a flag is a server that refuses to start rather than a login screen that
// fails for everyone at nine in the morning.
func NewLDAP(cfg LDAPConfig) (*LDAP, error) {
	if cfg.URL == "" {
		return nil, errors.New("directory: ldap url is required")
	}
	u, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("directory: ldap url: %w", err)
	}
	if u.Scheme != "ldap" && u.Scheme != "ldaps" {
		return nil, fmt.Errorf("directory: ldap url scheme must be ldap or ldaps, got %q", u.Scheme)
	}
	if cfg.UserBase == "" {
		return nil, errors.New("directory: ldap user base is required")
	}
	if cfg.BindDN == "" && cfg.BindPassword != "" {
		return nil, errors.New("directory: ldap bind password given without a bind dn")
	}

	if cfg.Name == "" {
		cfg.Name = defaultName
	}
	if cfg.UserFilter == "" {
		cfg.UserFilter = defaultUserFilter
	}
	if cfg.MailAttr == "" {
		cfg.MailAttr = defaultMailAttr
	}
	if cfg.DisplayNameAttr == "" {
		cfg.DisplayNameAttr = defaultNameAttr
	}
	if cfg.UniqueIDAttr == "" {
		cfg.UniqueIDAttr = defaultUniqueAttr
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	if strings.Count(cfg.UserFilter, "%s") == 0 {
		return nil, errors.New("directory: ldap user filter must contain %s for the identifier")
	}

	tlsCfg := &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12}
	if cfg.CACertFile != "" {
		pem, err := os.ReadFile(cfg.CACertFile)
		if err != nil {
			return nil, fmt.Errorf("directory: read ldap ca certificate: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("directory: %s contains no PEM certificate", cfg.CACertFile)
		}
		tlsCfg.RootCAs = pool
	}

	return &LDAP{cfg: cfg, tls: tlsCfg}, nil
}

func (l *LDAP) Provider() string { return l.cfg.Name }

// Authenticate finds the entry matching identifier, then binds as that entry
// with the presented password.
//
// Two binds, not one. Searching as the person would require already knowing
// their DN, and guessing it by pattern ("uid=%s,ou=people,...") breaks the
// moment a directory nests people in more than one place — which every real
// directory eventually does.
func (l *LDAP) Authenticate(ctx context.Context, identifier, password string) (Identity, error) {
	// Before anything reaches the network. An LDAP simple bind with an empty
	// password is not a failed authentication: it is an *unauthenticated
	// bind*, which servers may accept — the classic way an LDAP integration
	// admits anyone who types a known username and leaves the password blank.
	//
	// go-ldap refuses an empty simple bind itself (result code 206), so this
	// is not the only thing standing in the way. It is here for two reasons
	// that survive that: the caller must see a credential failure rather than
	// a transport error, and a security property should not depend on a
	// library keeping a behaviour it never promised.
	if password == "" {
		return Identity{}, ErrInvalidCredentials
	}
	if strings.TrimSpace(identifier) == "" {
		return Identity{}, ErrNoSuchUser
	}

	entry, err := l.find(ctx, identifier)
	if err != nil {
		return Identity{}, err
	}

	// A second, separate connection for the password check, so the service
	// account's connection is never left bound as the person — anything
	// reusing it would silently act as them.
	conn, err := l.dial(ctx)
	if err != nil {
		return Identity{}, err
	}
	defer func() { _ = conn.Close() }()

	if err := conn.Bind(entry.DN, password); err != nil {
		if ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials) {
			return Identity{}, ErrInvalidCredentials
		}
		// Anything else — the account is locked, the password has expired,
		// the server objected — is not "wrong password". Reporting it as one
		// sends someone to change a password that is already correct.
		return Identity{}, fmt.Errorf("%w: bind as %s: %w", ErrUnavailable, entry.DN, err)
	}

	subject := entry.GetAttributeValue(l.cfg.UniqueIDAttr)
	if subject == "" {
		// Without a stable subject there is nothing to key user_identities
		// on, and falling back to the DN would break the first time anyone
		// is moved between organizational units — which is what happens when
		// someone changes department.
		return Identity{}, fmt.Errorf("directory: entry %s has no %s attribute to identify it by",
			entry.DN, l.cfg.UniqueIDAttr)
	}
	email := entry.GetAttributeValue(l.cfg.MailAttr)
	if email == "" {
		return Identity{}, fmt.Errorf("%w: %s", ErrNoEmail, entry.DN)
	}

	name := entry.GetAttributeValue(l.cfg.DisplayNameAttr)
	if name == "" {
		name = entry.GetAttributeValue("cn")
	}
	if name == "" {
		name = identifier
	}

	return Identity{
		Provider:    l.cfg.Name,
		Subject:     subject,
		Email:       email,
		DisplayName: name,
	}, nil
}

// find searches for the one entry matching identifier, as the service account.
func (l *LDAP) find(ctx context.Context, identifier string) (*ldap.Entry, error) {
	conn, err := l.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()

	if l.cfg.BindDN != "" {
		if err := conn.Bind(l.cfg.BindDN, l.cfg.BindPassword); err != nil {
			// The service account's own credentials are a configuration
			// problem, never the caller's.
			return nil, fmt.Errorf("%w: bind as %s: %w", ErrUnavailable, l.cfg.BindDN, err)
		}
	}

	// Escaped before substitution: an identifier containing `)` or `*` would
	// otherwise reshape the filter, and `*` alone would match every entry.
	//
	// Measured without the escaping, the wildcard comes back as ErrAmbiguous
	// rather than as a bypass, because "exactly one entry or nothing" below
	// refuses a multiple match. Two independent reasons it cannot work, which
	// is the point: this one stops the filter being rewritten, that one stops
	// a rewritten filter being useful.
	safe := ldap.EscapeFilter(identifier)
	filter := strings.ReplaceAll(l.cfg.UserFilter, "%s", safe)

	res, err := conn.Search(&ldap.SearchRequest{
		BaseDN:     l.cfg.UserBase,
		Scope:      ldap.ScopeWholeSubtree,
		Filter:     filter,
		Attributes: []string{"dn", l.cfg.UniqueIDAttr, l.cfg.MailAttr, l.cfg.DisplayNameAttr, "cn"},
		// Two, not one: asking for two is how "exactly one" can be told apart
		// from "more than one". A SizeLimit of 1 would make an ambiguous
		// filter look unambiguous.
		SizeLimit: 2,
		TimeLimit: int(l.cfg.Timeout.Seconds()),
	})
	if err != nil {
		// A size-limit result means the filter matched more than we asked
		// for, which is the ambiguity below rather than a transport failure.
		if ldap.IsErrorWithCode(err, ldap.LDAPResultSizeLimitExceeded) {
			return nil, ErrAmbiguous
		}
		return nil, fmt.Errorf("%w: search %s: %w", ErrUnavailable, l.cfg.UserBase, err)
	}

	switch len(res.Entries) {
	case 0:
		return nil, ErrNoSuchUser
	case 1:
		return res.Entries[0], nil
	default:
		return nil, ErrAmbiguous
	}
}

func (l *LDAP) dial(ctx context.Context) (*ldap.Conn, error) {
	conn, err := ldap.DialURL(l.cfg.URL, ldap.DialWithTLSConfig(l.tls),
		ldap.DialWithDialer(&net.Dialer{Timeout: l.cfg.Timeout}))
	if err != nil {
		return nil, fmt.Errorf("%w: dial %s: %w", ErrUnavailable, l.cfg.URL, err)
	}
	conn.SetTimeout(l.cfg.Timeout)

	if l.cfg.StartTLS && strings.HasPrefix(l.cfg.URL, "ldap://") {
		if err := conn.StartTLS(l.tls); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("%w: starttls: %w", ErrUnavailable, err)
		}
	}

	// Respect a cancelled request rather than finishing an exchange nobody is
	// waiting for.
	if err := ctx.Err(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}
