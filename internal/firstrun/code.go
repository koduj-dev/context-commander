// Package firstrun holds the one-time code that lets the very first
// administrator be created through the browser instead of through a shell on
// the server — docs/adr/0019.
//
// The code exists only in this process's memory and only while the
// installation has no users. It is never written to the database, which is
// the whole point: a code in a table survives a restart, appears in every
// backup, and outlives its purpose by months. This one dies with the process
// and the next start prints a new one.
//
// What it assumes, stated plainly: anyone who can read the log of a freshly
// installed instance is already trusted with that instance. That is strictly
// stronger than "whoever reaches the setup screen first becomes admin", and
// strictly weaker than "you must have shell access on the host" — which is
// the thing being fixed.
package firstrun

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"sync"
)

// Code is a single-use setup code. A nil *Code accepts nothing, which is what
// an installation that already has users gets.
type Code struct {
	mu       sync.Mutex
	value    string
	consumed bool
}

// NewCode mints a code with 128 bits of entropy. Hex rather than base64 so it
// survives being read off a terminal and typed into a form by hand, which is
// exactly how it will be used.
func NewCode() (*Code, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("firstrun: generate setup code: %w", err)
	}
	return &Code{value: hex.EncodeToString(buf)}, nil
}

// String returns the code, for printing to the server's own log and nowhere
// else.
func (c *Code) String() string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.value
}

// Consume checks a presented code and, on a match, spends it: a second call
// returns false however correct the code is. Compared in constant time, and
// the already-consumed check happens under the same lock as the comparison so
// two simultaneous requests cannot both succeed.
func (c *Code) Consume(presented string) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.consumed || c.value == "" {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(presented), []byte(c.value)) != 1 {
		return false
	}
	c.consumed = true
	return true
}

// Spent reports whether the code has been used. Only for deciding what to
// tell a caller that arrives after setup finished; it is not an authorization
// check and must never be used as one.
func (c *Code) Spent() bool {
	if c == nil {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.consumed
}
