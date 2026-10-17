package gitoperationslib

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
)

func TestVisibilityValid(t *testing.T) {
	for _, v := range []Visibility{VisibilityPublic, VisibilityPrivate, VisibilityInternal} {
		if !v.Valid() {
			t.Errorf("%q should be valid", v)
		}
	}
	for _, v := range []Visibility{"", "secret"} {
		if v.Valid() {
			t.Errorf("%q should be invalid", v)
		}
	}
}

func TestMergeMethodValid(t *testing.T) {
	if !MergeMethodFastForwardOnly.Valid() {
		t.Error("fast-forward-only should be valid")
	}
	if MergeMethod("octopus").Valid() {
		t.Error("octopus should be invalid")
	}
}

func TestNotSupported(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", NotSupported(BackendGitHub, "repos.create", "multiple gitignores"))
	if !errors.Is(err, ErrNotSupported) {
		t.Fatal("errors.Is(err, ErrNotSupported) = false")
	}
	var nse *NotSupportedError
	if !errors.As(err, &nse) || nse.Backend != BackendGitHub || nse.Operation != "repos.create" {
		t.Fatalf("errors.As gave %+v", nse)
	}
	want := "wrapped: github: repos.create: operation not supported (multiple gitignores)"
	if err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
}

func TestAPIError(t *testing.T) {
	err := fmt.Errorf("get repo: %w", &APIError{Backend: BackendForgejo, StatusCode: 404})
	if !IsNotFound(err) || IsForbidden(err) || IsUnauthorized(err) {
		t.Error("status helpers disagree with StatusCode 404")
	}
	if got, want := err.Error(), "get repo: forgejo: HTTP 404: Not Found"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	if StatusCode(errors.New("plain")) != 0 {
		t.Error("StatusCode of a non-API error should be 0")
	}

	detailed := &APIError{Backend: BackendGitHub, StatusCode: 422, Message: "Validation Failed", Details: []string{"name already exists"}}
	if got, want := detailed.Error(), "github: HTTP 422: Validation Failed [name already exists]"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestCapability(t *testing.T) {
	c := CapRepos | CapWebhooks
	if !c.Has(CapRepos) || !c.Has(CapRepos|CapWebhooks) || c.Has(CapAdmin) || c.Has(CapRepos|CapAdmin) {
		t.Error("Has gave wrong results")
	}
	if got := c.String(); got != "repos|webhooks" {
		t.Errorf("String() = %q", got)
	}
	if got := Capability(0).String(); got != "none" {
		t.Errorf("String() = %q", got)
	}
}

// fakePages serves items in pages of size n, recording requested page numbers.
func fakePages(items []int, n int, requested *[]int) func(context.Context, ListOptions) (Page[int], error) {
	return func(_ context.Context, o ListOptions) (Page[int], error) {
		page := max(o.Page, 1)
		*requested = append(*requested, page)
		start := min((page-1)*n, len(items))
		end := min(start+n, len(items))
		p := Page[int]{Items: items[start:end]}
		if end < len(items) {
			p.NextPage = page + 1
		}
		return p, nil
	}
}

func TestAll(t *testing.T) {
	var requested []int
	var got []int
	for v, err := range All(context.Background(), ListOptions{}, fakePages([]int{1, 2, 3, 4, 5}, 2, &requested)) {
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, v)
	}
	if !slices.Equal(got, []int{1, 2, 3, 4, 5}) || !slices.Equal(requested, []int{1, 2, 3}) {
		t.Errorf("got %v, requested pages %v", got, requested)
	}
}

func TestAllStopsEarly(t *testing.T) {
	var requested []int
	for v := range All(context.Background(), ListOptions{}, fakePages([]int{1, 2, 3, 4, 5}, 2, &requested)) {
		if v == 2 {
			break
		}
	}
	if !slices.Equal(requested, []int{1}) {
		t.Errorf("requested pages %v, want [1]", requested)
	}
}

func TestAllError(t *testing.T) {
	boom := errors.New("boom")
	calls := 0
	fetch := func(context.Context, ListOptions) (Page[int], error) {
		calls++
		return Page[int]{}, boom
	}
	for _, err := range All(context.Background(), ListOptions{}, fetch) {
		if !errors.Is(err, boom) {
			t.Errorf("err = %v", err)
		}
	}
	if calls != 1 {
		t.Errorf("fetch called %d times", calls)
	}
}
