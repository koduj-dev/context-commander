package gates_test

import (
	"context"
	"strings"
	"testing"

	"github.com/koduj-dev/context-commander/internal/gates"
)

func evalSecretScanner(t *testing.T, payload string) gates.Result {
	t.Helper()
	g, ok := gates.Get("secret-scanner")
	if !ok {
		t.Fatal("secret-scanner gate not registered")
	}
	r, err := g.Evaluate(context.Background(), gates.HookContext{Hook: "artifact.before_write", Payload: []byte(payload)})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	return r
}

func TestSecretScannerTruePositives(t *testing.T) {
	cases := map[string]string{ //nolint:gosec // fixture strings shaped like credentials, not real ones — this is the secret-scanner's own test
		"github_token":      "token: ghp_" + strings.Repeat("a", 36),
		"aws_access_key":    "AWS_ACCESS_KEY_ID=AKIAABCDEFGHIJKLMNOP",
		"private_key_block": "-----BEGIN RSA PRIVATE KEY-----\nMIIB...\n-----END RSA PRIVATE KEY-----",
		"jwt":               "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U",
		"password_assign":   `DATABASE_PASSWORD="hunter2-super-secret"`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			r := evalSecretScanner(t, payload)
			if r.Verdict != gates.Deny {
				t.Fatalf("payload %q: got verdict %s, want DENY", payload, r.Verdict)
			}
			if strings.Contains(r.Guidance, payload) {
				t.Fatalf("guidance leaks the matched secret text: %q", r.Guidance)
			}
		})
	}
}

func TestSecretScannerFalsePositives(t *testing.T) {
	cases := []string{
		"# Architecture notes\n\nThis document describes the login flow.",
		"See the password reset flow in docs/auth.md for details.",
		"func main() { fmt.Println(\"hello\") }",
	}
	for _, payload := range cases {
		r := evalSecretScanner(t, payload)
		if r.Verdict != gates.Allow {
			t.Errorf("payload %q: got verdict %s, want ALLOW", payload, r.Verdict)
		}
	}
}
