// Package partial helps backends build Directory info results from several
// reads, keeping the parts the caller may not see out of the result instead
// of failing the whole call.
package partial

import (
	"context"
	"errors"
	"fmt"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// PerPage is the page size used to collect lists.
const PerPage = 50

// Collect returns every item of a paginated list.
func Collect[T any](ctx context.Context, fetch func(context.Context, gitoperationslib.ListOptions) (gitoperationslib.Page[T], error)) ([]T, error) {
	items := []T{}
	for item, err := range gitoperationslib.All(ctx, gitoperationslib.ListOptions{PerPage: PerPage}, fetch) {
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

// Result accumulates the parts of an info call the server refused.
type Result struct {
	errs  []error
	parts []gitoperationslib.PartError
}

// Add records err for part when the caller lacks access to it (HTTP 401,
// 403, 404) or the backend cannot read it (ErrNotSupported), and returns
// nil; any other error is returned unchanged so the call fails.
func (r *Result) Add(part string, err error) error {
	if err == nil {
		return nil
	}
	status := gitoperationslib.StatusCode(err)
	switch status {
	case 401, 403, 404:
	default:
		if !errors.Is(err, gitoperationslib.ErrNotSupported) {
			return err
		}
	}
	r.errs = append(r.errs, fmt.Errorf("%s: %w", part, err))
	r.parts = append(r.parts, gitoperationslib.PartError{Part: part, Status: status, Message: err.Error()})
	return nil
}

// Err returns the recorded refusals joined, or nil.
func (r *Result) Err() error { return errors.Join(r.errs...) }

// Parts returns the recorded refusals as data, or nil.
func (r *Result) Parts() []gitoperationslib.PartError { return r.parts }
