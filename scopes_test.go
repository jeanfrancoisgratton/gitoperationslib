package gitoperationslib

import (
	"errors"
	"testing"
)

func TestScopes(t *testing.T) {
	if got := RepoHooks("acme", "widget"); got != (HookScope{Owner: "acme", Repo: "widget"}) {
		t.Errorf("RepoHooks = %+v", got)
	}
	if got := OrgHooks("acme"); got != (HookScope{Owner: "acme"}) {
		t.Errorf("OrgHooks = %+v", got)
	}
	if got := RepoCI("acme", "widget"); got != (CIScope{Owner: "acme", Repo: "widget"}) {
		t.Errorf("RepoCI = %+v", got)
	}
	if got := OrgCI("acme"); got != (CIScope{Owner: "acme"}) {
		t.Errorf("OrgCI = %+v", got)
	}
	if got := UserCI(); got != (CIScope{}) {
		t.Errorf("UserCI = %+v", got)
	}
}

// The recognised refusal is reached through Unwrap, and the status stays.
func TestAPIErrorUnwrap(t *testing.T) {
	err := error(&APIError{StatusCode: 500, Message: "x", Err: ErrOwnsRepos})
	if !errors.Is(err, ErrOwnsRepos) || errors.Is(err, ErrLastOwner) {
		t.Errorf("errors.Is on %v", err)
	}
	if StatusCode(err) != 500 {
		t.Errorf("StatusCode = %d", StatusCode(err))
	}
	if got := errors.Unwrap(&APIError{StatusCode: 500}); got != nil {
		t.Errorf("Unwrap without a refusal = %v", got)
	}
}
