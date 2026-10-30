// Package refusal recognises refusals in error responses and tags them with
// a sentinel error (APIError.Err), so callers can match them with errors.Is.
package refusal

import (
	"errors"
	"strings"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// Rule tags an *APIError with Sentinel when its status is Status and its
// message contains Fragment (case-insensitively). Status 0 matches any
// status; an empty Fragment matches any message.
type Rule struct {
	Status   int
	Fragment string
	Sentinel error
}

// Mark applies the first matching rule to err's *APIError and returns err.
// Errors that are not an *APIError, or already tagged, are left alone.
func Mark(err error, rules ...Rule) error {
	var apiErr *gitoperationslib.APIError
	if !errors.As(err, &apiErr) || apiErr.Err != nil {
		return err
	}
	msg := strings.ToLower(apiErr.Message)
	for _, r := range rules {
		if (r.Status == 0 || r.Status == apiErr.StatusCode) && strings.Contains(msg, strings.ToLower(r.Fragment)) {
			apiErr.Err = r.Sentinel
			break
		}
	}
	return err
}
