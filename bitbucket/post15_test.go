package bitbucket

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func TestPullFilters(t *testing.T) {
	var q string
	c := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		q = r.URL.Query().Get("q")
		io.WriteString(w, `{"values":[]}`)
	})
	ctx := context.Background()
	if _, err := c.PullRequests().List(ctx, "acme", "widget", gitoperationslib.ListPROptions{Base: "main", Head: `a"b`}); err != nil {
		t.Fatal(err)
	}
	if want := `destination.branch.name = "main" AND source.branch.name = "a\"b"`; q != want {
		t.Errorf("q = %s", q)
	}
	if _, err := c.PullRequests().List(ctx, "acme", "widget", gitoperationslib.ListPROptions{Head: "fork:feature"}); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("user:branch: %v", err)
	}
}

func TestReviewCommentOnSeveralLines(t *testing.T) {
	var sent struct {
		Inline map[string]any `json:"inline"`
	}
	c := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&sent)
		io.WriteString(w, `{"id":3,"inline":{"path":"a.go","start_to":2,"to":4}}`)
	})
	got, err := c.PullRequests().CreateReviewComment(context.Background(), "acme", "widget", 1, gitoperationslib.CreateReviewCommentOptions{Path: "a.go", Body: "b", Line: 2, ExtraLines: 2})
	if err != nil {
		t.Fatal(err)
	}
	if sent.Inline["start_to"] != float64(2) || sent.Inline["to"] != float64(4) {
		t.Errorf("sent %v", sent.Inline)
	}
	if got.Line != 2 || got.ExtraLines != 2 {
		t.Errorf("comment = %+v", got)
	}
}
