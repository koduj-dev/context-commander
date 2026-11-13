// Package signin holds the rules an installation administrator sets for who
// may sign in through OpenID Connect, and what a new account receives
// (docs/adr/0024).
//
// A rule names either a domain or one address, and the organizations a
// newly created account matching it joins. Rules live in the database so an
// administrator changes them in the interface without restarting the server;
// --oidc-allowed-domains keeps working beside them.
//
// What this package decides is admission and the grant on creation. Who
// someone is stays the provider's job (internal/directory), and what an
// existing account may do stays with project and organization membership.
package signin

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Kind is what a rule matches.
type Kind string

const (
	// KindDomain admits every verified address in one domain. Exact match on
	// the part after @: "example.com" does not admit "mail.example.com",
	// for the same reason --oidc-allowed-domains does not.
	KindDomain Kind = "domain"
	// KindAddress admits one address. The way to let in a contractor whose
	// address is in nobody's domain.
	KindAddress Kind = "address"
)

// Organization is an organization a rule grants, with its name for display.
type Organization struct {
	ID   uuid.UUID
	Name string
}

// Rule is one entry of the allowlist.
type Rule struct {
	ID            uuid.UUID
	Kind          Kind
	Value         string
	Organizations []Organization
	CreatedBy     *uuid.UUID
	CreatedAt     time.Time
}

// ErrInvalidValue is a rule value that is neither a domain nor an address.
var ErrInvalidValue = errors.New("signin: a rule is a domain like example.com or an address like someone@example.com")

// maxRuleOrganizations bounds the grant of one rule. A rule naming a
// hundred organizations is a mistake, and every sign-in it matches would
// pay for it.
const maxRuleOrganizations = 50

// ErrTooManyOrganizations is a rule naming more organizations than allowed.
var ErrTooManyOrganizations = fmt.Errorf("signin: a rule names at most %d organizations", maxRuleOrganizations)

// Normalize reads what an administrator typed. Something with an @ is an
// address, anything else a domain; both are lowercased, because providers
// and people spell addresses in whatever case they like and a rule that
// silently never matches is worse than one that is refused.
func Normalize(raw string) (Kind, string, error) {
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == "" || len(v) > 320 {
		return "", "", ErrInvalidValue
	}
	if at := strings.IndexByte(v, '@'); at >= 0 {
		local, domain := v[:at], v[at+1:]
		if local == "" || len(local) > 64 || strings.ContainsAny(local, " \t\r\n<>()[],;:\"") || !validDomain(domain) {
			return "", "", ErrInvalidValue
		}
		return KindAddress, v, nil
	}
	if !validDomain(v) {
		return "", "", ErrInvalidValue
	}
	return KindDomain, v, nil
}

// validDomain accepts a hostname with at least two labels. No wildcard and
// no leading dot: "*.example.com" or ".example.com" reads as "and every
// subdomain", which the matching does not do, and a rule must mean what it
// looks like.
func validDomain(d string) bool {
	if len(d) < 3 || len(d) > 253 || !strings.Contains(d, ".") {
		return false
	}
	for _, label := range strings.Split(d, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
	}
	return true
}

// domainOf is the part of an address after its last @, lowercased.
func domainOf(email string) string {
	at := strings.LastIndexByte(email, '@')
	if at < 0 {
		return ""
	}
	return strings.ToLower(email[at+1:])
}
