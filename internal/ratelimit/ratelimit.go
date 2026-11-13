// Package ratelimit bounds what one caller, and what the unauthenticated
// world as a whole, can make this server do.
//
// Per-IP limiting applies only to unauthenticated endpoints, and only to an
// address that means something (docs/adr/0029). Behind a reverse proxy every
// request arrives from the proxy, and one bucket for everyone would let any
// caller lock out all the others. internal/clientip reads the client from
// X-Forwarded-For only when the operator named the proxy (--trusted-proxies);
// without that, a loopback peer is the proxy and is not limited per IP.
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
	"crypto/sha256"
	"net/netip"
	"strings"
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

	// PerSignIn is the sustained password sign-in attempts per second
	// allowed for ONE identifier (an email address); SignInBurst is how many
	// may arrive together. Zero disables.
	//
	// Separate from PerCredential because a credential id is what a caller
	// receives AFTER signing in: the per-credential bucket cannot see a
	// brute-force attempt at all, since every failed guess belongs to no
	// credential.
	PerSignIn   float64
	SignInBurst int

	// MaxConcurrentPasswordHash bounds argon2id verifications in flight.
	// Zero disables.
	//
	// This is a memory bound, not a fairness one. Each verification holds
	// 64 MiB (internal/identity.argonMemory) for its duration, so running
	// them at MaxConcurrentAuth's ceiling of 64 would let unauthenticated
	// callers reserve 4 GiB — the hash parameters that protect a leaked
	// database are an amplifier pointed at the live server, and the answer
	// is to let only a few run at once rather than to weaken them.
	MaxConcurrentPasswordHash int

	// PerIP is the sustained unauthenticated requests per second allowed for
	// one client address; IPBurst is how many may arrive together. Zero
	// disables. IPv6 addresses share a bucket per /64, the block one host is
	// usually given, so rotating within it buys nothing.
	PerIP   float64
	IPBurst int
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

	// Keyed by a hash of the identifier rather than the identifier itself:
	// this map would otherwise be a list of every email address anyone has
	// tried to sign in with, sitting in memory and in any core dump.
	signIns map[[32]byte]*bucket

	// ips holds the per-address buckets for unauthenticated traffic.
	ips map[netip.Prefix]*bucket

	// authGate is a counting semaphore. Buffered channel rather than
	// sync.Cond or x/sync/semaphore: acquiring must be able to give up
	// immediately when full, which a channel select does in one line.
	authGate chan struct{}

	// hashGate bounds argon2id verifications specifically — see
	// Limits.MaxConcurrentPasswordHash.
	hashGate chan struct{}
}

type bucket struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func New(limits Limits) *Limiter {
	l := &Limiter{
		limits:  limits,
		buckets: make(map[uuid.UUID]*bucket),
		signIns: make(map[[32]byte]*bucket),
		ips:     make(map[netip.Prefix]*bucket),
	}
	if limits.MaxConcurrentAuth > 0 {
		l.authGate = make(chan struct{}, limits.MaxConcurrentAuth)
	}
	if limits.MaxConcurrentPasswordHash > 0 {
		l.hashGate = make(chan struct{}, limits.MaxConcurrentPasswordHash)
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

// AllowSignIn reports whether one more password sign-in attempt may be made
// for this identifier now.
//
// Keyed on the identifier the caller typed, lowercased — not on a resolved
// user id, because an attempt for an address with no account must consume the
// same allowance as one for an address with an account. If it did not, the
// limiter itself would answer "does this email exist here?" by refusing
// quickly in one case and slowly in the other.
func (l *Limiter) AllowSignIn(identifier string) bool {
	if l == nil || l.limits.PerSignIn <= 0 {
		return true
	}
	key := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(identifier))))

	l.mu.Lock()
	b, ok := l.signIns[key]
	if !ok {
		burst := l.limits.SignInBurst
		if burst <= 0 {
			burst = 1
		}
		b = &bucket{limiter: rate.NewLimiter(rate.Limit(l.limits.PerSignIn), burst)}
		l.signIns[key] = b
		l.evictIdleSignInsLocked()
	}
	b.lastSeen = time.Now()
	limiter := b.limiter
	l.mu.Unlock()

	return limiter.Allow()
}

// AcquirePasswordHash reserves a slot for one argon2id verification. ok=false
// means too many are already running and the caller must be turned away with
// a retry rather than queued — queueing would hold the request open while
// still not reducing the memory in flight, which is the thing being bounded.
func (l *Limiter) AcquirePasswordHash() (release func(), ok bool) {
	if l == nil || l.hashGate == nil {
		return func() {}, true
	}
	select {
	case l.hashGate <- struct{}{}:
		return func() { <-l.hashGate }, true
	default:
		return func() {}, false
	}
}

// evictIdleSignInsLocked is AllowSignIn's half of the same growth-paid sweep
// evictIdleLocked does for credentials. The threshold is lower because the
// key space here is attacker-chosen: anyone can mint a new bucket by trying
// a new address, so this map must not be allowed to become the memory leak
// that a per-credential map cannot be. Caller holds l.mu.
func (l *Limiter) evictIdleSignInsLocked() {
	if len(l.signIns) < 256 {
		return
	}
	cutoff := time.Now().Add(-idleEviction)
	for key, b := range l.signIns {
		if b.lastSeen.Before(cutoff) {
			delete(l.signIns, key)
		}
	}
}

// AllowIP reports whether one more unauthenticated request may come from
// this address now.
func (l *Limiter) AllowIP(a netip.Addr) bool {
	if l == nil || l.limits.PerIP <= 0 || !a.IsValid() {
		return true
	}
	a = a.Unmap()
	key := netip.PrefixFrom(a, a.BitLen())
	if a.Is6() {
		key, _ = a.Prefix(64)
	}

	l.mu.Lock()
	b, ok := l.ips[key]
	if !ok {
		burst := l.limits.IPBurst
		if burst <= 0 {
			burst = 1
		}
		b = &bucket{limiter: rate.NewLimiter(rate.Limit(l.limits.PerIP), burst)}
		l.ips[key] = b
		// Attacker-chosen key space, like sign-ins: sweep early.
		if len(l.ips) >= 1024 {
			cutoff := time.Now().Add(-idleEviction)
			for k, old := range l.ips {
				if old.lastSeen.Before(cutoff) {
					delete(l.ips, k)
				}
			}
		}
	}
	b.lastSeen = time.Now()
	limiter := b.limiter
	l.mu.Unlock()
	return limiter.Allow()
}
