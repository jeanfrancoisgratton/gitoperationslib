package forgejoapi

import (
	"net/url"
	"testing"
)

func TestResponseNext(t *testing.T) {
	tests := []struct {
		name string
		resp Response
		n    int
		want int
	}{
		{"link wins", Response{NextPage: 7, TotalCount: 3, Page: 1}, 1, 7},
		{"no link, more items", Response{TotalCount: 5, Page: 1}, 2, 2},
		{"no link, later page", Response{TotalCount: 5, Page: 2}, 2, 3},
		{"no link, full last page", Response{TotalCount: 4, Page: 2}, 2, 0},
		{"no link, all at once", Response{TotalCount: 5, Page: 1}, 5, 0},
		{"short last page costs one request", Response{TotalCount: 5, Page: 3}, 1, 4},
		{"empty page ends it", Response{TotalCount: 5, Page: 4}, 0, 0},
		{"no count, no link", Response{TotalCount: -1, Page: 1}, 30, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.resp.Next(tt.n); got != tt.want {
				t.Errorf("Next(%d) = %d, want %d", tt.n, got, tt.want)
			}
		})
	}
}

func TestRequestedPage(t *testing.T) {
	tests := map[string]int{"": 1, "page=3": 3, "page=0": 1, "page=x": 1}
	for query, want := range tests {
		q, err := url.ParseQuery(query)
		if err != nil {
			t.Fatal(err)
		}
		if got := requestedPage(q); got != want {
			t.Errorf("requestedPage(%q) = %d, want %d", query, got, want)
		}
	}
}
