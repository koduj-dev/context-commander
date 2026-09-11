// Package paging is the one keyset-pagination contract every growing list
// endpoint shares — see docs/architecture-cookbook.md §22.
//
// Keyset, never offset: OFFSET re-scans and skips rows when the underlying
// set changes between pages, which for an append-heavy store like this one
// is the common case, not the exotic one. Each list orders by a key that
// only moves in one direction and asks for "the rows past this position".
//
// The position itself is opaque on the wire. Callers echo NextCursor back
// and never parse it, so the encoding below can change without breaking
// them — and because each cursor names its own kind, a cursor from one
// endpoint pasted into another is rejected outright instead of being
// reinterpreted as a position it never meant.
package paging

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	// DefaultLimit applies when a caller asks for no particular page size.
	DefaultLimit = 100
	// MaxLimit bounds what a caller can ask for. A limit above it is
	// clamped rather than rejected: the caller gets a smaller page and a
	// cursor, which is the behavior they wanted anyway.
	MaxLimit = 500
)

// ErrInvalidCursor is returned for a cursor that is malformed, or that
// belongs to a different list than the one being paged. Adapters map it to
// 400 — it is a client mistake, not a server fault.
var ErrInvalidCursor = errors.New("paging: invalid cursor")

// Options is the request half of the contract: how many rows, and where to
// continue from. The zero value asks for the first page at DefaultLimit.
type Options struct {
	Limit  int
	Cursor string
}

// ClampedLimit is the page size to actually query with — never zero, never
// above MaxLimit. Services must call this rather than trusting Limit.
func (o Options) ClampedLimit() int {
	if o.Limit <= 0 {
		return DefaultLimit
	}
	if o.Limit > MaxLimit {
		return MaxLimit
	}
	return o.Limit
}

// Page is the response half: one page of rows, plus where the next one
// starts. An empty NextCursor means this was the last page — services
// determine that by asking the database for one row more than the caller's
// limit (see More), so "there is more" is a fact about the data, not a
// guess from a full page.
type Page[T any] struct {
	Items      []T
	NextCursor string
}

// More reports whether rows holds more than limit entries — i.e. whether the
// limit+1 probe row came back — and returns rows trimmed to the page the
// caller asked for. Every service's List follows the same three lines:
// query LIMIT+1, call More, and encode a cursor from the last kept row only
// if More said there is one.
func More[T any](rows []T, limit int) (page []T, hasMore bool) {
	if len(rows) > limit {
		return rows[:limit], true
	}
	return rows, false
}

// Cursor kinds. A cursor names the shape of the position it carries so that
// decoding can refuse one meant for a differently-ordered list.
const (
	kindTimeUUID = "tu" // (timestamp, uuid) — rows keyed by a UUID, ordered by time
	kindSeq      = "n"  // a monotonic integer — bigserial seq/id columns
	kindString   = "s"  // a unique text key — e.g. a secret name
)

func encode(kind string, parts ...string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(kind + "|" + strings.Join(parts, "|")))
}

func decode(cursor, wantKind string, wantParts int) ([]string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, fmt.Errorf("%w: not base64url", ErrInvalidCursor)
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != wantParts+1 {
		return nil, fmt.Errorf("%w: wrong shape", ErrInvalidCursor)
	}
	if parts[0] != wantKind {
		return nil, fmt.Errorf("%w: belongs to a different list", ErrInvalidCursor)
	}
	return parts[1:], nil
}

// EncodeTimeUUID builds a cursor for a list ordered by (timestamp, id) —
// the shape every UUID-keyed table here uses. The id half is not decoration:
// two rows can share a timestamp to the microsecond, so a timestamp-only
// cursor either skips rows or repeats them.
func EncodeTimeUUID(t time.Time, id uuid.UUID) string {
	return encode(kindTimeUUID, strconv.FormatInt(t.UTC().UnixNano(), 10), id.String())
}

// DecodeTimeUUID reads a cursor built by EncodeTimeUUID. ok is false when
// there is no cursor at all — the first page — which is not an error.
func (o Options) DecodeTimeUUID() (t time.Time, id uuid.UUID, ok bool, err error) {
	if o.Cursor == "" {
		return time.Time{}, uuid.Nil, false, nil
	}
	parts, err := decode(o.Cursor, kindTimeUUID, 2)
	if err != nil {
		return time.Time{}, uuid.Nil, false, err
	}
	nanos, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, uuid.Nil, false, fmt.Errorf("%w: bad timestamp", ErrInvalidCursor)
	}
	id, err = uuid.Parse(parts[1])
	if err != nil {
		return time.Time{}, uuid.Nil, false, fmt.Errorf("%w: bad id", ErrInvalidCursor)
	}
	return time.Unix(0, nanos).UTC(), id, true, nil
}

// EncodeSeq builds a cursor for a list ordered by a monotonic integer.
func EncodeSeq(seq int64) string {
	return encode(kindSeq, strconv.FormatInt(seq, 10))
}

// DecodeSeq reads a cursor built by EncodeSeq.
func (o Options) DecodeSeq() (seq int64, ok bool, err error) {
	if o.Cursor == "" {
		return 0, false, nil
	}
	parts, err := decode(o.Cursor, kindSeq, 1)
	if err != nil {
		return 0, false, err
	}
	seq, err = strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("%w: bad sequence", ErrInvalidCursor)
	}
	return seq, true, nil
}

// EncodeString builds a cursor for a list ordered by a unique text key.
func EncodeString(key string) string {
	return encode(kindString, key)
}

// DecodeString reads a cursor built by EncodeString. The key may itself
// contain "|", so the payload is rejoined rather than taken as one part.
func (o Options) DecodeString() (key string, ok bool, err error) {
	if o.Cursor == "" {
		return "", false, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(o.Cursor)
	if err != nil {
		return "", false, fmt.Errorf("%w: not base64url", ErrInvalidCursor)
	}
	kind, payload, found := strings.Cut(string(raw), "|")
	if !found || kind != kindString {
		return "", false, fmt.Errorf("%w: belongs to a different list", ErrInvalidCursor)
	}
	return payload, true, nil
}
