package gitoperationslib

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ErrNotSupported is matched (via errors.Is) by every error returned when a
// backend cannot perform an operation, instead of silently degrading.
var ErrNotSupported = errors.New("operation not supported")

// ErrOwnerTeam is returned when an operation would delete an organisation's
// owner team, rename it, or change its permissions. Forgejo allows deleting
// it, which leaves the organisation without owners.
var ErrOwnerTeam = errors.New("refusing to delete, rename or change the permissions of an owner team")

// Refusals recognised in a server's error response. The *APIError keeps the
// server's status and message, and matches the sentinel through errors.Is:
//
//	if errors.Is(err, gitoperationslib.ErrOwnsRepos) { … }
//
// Recognition relies on the server's wording; an unrecognised refusal is
// reported as a plain *APIError, as before.
var (
	// ErrOwnsRepos: deleting an organisation (Forgejo: HTTP 500) or a user
	// without Purge (HTTP 422) that still owns repositories.
	ErrOwnsRepos = errors.New("the account still owns repositories")
	// ErrOrgMember: deleting a user without Purge who still belongs to
	// organisations (Forgejo).
	ErrOrgMember = errors.New("the user still belongs to organisations")
	// ErrLastOwner: removing the last member of an organisation's owner team,
	// from the organisation or from the team (Forgejo: HTTP 500).
	ErrLastOwner = errors.New("cannot remove the last owner of an organisation")
	// ErrBranchProtected: deleting a branch a protection rule covers
	// (Forgejo 403, GitHub 422, Bitbucket 500).
	ErrBranchProtected = errors.New("the branch is protected")
	// ErrInvalidOTP: basic authentication of an account with two-factor
	// authentication, without a valid code (Forgejo: HTTP 401 on every
	// request, forgejo.New included). Retry with forgejo.BasicAuthOTP.
	ErrInvalidOTP = errors.New("two-factor authentication code missing or invalid")
	// ErrGPGSignature: a GPG key needs a valid signature of the verification
	// token (GPGKeys.VerificationToken). Forgejo answers HTTP 404 when none of
	// the key's e-mail addresses belongs to the account, and 422 when the
	// signature does not match.
	ErrGPGSignature = errors.New("the GPG key needs a valid signature of the verification token")
	// ErrPasswordRequired: the operation needs username and password
	// authentication, not a token (Forgejo: HTTP 401 "auth method not
	// allowed" when creating or deleting access tokens).
	ErrPasswordRequired = errors.New("the operation needs username and password authentication")
)

// NotSupportedError describes which operation a backend refused.
type NotSupportedError struct {
	Backend   Backend `json:"backend"`
	Operation string  `json:"operation"`
	Reason    string  `json:"reason,omitzero"` // optional
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

// UnsupportedVersionError is returned when a server is older than the
// minimum version the library supports.
type UnsupportedVersionError struct {
	Backend Backend `json:"backend"`
	Version string  `json:"version"`
	Minimum string  `json:"minimum"`
}

func (e *UnsupportedVersionError) Error() string {
	return fmt.Sprintf("%s: server version %s is below the minimum supported version %s", e.Backend, e.Version, e.Minimum)
}

// APIError is the normalised form of an error response from either backend.
type APIError struct {
	Backend    Backend `json:"backend"`
	StatusCode int     `json:"statusCode"`
	Message    string  `json:"message"`
	// DocumentationURL is GitHub's documentation_url or Forgejo's url field.
	DocumentationURL string `json:"documentationUrl,omitzero"`
	// Details holds GitHub's errors[] entries, flattened to strings.
	Details []string `json:"details,omitzero"`
	// RetryAt is when the server allows the request again: from Retry-After,
	// or from a rate limit's reset time. Zero when the server did not say.
	RetryAt time.Time `json:"retryAt,omitzero"`
	// Err is the recognised refusal (ErrOwnsRepos, ErrBranchProtected…), or
	// nil. It is what errors.Is and errors.As see through Unwrap.
	Err error `json:"-"`
}

func (e *APIError) Unwrap() error { return e.Err }

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
