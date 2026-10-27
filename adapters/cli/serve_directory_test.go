package cli

import (
	"strings"
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
	// to start rather than offer a button that fails for everyone. One field
	// missing at a time — with all three missing, the first check to run
	// would satisfy the test alone and the other two would go unproven.
	complete := map[string]string{
		"oidc-client-id":    "cc",
		"oidc-redirect-url": "https://cc.example.test/v1/auth/oidc/callback",
	}
	cases := map[string]struct {
		drop   string
		secret string
		want   string
	}{
		"no client id": {drop: "oidc-client-id", secret: "s", want: "client id"},
		"no secret":    {secret: "", want: "client secret"},
		"no redirect":  {drop: "oidc-redirect-url", secret: "s", want: "redirect url"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv(oidcIssuerEnv, "https://id.example.test/realms/x")
			t.Setenv(oidcClientSecretEnv, tc.secret)
			cmd := newServeCmd()
			for flag, value := range complete {
				if flag == tc.drop {
					continue
				}
				if err := cmd.Flags().Set(flag, value); err != nil {
					t.Fatal(err)
				}
			}
			_, err := oidcFromFlags(cmd)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want an error naming the %s, got %v", tc.want, err)
			}
		})
	}

	// And with nothing missing, it starts.
	t.Setenv(oidcIssuerEnv, "https://id.example.test/realms/x")
	t.Setenv(oidcClientSecretEnv, "s")
	cmd := newServeCmd()
	for flag, value := range complete {
		if err := cmd.Flags().Set(flag, value); err != nil {
			t.Fatal(err)
		}
	}
	if p, err := oidcFromFlags(cmd); err != nil || p == nil {
		t.Fatalf("complete configuration: provider %v, error %v", p, err)
	}
}

func TestTwoDirectoriesCannotShareAName(t *testing.T) {
	ldapDir, err := directory.NewLDAP(directory.LDAPConfig{
		URL: "ldap://127.0.0.1:1389", UserBase: "ou=people,dc=example,dc=test",
	})
	if err != nil {
		t.Fatal(err)
	}
	oidcFor := func(name string) directory.RedirectAuthenticator {
		o, err := directory.NewOIDC(directory.OIDCConfig{
			Name: name, IssuerURL: "https://id.example.test/realms/x", ClientID: "cc",
			ClientSecret: "s", RedirectURL: "https://cc.example.test/v1/auth/oidc/callback",
		})
		if err != nil {
			t.Fatal(err)
		}
		return o
	}
	if err := distinctDirectoryNames(ldapDir, oidcFor("ldap")); err == nil {
		t.Fatal("LDAP and OIDC both named ldap were accepted")
	}
	if err := distinctDirectoryNames(ldapDir, oidcFor("oidc")); err != nil {
		t.Fatalf("distinct names refused: %v", err)
	}
	if err := distinctDirectoryNames(nil, oidcFor("ldap")); err != nil {
		t.Fatalf("OIDC alone refused: %v", err)
	}
}
