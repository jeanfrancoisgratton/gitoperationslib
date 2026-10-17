package gitoperationslib

import (
	"context"
	"iter"
)

// ListOptions selects one page of a list call. Zero values mean
// "first page" and "backend default page size".
type ListOptions struct {
	Page    int // 1-based
	PerPage int
}

// Page is one page of results.
type Page[T any] struct {
	Items []T
	// NextPage is the page number to request next, or 0 if this is the last page.
	NextPage int
}

// All iterates over every item of a paginated list, starting at opts.Page.
// Iteration stops at the first error, which is yielded with a zero item.
//
//	for repo, err := range gitoperationslib.All(ctx, opts, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Repo], error) {
//		return client.Repos().List(ctx, "owner", o)
//	}) { ... }
func All[T any](ctx context.Context, opts ListOptions, fetch func(context.Context, ListOptions) (Page[T], error)) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		for {
			page, err := fetch(ctx, opts)
			if err != nil {
				var zero T
				yield(zero, err)
				return
			}
			for _, item := range page.Items {
				if !yield(item, nil) {
					return
				}
			}
			if page.NextPage == 0 {
				return
			}
			opts.Page = page.NextPage
		}
	}
}
