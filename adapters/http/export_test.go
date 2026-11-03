package http

import "time"

// SetStreamTimings shortens the board streams' heartbeat and lifetime for a
// test, and returns what puts them back.
func SetStreamTimings(heartbeat, lifetime time.Duration) (restore func()) {
	hb, life := streamHeartbeat.Swap(int64(heartbeat)), streamLifetime.Swap(int64(lifetime))
	return func() { streamHeartbeat.Store(hb); streamLifetime.Store(life) }
}
