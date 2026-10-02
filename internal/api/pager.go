package api

import (
	"context"
	"errors"
	"iter"
)

// ErrCursorRepeated is what Pages yields when the service hands back the
// cursor it was just given, which would otherwise page for ever.
var ErrCursorRepeated = errors.New("polymarket api: the next page has the cursor of this one")

// Pages iterates over a cursor-paged listing, one page at a time. fetch is
// called with the empty cursor first and then with each cursor it returned,
// until it returns an empty one. An error ends the iteration and is yielded
// as its last element.
//
// Breaking out of the loop stops the paging, so the same iterator serves a
// caller that wants one more page and one that drains the listing.
func Pages[T any](ctx context.Context, fetch func(cursor string) ([]T, string, error)) iter.Seq2[[]T, error] {
	return func(yield func([]T, error) bool) {
		cursor := ""
		for {
			if err := ctx.Err(); err != nil {
				yield(nil, err)
				return
			}
			items, next, err := fetch(cursor)
			if err != nil {
				yield(nil, err)
				return
			}
			if !yield(items, nil) || next == "" {
				return
			}
			if next == cursor {
				yield(nil, ErrCursorRepeated)
				return
			}
			cursor = next
		}
	}
}
