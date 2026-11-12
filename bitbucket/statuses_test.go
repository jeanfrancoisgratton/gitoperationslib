package bitbucket

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func TestCommitStatusBitbucket(t *testing.T) {
	sha := strings.Repeat("a", 40)
	var posted string
	c := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		const base = "/2.0/repositories/acme/widget/"
		switch r.Method + " " + r.URL.Path {
		case "GET " + base + "refs/branches/main":
			fmt.Fprintf(w, `{"target":{"hash":%q}}`, sha)
		case "GET " + base + "commit/" + sha + "/statuses":
			fmt.Fprint(w, `{"values":[{"key":"build","state":"SUCCESSFUL","url":"https://ci/1"},{"key":"lint","state":"STOPPED"}]}`)
		case "POST " + base + "commit/" + sha + "/statuses/build":
			b, _ := io.ReadAll(r.Body)
			posted = string(b)
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
		}
	})
	ctx := context.Background()
	cs, err := c.CommitStatus(ctx, "acme", "widget", "main")
	if err != nil || cs.SHA != sha || cs.State != gitoperationslib.StatusError || len(cs.Statuses) != 2 || cs.Statuses[0].TargetURL != "https://ci/1" {
		t.Errorf("%+v %v", cs, err)
	}
	err = c.SetCommitStatus(ctx, "acme", "widget", sha, gitoperationslib.CommitStatus{Context: "ci", State: gitoperationslib.StatusPending, TargetURL: "https://ci/2"})
	if err != nil || !strings.Contains(posted, `"state":"INPROGRESS"`) || !strings.Contains(posted, `"key":"ci"`) {
		t.Errorf("posted %s, %v", posted, err)
	}
	if err := c.SetCommitStatus(ctx, "acme", "widget", sha, gitoperationslib.CommitStatus{Context: "ci", State: gitoperationslib.StatusWarning, TargetURL: "u"}); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("warning: %v", err)
	}
	if err := c.SetCommitStatus(ctx, "acme", "widget", sha, gitoperationslib.CommitStatus{Context: "ci", State: gitoperationslib.StatusSuccess}); err == nil {
		t.Error("no target URL accepted")
	}
}
