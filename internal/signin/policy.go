package signin

import "strings"

// Input is everything one admission decision reads.
type Input struct {
	// Email is the address the provider verified.
	Email string
	// HostedDomain is Google's `hd` claim: the primary domain of the
	// Workspace the account belongs to, empty for a personal account.
	HostedDomain string
	// RequireHostedDomain is set for a Google issuer (docs/adr/0024). A
	// domain then counts only when the account belongs to a Workspace whose
	// domain is also listed.
	RequireHostedDomain bool
	// FlagDomains is --oidc-allowed-domains, already normalized.
	FlagDomains []string
	// Rules is every rule an administrator set.
	Rules []Rule
	// Invited is whether the address holds a live project invitation
	// (docs/adr/0030). An invitation names one address, like an address
	// rule, and is the inviter's consent to an account for it.
	Invited bool
}

// Decision is the outcome of one sign-in.
type Decision struct {
	// Admitted is whether the sign-in may proceed at all.
	Admitted bool
	// ByRule is whether an administrator's rule matched or the address was
	// invited to a project. Either is consent to an account for what it
	// names, so it permits creating one even when --oidc-allow-signup is off.
	ByRule bool
	// DomainTrusted is whether the address may stand for its domain when
	// organizations are granted. False for a Google account outside any
	// listed Workspace; such an account can only have been admitted by an
	// address rule, and only address rules then grant.
	DomainTrusted bool
}

// Decide applies the rules to one verified address.
//
// With no list anywhere — no flag domain and no rule — everyone the provider
// verifies is admitted, which is what the server did before rules existed;
// whether they get an account is then --oidc-allow-signup's decision alone.
// Once anything is listed, only what is listed gets in, and that is checked
// on every sign-in, so removing a rule takes effect for people already
// linked.
func Decide(in Input) Decision {
	email := strings.ToLower(in.Email)
	domain := domainOf(email)

	domains := make(map[string]bool, len(in.FlagDomains))
	for _, d := range in.FlagDomains {
		domains[d] = true
	}
	ruleDomain, ruleAddress := false, false
	for _, r := range in.Rules {
		switch r.Kind {
		case KindDomain:
			domains[r.Value] = true
			if r.Value == domain {
				ruleDomain = true
			}
		case KindAddress:
			if r.Value == email {
				ruleAddress = true
			}
		}
	}

	// Google keeps a personal account verified under a work address after
	// its owner leaves the Workspace. The address then proves nothing about
	// the company, and only the `hd` claim does.
	trusted := true
	if in.RequireHostedDomain {
		hd := strings.ToLower(in.HostedDomain)
		trusted = hd != "" && domains[hd]
	}
	domainMatch := trusted && domains[domain]

	open := len(domains) == 0 && !hasAddressRules(in.Rules)
	return Decision{
		Admitted:      open || ruleAddress || domainMatch || in.Invited,
		ByRule:        ruleAddress || (domainMatch && ruleDomain) || in.Invited,
		DomainTrusted: trusted,
	}
}

func hasAddressRules(rules []Rule) bool {
	for _, r := range rules {
		if r.Kind == KindAddress {
			return true
		}
	}
	return false
}
