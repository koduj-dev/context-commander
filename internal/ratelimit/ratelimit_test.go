package ratelimit_test

import (
	"net/netip"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/koduj-dev/context-commander/internal/ratelimit"
)

func TestZeroLimitsAllowEverything(t *testing.T) {
	// The zero value is what the CLI and most tests construct. It must not
	// quietly throttle anything.
	l := ratelimit.New(ratelimit.Limits{})
	id := uuid.New()
	for i := range 1000 {
		if !l.AllowCredential(id) {
			t.Fatalf("request %d refused by a limiter with no limits configured", i)
		}
	}
	release, ok := l.AcquireAuth()
	if !ok {
		t.Fatal("AcquireAuth refused with no concurrency limit configured")
	}
	release()
}

func TestNilLimiterAllowsEverything(t *testing.T) {
	// Deps.Limiter is documented as safe to leave nil.
	var l *ratelimit.Limiter
	if !l.AllowCredential(uuid.New()) {
		t.Fatal("nil limiter refused a request")
	}
	if _, ok := l.AcquireAuth(); !ok {
		t.Fatal("nil limiter refused an authentication")
	}
}

func TestBurstIsSpentThenRefused(t *testing.T) {
	// A very low sustained rate so the bucket does not measurably refill
	// during the test: what is being pinned is that the burst is finite.
	l := ratelimit.New(ratelimit.Limits{PerCredential: 0.001, Burst: 5})
	id := uuid.New()

	for i := range 5 {
		if !l.AllowCredential(id) {
			t.Fatalf("request %d was refused while still inside the burst of 5", i)
		}
	}
	if l.AllowCredential(id) {
		t.Fatal("the sixth request was allowed despite a burst of 5 and an effectively zero refill rate")
	}
}

func TestCredentialsAreLimitedIndependently(t *testing.T) {
	// One noisy agent must not throttle another. This is the property a
	// per-IP limiter could not provide behind a reverse proxy, and the
	// reason this package limits per credential instead.
	l := ratelimit.New(ratelimit.Limits{PerCredential: 0.001, Burst: 2})
	noisy, quiet := uuid.New(), uuid.New()

	for range 2 {
		l.AllowCredential(noisy)
	}
	if l.AllowCredential(noisy) {
		t.Fatal("the noisy credential was not throttled")
	}
	if !l.AllowCredential(quiet) {
		t.Fatal("a second credential was refused because a different one exhausted its budget")
	}
}

func TestAuthConcurrencyIsBounded(t *testing.T) {
	l := ratelimit.New(ratelimit.Limits{MaxConcurrentAuth: 2})

	first, ok := l.AcquireAuth()
	if !ok {
		t.Fatal("first acquire refused")
	}
	second, ok := l.AcquireAuth()
	if !ok {
		t.Fatal("second acquire refused")
	}
	if _, ok := l.AcquireAuth(); ok {
		t.Fatal("a third concurrent authentication was admitted past a ceiling of 2")
	}

	// Releasing frees the slot rather than leaking it — a limiter that
	// never recovers is an outage with extra steps.
	first()
	third, ok := l.AcquireAuth()
	if !ok {
		t.Fatal("a slot was not freed by release")
	}
	third()
	second()
}

func TestConcurrentUseIsRaceFree(t *testing.T) {
	l := ratelimit.New(ratelimit.Limits{PerCredential: 1000, Burst: 1000, MaxConcurrentAuth: 8})
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := uuid.New()
			for range 20 {
				l.AllowCredential(id)
				if release, ok := l.AcquireAuth(); ok {
					release()
				}
			}
		}()
	}
	wg.Wait()
}

