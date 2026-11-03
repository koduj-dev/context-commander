package http

import (
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/koduj-dev/context-commander/internal/plans"
)

// Board streams (docs/adr/0022, "Push instead of polling"), as Server-Sent
// Events. A stream says only that something changed:
//
//	event: ready     the stream is open; re-read once to cover the gap
//	event: changed   re-read the board (or presence)
//	: ping           a comment, so proxies do not close an idle connection
//
// What changed is never sent. The reader re-reads through the ordinary
// endpoint, which is where authorization and decryption already are, so a
// stream cannot show anyone more than a read would.
//
// A stream is authorized once, when it opens, and lives at most
// streamLifetime. The client reconnects, which authenticates and authorizes
// again — so a revoked credential, an expired session or a reader removed
// from the project loses the stream within that bound.
//
// Reverse proxies must not buffer these responses (nginx: proxy_buffering
// off, or honour the X-Accel-Buffering header sent here), or the events sit
// in the proxy until the stream ends.

// Atomics rather than constants so the tests can wait milliseconds, not
// minutes, without racing the handlers that read them.
var (
	streamHeartbeat = newDuration(25 * time.Second)
	streamLifetime  = newDuration(10 * time.Minute)
)

// streamWriteWait bounds one write. The server's WriteTimeout bounds the
// whole response, which a stream outlives by design, so each write pushes
// the deadline instead.
const streamWriteWait = 15 * time.Second

func newDuration(d time.Duration) *atomic.Int64 {
	var v atomic.Int64
	v.Store(int64(d))
	return &v
}

func (d Deps) streamPlanBoard(w http.ResponseWriter, r *http.Request) {
	sub, err := d.Plans.SubscribeBoard(r.Context(), principalIDFrom(r.Context()), credentialOf(r), projectFrom(r.Context()).ID)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	defer sub.Close()
	serveStream(w, r, sub)
}

func (d Deps) streamMyPresence(w http.ResponseWriter, r *http.Request) {
	sub, err := d.Plans.SubscribePresence(r.Context(), principalIDFrom(r.Context()), credentialOf(r))
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	defer sub.Close()
	serveStream(w, r, sub)
}

// credentialOf is the credential the stream cap is counted against. The
// authentication middleware always sets one; uuid.Nil would only pool every
// such caller under one cap, never lift it.
func credentialOf(r *http.Request) uuid.UUID {
	if id := clientIDFrom(r.Context()); id != nil {
		return *id
	}
	return uuid.Nil
}

// changeStream is what serveStream needs of a *plans.Subscription.
type changeStream interface {
	C() <-chan struct{}
	Done() <-chan struct{}
}

var _ changeStream = (*plans.Subscription)(nil)

func serveStream(w http.ResponseWriter, r *http.Request, sub changeStream) {
	rc := http.NewResponseController(w)
	// Neither of the server's whole-request deadlines fits a response meant
	// to stay open: the read one is cleared here, and the write one, which
	// would fail the first write after 60 seconds, is pushed forward on
	// every write by send.
	_ = rc.SetReadDeadline(time.Time{})

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	// nginx reads this and stops buffering this one response.
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	send := func(frame string) bool {
		if err := rc.SetWriteDeadline(time.Now().Add(streamWriteWait)); err != nil {
			return false
		}
		if _, err := io.WriteString(w, frame); err != nil {
			return false
		}
		return rc.Flush() == nil
	}

	// retry tells EventSource-style clients how long to wait before
	// reconnecting; the web client does its own, with the same spirit.
	if !send(fmt.Sprintf("retry: %d\nevent: ready\ndata: {}\n\n", (5 * time.Second).Milliseconds())) {
		return
	}
	heartbeat := time.NewTicker(time.Duration(streamHeartbeat.Load()))
	defer heartbeat.Stop()
	lifetime := time.NewTimer(time.Duration(streamLifetime.Load()))
	defer lifetime.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-sub.Done():
			return
		case <-lifetime.C:
			return
		case <-heartbeat.C:
			if !send(": ping\n\n") {
				return
			}
		case <-sub.C():
			if !send("event: changed\ndata: {}\n\n") {
				return
			}
		}
	}
}
