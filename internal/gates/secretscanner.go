package gates

import (
	"context"
	"fmt"
	"regexp"
)

// secretPattern is one class of secret-like text the scanner looks for —
// docs/architecture-cookbook.md §15.1's list (GitHub tokens, cloud access
// keys, private keys, JWTs, generic API tokens, password assignments).
type secretPattern struct {
	name string
	re   *regexp.Regexp
}

var secretPatterns = []secretPattern{
	{"github_token", regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{36,}`)},
	{"aws_access_key", regexp.MustCompile(`AKIA[0-9A-Z]{16}`)},
	{"private_key_block", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
	{"jwt", regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`)},
	{"generic_secret_assignment", regexp.MustCompile(`(?i)(password|passwd|secret|api[_-]?key)\s*[:=]\s*['"]?[^\s'"]{6,}`)},
}

// secretScanner is the "secret-scanner" Gate named in cookbook §15.1 —
// registered under internal/gates so detection is a Gate concern, not
// hard-coded into internal/artifacts (the cookbook's explicit decision).
// It blocks outright (DENY) rather than warning: §15.1 lists
// "warn / redact / block according to policy" as the range of outcomes,
// and DENY is the safe default for a provider with no per-binding policy
// config yet — a project can still choose a softer policy by configuring a
// different gate_bindings.config in the future.
type secretScanner struct{}

func (secretScanner) Evaluate(_ context.Context, hc HookContext) (Result, error) {
	text := string(hc.Payload)
	for _, p := range secretPatterns {
		if loc := p.re.FindStringIndex(text); loc != nil {
			// Guidance names the pattern and byte offset only — never the
			// matched text itself, so the secret doesn't leak into
			// guidance/logs (docs/architecture-cookbook.md §15.1).
			return Result{
				Verdict: Deny,
				Guidance: fmt.Sprintf(
					"blocked: content matches a %s-like pattern at byte offset %d — remove the secret and reference it via internal/secrets instead of writing it into content",
					p.name, loc[0],
				),
			}, nil
		}
	}
	return Result{Verdict: Allow}, nil
}

func init() {
	Register("secret-scanner", secretScanner{})
}
