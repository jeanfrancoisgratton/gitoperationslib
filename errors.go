package gitoperationslib

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// ErrNotSupported is matched (via errors.Is) by every error returned when a
// backend cannot perform an operation, instead of silently degrading.
var ErrNotSupported = errors.New("operation not supported")

// NotSupportedError describes which operation a backend refused.
type NotSupportedError struct {
	Backend   Backend
	Operation string
	Reason    string // optional
}

func (e *NotSupportedError) Error() string {
	msg := fmt.Sprintf("%s: %s: %s", e.Backend, e.Operation, ErrNotSupported)
	if e.Reason != "" {
		msg += " (" + e.Reason + ")"
	}
	return msg
}

func (e *NotSupportedError) Is(target error) bool { return target == ErrNotSupported }

// NotSupported returns a *NotSupportedError.
func NotSupported(backend Backend, operation, reason string) error {
	return &NotSupportedError{Backend: backend, Operation: operation, Reason: reason}
}

// APIError is the normalised form of an error response from either backend.
type APIError struct {
	Backend    Backend
	StatusCode int
	Message    string
	// DocumentationURL is GitHub's documentation_url or Forgejo's url field.
	DocumentationURL string
	// Details holds GitHub's errors[] entries, flattened to strings.
	Details []string
}

func (e *APIError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = http.StatusText(e.StatusCode)
	}
	s := fmt.Sprintf("%s: HTTP %d: %s", e.Backend, e.StatusCode, msg)
	if len(e.Details) > 0 {
		s += " [" + strings.Join(e.Details, "; ") + "]"
	}
	return s
}

// StatusCode returns the HTTP status of the first *APIError in err's chain,
// or 0 if there is none.
func StatusCode(err error) int {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode
	}
	return 0
}

func IsNotFound(err error) bool     { return StatusCode(err) == http.StatusNotFound }
func IsUnauthorized(err error) bool { return StatusCode(err) == http.StatusUnauthorized }
func IsForbidden(err error) bool    { return StatusCode(err) == http.StatusForbidden }
