package cli

import (
	"testing"

	"github.com/koduj-dev/context-commander/internal/directory"
)

// A server started without a directory must hand the HTTP adapter a nil
// interface, not an interface holding a nil pointer.
//
// It did not, from LDAP's first commit until this test: ldapFromFlags
// returned a nil *directory.LDAP, Deps.Directory became non-nil, and every
// server with no LDAP configured answered GET /v1/auth/methods — and every
// mistyped password — with a panic. The adapter's own tests build Deps by
// hand and could not see it; only the flags path does this.
func TestNoDirectoryFlagsMeansNoDirectory(t *testing.T) {
	t.Setenv(ldapURLEnv, "")
	t.Setenv(oidcIssuerEnv, "")
	cmd := newServeCmd()

	// Assigned to the interface exactly as Deps.Directory receives it.
	// Comparing the function's result directly would pass against the bug:
	// a nil *LDAP compares equal to nil until it is wrapped.
	var ldap directory.Authenticator
	ldap, err := ldapFromFlags(cmd)
	if err != nil {
		t.Fatalf("ldapFromFlags: %v", err)
	}
	if ldap != nil {
		t.Fatalf("ldapFromFlags returned %#v with no --ldap-url; Deps.Directory would be non-nil", ldap)
	}

	oidc, err := oidcFromFlags(cmd)
	if err != nil {
		t.Fatalf("oidcFromFlags: %v", err)
	}
	if oidc != nil {
		t.Fatalf("oidcFromFlags returned %#v with no --oidc-issuer", oidc)
	}
}

func TestOIDCFlagsRefuseAnIncompleteConfiguration(t *testing.T) {
	// The issuer switches OIDC on; without the rest the server must refuse
	// to start rather than offer a button that fails for everyone.
	t.Setenv(oidcIssuerEnv, "https://id.example.test/realms/x")
	t.Setenv(oidcClientSecretEnv, "")
	if _, err := oidcFromFlags(newServeCmd()); err == nil {
		t.Fatal("want a configuration error for an issuer with no client id, secret or redirect")
	}
}
