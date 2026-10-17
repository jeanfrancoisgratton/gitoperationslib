package forgejoapi

import (
	"net/http"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func TestListQuery(t *testing.T) {
	if got := ListQuery(gitoperationslib.ListOptions{}).Encode(); got != "" {
		t.Errorf("zero options gave %q", got)
	}
	if got := ListQuery(gitoperationslib.ListOptions{Page: 2, PerPage: 50}).Encode(); got != "limit=50&page=2" {
		t.Errorf("got %q", got)
	}
}

func TestNextPage(t *testing.T) {
	tests := []struct {
		name  string
		links []string
		want  int
	}{
		{"none", nil, 0},
		{"forgejo", []string{`<https://h/api/v1/repos/search?limit=1&page=2>; rel="next",<https://h/api/v1/repos/search?limit=1&page=26>; rel="last"`}, 2},
		{"next not first", []string{`<https://h/x?page=1>; rel="first", <https://h/x?page=4>; rel="next"`}, 4},
		{"last page", []string{`<https://h/x?page=1>; rel="first", <https://h/x?page=3>; rel="prev"`}, 0},
		{"multi-valued rel", []string{`<https://h/x?page=5>; rel="next last"`}, 5},
		{"unquoted rel, extra param", []string{`<https://h/x?a=1,2&page=7>; title="t"; rel=next`}, 7},
		{"separate headers", []string{`<https://h/x?page=1>; rel="prev"`, `<https://h/x?page=3>; rel="next"`}, 3},
		{"no page param", []string{`<https://h/x?cursor=abc>; rel="next"`}, 0},
		{"malformed", []string{`<https://h/x?page=2; rel="next"`}, 0},
	}
	for _, tt := range tests {
		h := http.Header{}
		for _, l := range tt.links {
			h.Add("Link", l)
		}
		if got := nextPage(h); got != tt.want {
			t.Errorf("%s: nextPage = %d, want %d", tt.name, got, tt.want)
		}
	}
}
