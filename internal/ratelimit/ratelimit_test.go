package ratelimit_test

import (
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
