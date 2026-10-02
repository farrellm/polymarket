package api

import (
	"context"
	"errors"
	"slices"
	"testing"
)

// pagesOf serves the given pages, using each page's index as its cursor.
func pagesOf(pages [][]int, seen *[]string) func(string) ([]int, string, error) {
	cursors := []string{"", "a", "b", "c", "d"}
	return func(cursor string) ([]int, string, error) {
		*seen = append(*seen, cursor)
		i := slices.Index(cursors, cursor)
		next := ""
		if i+1 < len(pages) {
			next = cursors[i+1]
		}
		return pages[i], next, nil
	}
}

func TestPagesDrains(t *testing.T) {
	var seen []string
	var got []int
	for page, err := range Pages(context.Background(), pagesOf([][]int{{1, 2}, {3, 4}, {5}}, &seen)) {
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, page...)
	}
	if !slices.Equal(got, []int{1, 2, 3, 4, 5}) {
		t.Errorf("items = %v", got)
	}
	if !slices.Equal(seen, []string{"", "a", "b"}) {
		t.Errorf("cursors = %q", seen)
	}
}

func TestPagesStopsWhenTheCallerDoes(t *testing.T) {
	var seen []string
	for range Pages(context.Background(), pagesOf([][]int{{1}, {2}, {3}}, &seen)) {
		break
	}
	if !slices.Equal(seen, []string{""}) {
		t.Errorf("cursors = %q, want only the first page fetched", seen)
	}
}

func TestPagesYieldsTheError(t *testing.T) {
	boom := errors.New("boom")
	calls := 0
	fetch := func(string) ([]int, string, error) {
		calls++
		if calls == 2 {
			return nil, "", boom
		}
		return []int{calls}, "next", nil
	}

	var got []int
	var gotErr error
	for page, err := range Pages(context.Background(), fetch) {
		if err != nil {
			gotErr = err
			continue
		}
		got = append(got, page...)
	}
	if !errors.Is(gotErr, boom) {
		t.Errorf("error = %v, want boom", gotErr)
	}
	if !slices.Equal(got, []int{1}) || calls != 2 {
		t.Errorf("items = %v after %d calls, want the first page and then a stop", got, calls)
	}
}

func TestPagesStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	fetch := func(string) ([]int, string, error) {
		calls++
		cancel()
		return []int{calls}, "next" + string(rune('0'+calls)), nil
	}

	var gotErr error
	for _, err := range Pages(ctx, fetch) {
		gotErr = err
	}
	if !errors.Is(gotErr, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", gotErr)
	}
	if calls != 1 {
		t.Errorf("%d fetches, want 1", calls)
	}
}

func TestPagesRefusesARepeatedCursor(t *testing.T) {
	calls := 0
	fetch := func(string) ([]int, string, error) {
		calls++
		return []int{calls}, "same", nil
	}

	var gotErr error
	for _, err := range Pages(context.Background(), fetch) {
		gotErr = err
	}
	if !errors.Is(gotErr, ErrCursorRepeated) {
		t.Errorf("error = %v, want ErrCursorRepeated", gotErr)
	}
	if calls != 2 {
		t.Errorf("%d fetches, want 2", calls)
	}
}
