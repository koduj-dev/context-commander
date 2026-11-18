// Package updatecheck tells an installation's administrators that a newer
// release exists (docs/adr/0043). It is off unless the operator turns it on:
// the server does not contact anyone by itself, and asking GitHub once a day
// is a choice the operator makes.
package updatecheck

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"golang.org/x/mod/semver"
)

// Source answers which release is newest; *selfupdate.Updater is one.
type Source interface {
	Latest(ctx context.Context, pre bool) (tag, url string, err error)
}

// Status is what the interface shows.
type Status struct {
	Current         string
	Enabled         bool
	Latest          string
	URL             string
	UpdateAvailable bool
	CheckedAt       *time.Time
	Error           string
}

// Checker asks Source now and then and remembers the answer.
type Checker struct {
	current string
	source  Source
	mu      sync.Mutex
	status  Status
}

// New returns a checker for the running version. A nil source means the
// check is off: Status says so and nothing is ever asked.
func New(current string, source Source) *Checker {
	return &Checker{current: current, source: source, status: Status{Current: current, Enabled: source != nil}}
}

// Status is the latest answer.
func (c *Checker) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

// Check asks once. A pre-release build follows pre-releases, so a beta hears
// about the next beta; a stable build hears only about stable releases.
func (c *Checker) Check(ctx context.Context) {
	if c.source == nil {
		return
	}
	pre := semver.Prerelease(c.current) != ""
	tag, url, err := c.source.Latest(ctx, pre)
	now := time.Now().UTC()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status.CheckedAt = &now
	if err != nil {
		c.status.Error = err.Error()
		slog.Warn("update check failed", "err", err)
		return
	}
	c.status.Error = ""
	c.status.Latest, c.status.URL = tag, url
	c.status.UpdateAvailable = semver.IsValid(c.current) && semver.Compare(tag, c.current) > 0
	if c.status.UpdateAvailable {
		slog.Info("a newer release is available", "current", c.current, "latest", tag, "url", url)
	}
}

// Run checks a minute after start and then about once a day, spread by up
// to an hour so a fleet of installations does not ask at the same instant,
// until ctx ends.
func (c *Checker) Run(ctx context.Context) {
	if c.source == nil {
		return
	}
	wait := time.Minute
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		c.Check(ctx)
		wait = 24*time.Hour + time.Duration(rand.Int64N(int64(time.Hour))) //nolint:gosec // spreading checks, not security
	}
}
