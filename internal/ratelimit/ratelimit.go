// Package ratelimit bounds what one caller, and what the unauthenticated
// world as a whole, can make this server do.
//
// It deliberately does NOT limit per IP address. adapters/cli/serve.go
// refuses to trust X-Forwarded-For (there is no trusted-proxy
// configuration, and the header is spoofable without one), while the
// supported deployment is a loopback bind behind a TLS-terminating reverse
// proxy — so every request arrives with the proxy's address. A per-IP
// bucket would therefore put every user in ONE bucket, which is worse than
// no limit at all: any caller could lock out all the others by tripping the
// shared ceiling. Per-IP limiting becomes possible the day a trusted-proxy
// configuration exists, and not before.
//
// What is limited instead:
//
//   - Per credential, after authentication. Identity is proven at that
//     point, so no header has to be trusted. This is what contains a
//     runaway or compromised agent.
//   - The number of authentication attempts running at once, globally. An
//     unauthenticated caller still costs a database round trip — a token of
//     the form "cc_<any valid uuid>_<anything>" reaches the credential
//     lookup before being rejected — so this is the amplification worth
//     bounding. A concurrency ceiling rather than a rate: it bounds the
//     work in flight without ever refusing a caller who is simply early.
package ratelimit

import (
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/time/rate"
)

// Limits configures both mechanisms. The zero value disables limiting
// entirely, which is what tests and the CLI get.
type Limits struct {
	// PerCredential is the sustained requests per second one credential may
	// make; Burst is how far above it a short spike may go. Zero disables.
	PerCredential float64
	Burst         int

	// MaxConcurrentAuth bounds authentications in flight. Zero disables.
	MaxConcurrentAuth int
}

// idleEviction is how long an unused credential's bucket is kept. Long
// enough that a normally-paced agent never loses its accumulated burst
// allowance, short enough that a server issuing many short-lived
// credentials does not accumulate them forever.
const idleEviction = 30 * time.Minute

// Limiter holds one token bucket per credential plus the global
// authentication gate. Safe for concurrent use; one per process.
type Limiter struct {
	limits Limits

	mu      sync.Mutex
	buckets map[uuid.UUID]*bucket

	// authGate is a counting semaphore. Buffered channel rather than
	// sync.Cond or x/sync/semaphore: acquiring must be able to give up
	// immediately when full, which a channel select does in one line.
	authGate chan struct{}
}

type bucket struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func New(limits Limits) *Limiter {
	l := &Limiter{limits: limits, buckets: make(map[uuid.UUID]*bucket)}
	if limits.MaxConcurrentAuth > 0 {
		l.authGate = make(chan struct{}, limits.MaxConcurrentAuth)
	}
	return l
}

// AcquireAuth reserves a slot for one authentication attempt. The returned
// release must be called when the attempt finishes; ok=false means the
// server is already doing as many as it will do at once and the caller
// should be turned away rather than queued.
func (l *Limiter) AcquireAuth() (release func(), ok bool) {
	if l == nil || l.authGate == nil {
		return func() {}, true
	}
	select {
	case l.authGate <- struct{}{}:
		return func() { <-l.authGate }, true
	default:
		return func() {}, false
	}
}

// AllowCredential reports whether this credential may make one more
// request now.
func (l *Limiter) AllowCredential(credentialID uuid.UUID) bool {
	if l == nil || l.limits.PerCredential <= 0 {
		return true
	}

	l.mu.Lock()
	b, ok := l.buckets[credentialID]
	if !ok {
		burst := l.limits.Burst
		if burst <= 0 {
			burst = 1
		}
		b = &bucket{limiter: rate.NewLimiter(rate.Limit(l.limits.PerCredential), burst)}
		l.buckets[credentialID] = b
		l.evictIdleLocked()
	}
	b.lastSeen = time.Now()
	limiter := b.limiter
	l.mu.Unlock()

	// Outside the lock: rate.Limiter has its own.
	return limiter.Allow()
}

// evictIdleLocked drops buckets nobody has used recently. Called only when
// a new bucket is created, so the sweep cost is paid by growth rather than
// by a timer that runs forever in an idle process. Caller holds l.mu.
func (l *Limiter) evictIdleLocked() {
	if len(l.buckets) < 1024 {
		return
	}
	cutoff := time.Now().Add(-idleEviction)
	for id, b := range l.buckets {
		if b.lastSeen.Before(cutoff) {
			delete(l.buckets, id)
		}
	}
}
