package github

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func TestCommitStatusGitHub(t *testing.T) {
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/commits/main/status"):
			fmt.Fprint(w, `{"state":"success","sha":"abc","total_count":1,"statuses":[{"state":"success","context":"ci/x","target_url":"https://ci/1"}]}`)
		case strings.HasSuffix(r.URL.Path, "/commits/main/check-runs"):
			fmt.Fprint(w, `{"total_count":2,"check_runs":[
				{"name":"build","status":"completed","conclusion":"success","head_sha":"abc","html_url":"https://gh/run/1"},
				{"name":"test","status":"in_progress","head_sha":"abc","output":{"title":"running"}}]}`)
		case strings.HasSuffix(r.URL.Path, "/commits/empty/status"):
			fmt.Fprint(w, `{"state":"pending","sha":"def","total_count":0,"statuses":[]}`) // GitHub says pending for none
		case strings.HasSuffix(r.URL.Path, "/commits/empty/check-runs"):
			fmt.Fprint(w, `{"total_count":0,"check_runs":[]}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
		}
	})
	cs, err := c.Commits().Status(context.Background(), "acme", "widget", "main")
	if err != nil {
		t.Fatal(err)
	}
	if cs.SHA != "abc" || cs.State != gitoperationslib.StatusPending || len(cs.Statuses) != 3 {
		t.Fatalf("%+v", cs)
	}
	if s := cs.Statuses[2]; !s.Check || s.Context != "test" || s.State != gitoperationslib.StatusPending || s.Description != "running" {
		t.Errorf("check run %+v", s)
	}
	if cs, err = c.Commits().Status(context.Background(), "acme", "widget", "empty"); err != nil || cs.State != "" || cs.SHA != "def" {
		t.Errorf("none: %+v %v", cs, err)
	}
}

func TestCheckState(t *testing.T) {
	for in, want := range map[[2]string]gitoperationslib.StatusState{
		{"queued", ""}: gitoperationslib.StatusPending, {"completed", "skipped"}: gitoperationslib.StatusSuccess,
		{"completed", "cancelled"}: gitoperationslib.StatusError, {"completed", "timed_out"}: gitoperationslib.StatusFailure,
	} {
		if got := checkState(in[0], in[1]); got != want {
			t.Errorf("%v: %s, want %s", in, got, want)
		}
	}
}
