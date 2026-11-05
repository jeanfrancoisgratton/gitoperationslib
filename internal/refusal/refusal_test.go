package refusal

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

var (
	errFirst  = errors.New("first")
	errSecond = errors.New("second")
)

func apiError(status int, message string) *gitoperationslib.APIError {
	return &gitoperationslib.APIError{StatusCode: status, Message: message}
}

func TestMark(t *testing.T) {
	first := Rule{Status: 500, Fragment: "still owns", Sentinel: errFirst}
	for name, tc := range map[string]struct {
		status  int
		message string
		rules   []Rule
		want    error
	}{
		"status and fragment":     {500, "org still owns repos", []Rule{first}, errFirst},
		"fragment ignores case":   {500, "Org STILL OWNS repos", []Rule{first}, errFirst},
		"fragment in lower case":  {500, "org still owns repos", []Rule{{Status: 500, Fragment: "STILL OWNS", Sentinel: errFirst}}, errFirst},
		"wrong status":            {422, "org still owns repos", []Rule{first}, nil},
		"wrong fragment":          {500, "something else", []Rule{first}, nil},
		"status 0 matches any":    {418, "still owns", []Rule{{Fragment: "still owns", Sentinel: errFirst}}, errFirst},
		"empty fragment":          {500, "anything", []Rule{{Status: 500, Sentinel: errFirst}}, errFirst},
		"first match wins":        {500, "still owns", []Rule{first, {Status: 500, Sentinel: errSecond}}, errFirst},
		"falls through to second": {500, "other", []Rule{first, {Status: 500, Sentinel: errSecond}}, errSecond},
		"no rules":                {500, "still owns", nil, nil},
	} {
		apiErr := apiError(tc.status, tc.message)
		if got := Mark(apiErr, tc.rules...); got != error(apiErr) {
			t.Errorf("%s: Mark returned %v, want the same error", name, got)
		}
		if apiErr.Err != tc.want {
			t.Errorf("%s: Err = %v, want %v", name, apiErr.Err, tc.want)
		}
		if tc.want != nil && !errors.Is(apiErr, tc.want) {
			t.Errorf("%s: errors.Is does not see the sentinel", name)
		}
	}
}

func TestMarkLeavesOtherErrorsAlone(t *testing.T) {
	rule := Rule{Sentinel: errFirst}

	if err := Mark(nil, rule); err != nil {
		t.Errorf("nil: %v", err)
	}
	plain := errors.New("plain")
	if err := Mark(plain, rule); err != plain {
		t.Errorf("not an APIError: %v", err)
	}

	// Already tagged: the first tag stays.
	tagged := &gitoperationslib.APIError{StatusCode: 500, Message: "x", Err: errSecond}
	Mark(tagged, rule)
	if tagged.Err != errSecond {
		t.Errorf("tagged error retagged: %v", tagged.Err)
	}
}

func TestMarkFindsWrappedAPIError(t *testing.T) {
	apiErr := apiError(500, "still owns")
	wrapped := fmt.Errorf("orgs.delete: %w", apiErr)
	if got := Mark(wrapped, Rule{Fragment: "still owns", Sentinel: errFirst}); got != wrapped {
		t.Errorf("Mark returned %v, want the wrapping error", got)
	}
	if !errors.Is(wrapped, errFirst) {
		t.Errorf("errors.Is(%v, errFirst) = false", wrapped)
	}
}
