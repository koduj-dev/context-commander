package mcp

import "github.com/koduj-dev/context-commander/internal/paging"

// pagedResult is what every list tool returns. An agent that only wants the
// first page can ignore next_cursor entirely; one walking a long list echoes
// it back as `cursor` until it stops coming.
//
// The fields are declared on each tool's args struct rather than embedded
// from here, so the SDK's schema inference sees them directly and each tool
// can describe its own ordering in the field's jsonschema tag — what
// "continue from here" means differs between a newest-first artifact list
// and an oldest-first conversation.
type pagedResult[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

func paged[T any](items []T, nextCursor string) pagedResult[T] {
	if items == nil {
		items = []T{}
	}
	return pagedResult[T]{Items: items, NextCursor: nextCursor}
}

func pagingOptions(limit int, cursor string) paging.Options {
	return paging.Options{Limit: limit, Cursor: cursor}
}
