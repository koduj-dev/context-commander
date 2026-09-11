package http

import (
	"net/http"
	"strconv"

	"github.com/koduj-dev/context-commander/internal/paging"
)

// listResponse is the envelope every paginated list returns. It replaces
// the bare JSON arrays these endpoints used to return: an array has nowhere
// to put "there is more, continue from here", which left a client unable to
// tell a full last page from a full page with more behind it.
//
// next_cursor is omitted entirely on the last page rather than sent empty,
// so "is there more" is a field's presence, not a string comparison.
type listResponse[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// writeList writes one page. items is normalized to a non-nil slice so the
// JSON is always `[]`, never `null` — a client iterating the field should
// not have to special-case an empty page.
func writeList[T any](w http.ResponseWriter, items []T, nextCursor string) {
	if items == nil {
		items = []T{}
	}
	writeJSON(w, http.StatusOK, listResponse[T]{Items: items, NextCursor: nextCursor})
}

// pagingFrom reads ?limit= and ?cursor=. A malformed limit is a 400; an
// out-of-range one is clamped by paging.Options itself rather than
// rejected, since the caller still gets a well-formed page. The cursor is
// passed through opaque — only the domain that issued it can decode it, and
// an invalid one surfaces as paging.ErrInvalidCursor from there.
func pagingFrom(w http.ResponseWriter, r *http.Request) (paging.Options, bool) {
	opts := paging.Options{Cursor: r.URL.Query().Get("cursor")}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "?limit= must be an integer")
			return paging.Options{}, false
		}
		opts.Limit = v
	}
	return opts, true
}
