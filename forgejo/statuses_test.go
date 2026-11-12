package forgejo

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func TestCommitStatusForgejo(t *testing.T) {
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path + "?" + r.URL.Query().Get("sha") + r.URL.Query().Get("page") {
		case "/api/v1/repos/acme/widget/commits/main/status?1":
			fmt.Fprint(w, `{"state":"failure","sha":"abc","total_count":2,"statuses":[{"status":"failure","context":"a"}]}`)
		case "/api/v1/repos/acme/widget/commits/main/status?2":
			fmt.Fprint(w, `{"state":"failure","sha":"abc","total_count":2,"statuses":[{"status":"success","context":"b"}]}`)
		case "/api/v1/repos/acme/widget/commits/none/status?1", "/api/v1/repos/acme/widget/commits/gone/status?1":
			fmt.Fprint(w, `{"state":"","sha":"","total_count":0,"statuses":null}`) // Forgejo's answer to both
		case "/api/v1/repos/acme/widget/commits?none":
			fmt.Fprint(w, `[{"sha":"def"}]`)
		case "/api/v1/repos/acme/widget/commits?gone":
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `null`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
		}
	})
	ctx := context.Background()
	cs, err := c.CommitStatus(ctx, "acme", "widget", "main")
	if err != nil || cs.State != gitoperationslib.StatusFailure || len(cs.Statuses) != 2 || cs.SHA != "abc" {
		t.Errorf("paged: %+v %v", cs, err)
	}
	if cs, err = c.CommitStatus(ctx, "acme", "widget", "none"); err != nil || cs.State != "" || cs.SHA != "def" {
		t.Errorf("none: %+v %v", cs, err)
	}
	if _, err = c.CommitStatus(ctx, "acme", "widget", "gone"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("gone: %v", err)
	}
}
