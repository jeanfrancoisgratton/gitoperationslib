package partial

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func TestCollect(t *testing.T) {
	var asked []gitoperationslib.ListOptions
	items, err := Collect(context.Background(), func(_ context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[int], error) {
		asked = append(asked, o)
		if o.Page < 2 {
			return gitoperationslib.Page[int]{Items: []int{1, 2}, NextPage: 2}, nil
		}
		return gitoperationslib.Page[int]{Items: []int{3}}, nil
	})
	if err != nil || len(items) != 3 || items[2] != 3 {
		t.Fatalf("items = %v, err = %v", items, err)
	}
	if len(asked) != 2 || asked[0].PerPage != PerPage || asked[1].Page != 2 {
		t.Errorf("requests = %+v", asked)
	}

	empty, err := Collect(context.Background(), func(context.Context, gitoperationslib.ListOptions) (gitoperationslib.Page[int], error) {
		return gitoperationslib.Page[int]{}, nil
	})
	if err != nil || empty == nil || len(empty) != 0 {
		t.Errorf("empty list: %v, %v (want a non-nil empty slice)", empty, err)
	}
}

func TestResult(t *testing.T) {
	var r Result
	if r.Err() != nil {
		t.Fatal("new Result has an error")
	}
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound} {
		if err := r.Add("part", &gitoperationslib.APIError{StatusCode: status}); err != nil {
			t.Errorf("HTTP %d not tolerated: %v", status, err)
		}
	}
	if err := r.Add("teams", gitoperationslib.NotSupported(gitoperationslib.BackendGitHub, "x", "")); err != nil {
		t.Errorf("ErrNotSupported not tolerated: %v", err)
	}
	if err := r.Add("ok", nil); err != nil {
		t.Error(err)
	}

	err := r.Err()
	if !errors.Is(err, gitoperationslib.ErrNotSupported) || gitoperationslib.StatusCode(err) != http.StatusUnauthorized {
		t.Errorf("joined error loses its parts: %v", err)
	}

	parts := r.Parts()
	if len(parts) != 4 || parts[0].Part != "part" || parts[0].Status != 401 || !strings.Contains(parts[0].Message, "HTTP 401") ||
		parts[3].Part != "teams" || parts[3].Status != 0 || !strings.Contains(parts[3].Message, "not supported") {
		t.Errorf("Parts = %+v", parts)
	}

	fatal := []error{&gitoperationslib.APIError{StatusCode: http.StatusInternalServerError}, context.Canceled}
	for _, want := range fatal {
		if got := r.Add("x", want); got != want {
			t.Errorf("Add(%v) = %v, want it returned unchanged", want, got)
		}
	}
}
