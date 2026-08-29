// Package gates implements the lifecycle-hook policy subsystem
// (ALLOW / DENY / ALLOW_WITH_GUIDANCE / ALLOW_WITH_REDACTION) — see
// docs/architecture-cookbook.md §13.2/§13.3.
//
// A Gate is an in-process Go implementation registered under a name
// (Register); which gate runs on which hook for which scope is
// data-driven, stored in gate_bindings (migrations/000008) and resolved by
// Engine — mirrors internal/skills' SkillVersion/SkillBinding split
// (immutable code vs. mutable, scoped configuration).
package gates

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/google/uuid"
)

// Verdict is a gate's decision for one hook invocation.
// ALLOW_WITH_REDACTION exists alongside the three verdicts named in
// cookbook §13.2 because §15.1 explicitly describes "warn / redact / block"
// as the secret-scanner's possible outcomes — a fourth verdict reconciles
// that with the three-verdict enum in §13.2 rather than overloading
// ALLOW_WITH_GUIDANCE to also carry a content transformation.
type Verdict string

const (
	Allow              Verdict = "ALLOW"
	Deny               Verdict = "DENY"
	AllowWithGuidance  Verdict = "ALLOW_WITH_GUIDANCE"
	AllowWithRedaction Verdict = "ALLOW_WITH_REDACTION"
)

// verdictPriority orders verdicts for Engine.Evaluate's aggregation across
// multiple bound gates on the same hook: the most restrictive verdict any
// one gate returns wins.
var verdictPriority = map[Verdict]int{
	Allow:              0,
	AllowWithGuidance:  1,
	AllowWithRedaction: 2,
	Deny:               3,
}

// HookContext is what a Gate implementation receives — enough to evaluate
// the lifecycle hook named by cookbook §13.2 (e.g. "artifact.before_write")
// without the gate needing to know anything about the calling domain's own
// types.
type HookContext struct {
	Hook      string
	ProjectID uuid.UUID
	Payload   []byte
	Config    json.RawMessage
}

// Result is what a Gate returns. Guidance must never contain the sensitive
// material a gate detected (docs/architecture-cookbook.md §15.1 — a
// secret-scanner's guidance describing *what kind* of thing it found must
// not itself leak the secret into logs/guidance text). Redacted is only
// meaningful when Verdict is AllowWithRedaction.
type Result struct {
	Verdict  Verdict
	Guidance string
	Redacted []byte
}

// Gate is one in-process policy provider, registered under a name that
// gate_bindings.gate_name references.
type Gate interface {
	Evaluate(ctx context.Context, hc HookContext) (Result, error)
}

var (
	registryMu sync.RWMutex
	registry   = map[string]Gate{}
)

// Register makes a Gate implementation available under name for
// gate_bindings to reference. Called from providers' init() — see
// secretscanner.go.
func Register(name string, g Gate) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[name] = g
}

// Get returns the Gate registered under name, if any.
func Get(name string) (Gate, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	g, ok := registry[name]
	return g, ok
}

// combine aggregates two Results by verdict priority — DENY beats
// ALLOW_WITH_REDACTION beats ALLOW_WITH_GUIDANCE beats ALLOW. Guidance
// texts from every gate that produced one are concatenated so no gate's
// warning is silently dropped by a later, less-restrictive verdict.
func combine(a, b Result) Result {
	guidance := a.Guidance
	if b.Guidance != "" {
		if guidance != "" {
			guidance += "; "
		}
		guidance += b.Guidance
	}
	winner := a
	if verdictPriority[b.Verdict] > verdictPriority[a.Verdict] {
		winner = b
	}
	winner.Guidance = guidance
	return winner
}