func TestAllowSignInIsPerIdentifier(t *testing.T) {
	l := ratelimit.New(ratelimit.Limits{PerSignIn: 0.0001, SignInBurst: 2})

	// Two statements, not `a() || a()`: that form works only because || still
	// evaluates its right side, which reads as a copy-paste mistake and
	// breaks silently if anyone ever makes it &&.
	for i := 1; i <= 2; i++ {
		if !l.AllowSignIn("alice@example.test") {
			t.Fatalf("attempt %d within the burst was refused", i)
		}
	}
	if l.AllowSignIn("alice@example.test") {
		t.Error("a third attempt was allowed past a burst of 2")
	}
	// One identifier exhausting its allowance must not lock out another, or
	// anyone could lock any colleague out by guessing at their address.
	if !l.AllowSignIn("bob@example.test") {
		t.Error("a different identifier was refused because of alice's attempts")
	}
}

func TestAllowSignInIgnoresCaseAndSurroundingSpace(t *testing.T) {
	// Otherwise the limit is bypassed by typing the same address differently,
	// which is the first thing a brute-force script would do.
	l := ratelimit.New(ratelimit.Limits{PerSignIn: 0.0001, SignInBurst: 1})
	if !l.AllowSignIn("alice@example.test") {
		t.Fatal("the first attempt was refused")
	}
	for _, variant := range []string{"ALICE@example.test", " alice@example.test ", "Alice@Example.Test"} {
		if l.AllowSignIn(variant) {
			t.Errorf("%q was treated as a different identifier", variant)
		}
	}
}

func TestAllowSignInDisabledByZero(t *testing.T) {
	l := ratelimit.New(ratelimit.Limits{})
	for i := 0; i < 50; i++ {
		if !l.AllowSignIn("alice@example.test") {
			t.Fatalf("attempt %d refused although sign-in limiting is disabled", i)
		}
	}
}

func TestAcquirePasswordHashBoundsConcurrency(t *testing.T) {
	// The memory bound that keeps argon2id's cost from being an amplifier
	// pointed at the live server.
	l := ratelimit.New(ratelimit.Limits{MaxConcurrentPasswordHash: 2})

	first, ok := l.AcquirePasswordHash()
	if !ok {
		t.Fatal("the first verification was refused")
	}
	second, ok := l.AcquirePasswordHash()
	if !ok {
		t.Fatal("the second verification was refused")
	}
	if _, ok := l.AcquirePasswordHash(); ok {
		t.Error("a third concurrent verification was admitted past a ceiling of 2")
	}

	first()
	third, ok := l.AcquirePasswordHash()
	if !ok {
		t.Error("a slot did not become available after one was released")
	}
	second()
	third()
}

func TestAcquirePasswordHashDisabledByZero(t *testing.T) {
	l := ratelimit.New(ratelimit.Limits{})
	for i := 0; i < 100; i++ {
		release, ok := l.AcquirePasswordHash()
		if !ok {
			t.Fatalf("acquire %d refused although the gate is disabled", i)
		}
		defer release()
	}
}

func TestAllowIPIsPerAddressAndPerSlash64(t *testing.T) {
	l := ratelimit.New(ratelimit.Limits{PerIP: 0.001, IPBurst: 2})
	a := netip.MustParseAddr("198.51.100.7")
	for i := 0; i < 2; i++ {
		if !l.AllowIP(a) {
			t.Fatalf("request %d of the burst was refused", i+1)
		}
	}
	if l.AllowIP(a) {
		t.Fatal("a third request inside the burst window was allowed")
	}
	if !l.AllowIP(netip.MustParseAddr("198.51.100.8")) {
		t.Fatal("another address shared the first one's bucket")
	}
	v6 := ratelimit.New(ratelimit.Limits{PerIP: 0.001, IPBurst: 1})
	if !v6.AllowIP(netip.MustParseAddr("2001:db8:1:2::1")) {
		t.Fatal("first v6 request refused")
	}
	if v6.AllowIP(netip.MustParseAddr("2001:db8:1:2::ffff")) {
		t.Fatal("a second address in the same /64 got a fresh bucket")
	}
	if !ratelimit.New(ratelimit.Limits{}).AllowIP(a) {
		t.Fatal("the zero value limited")
	}
}
