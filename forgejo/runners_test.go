package forgejo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func TestRunnersPath(t *testing.T) {
	for scope, want := range map[gitoperationslib.RunnerScope]string{
		gitoperationslib.RepoRunners("acme", "widget"): "repos/acme/widget/actions/runners/7",
		gitoperationslib.OrgRunners("acme"):            "orgs/acme/actions/runners/7",
		gitoperationslib.UserRunners():                 "user/actions/runners/7",
		gitoperationslib.InstanceRunners():             "admin/actions/runners/7",
	} {
		if got, err := runnersPath(scope, "7"); err != nil || got != want {
			t.Errorf("%+v: %q, %v", scope, got, err)
		}
	}
	if _, err := runnersPath(gitoperationslib.RunnerScope{Repo: "widget"}, "7"); err == nil {
		t.Error("a repository without owner accepted")
	}
	if _, err := runnersPath(gitoperationslib.RunnerScope{Owner: "acme", Instance: true}, "7"); err == nil {
		t.Error("an instance scope with an owner accepted")
	}
}

func TestRunners(t *testing.T) {
	var requests []string
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		switch {
		case r.Method == http.MethodPost:
			fmt.Fprint(w, `{"id":3,"uuid":"u-3","token":"secret"}`)
		case strings.HasSuffix(r.URL.Path, "registration-token"):
			fmt.Fprint(w, `{"token":"reg"}`)
		case strings.HasSuffix(r.URL.Path, "/runners"):
			fmt.Fprint(w, `[{"id":1,"name":"a","status":"idle","labels":["self"]},{"id":2,"name":"b","status":"active"},{"id":3,"name":"c","status":"offline"}]`)
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			fmt.Fprint(w, `{"id":1,"name":"a","status":"idle"}`)
		}
	})
	ctx := context.Background()
	scope := gitoperationslib.RepoRunners("acme", "widget")
	page, err := c.CI().Runners().List(ctx, scope, gitoperationslib.ListRunnersOptions{Inherited: true})
	if err != nil || len(page.Items) != 3 {
		t.Fatalf("List = %+v, %v", page, err)
	}
	if a, b, off := page.Items[0], page.Items[1], page.Items[2]; !a.Online || a.Busy || !b.Online || !b.Busy || off.Online || off.Busy {
		t.Errorf("statuses = %+v", page.Items)
	}
	reg, err := c.CI().Runners().Register(ctx, scope, gitoperationslib.RegisterRunnerOptions{Name: "c", Ephemeral: true})
	if err != nil || reg.ID != 3 || reg.UUID != "u-3" || reg.Token != "secret" {
		t.Errorf("Register = %+v, %v", reg, err)
	}
	if _, err := c.CI().Runners().Register(ctx, scope, gitoperationslib.RegisterRunnerOptions{Name: "c", Labels: []string{"x"}}); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("labels: %v", err)
	}
	if tok, err := c.CI().Runners().RegistrationToken(ctx, scope); err != nil || tok.Token != "reg" || !tok.ExpiresAt.IsZero() {
		t.Errorf("token = %+v, %v", tok, err)
	}
	if err := c.CI().Runners().Delete(ctx, scope, 3); err != nil {
		t.Error(err)
	}
	if requests[0] != "GET /api/v1/repos/acme/widget/actions/runners?visible=true" {
		t.Errorf("list request = %s", requests[0])
	}
}

// followServer answers like 17.0.1: 500 while the job has no log, then the parts
// asked for by range, 416 when there is nothing new.
func followServer(t *testing.T, chunks []string, endAfter int) *Client {
	t.Helper()
	polls := 0
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		log := strings.Join(chunks[:min(polls, len(chunks))], "")
		if strings.HasSuffix(r.URL.Path, "/logs") {
			if polls == 1 {
				w.WriteHeader(http.StatusNotFound)
				io.WriteString(w, `{"message":"job has not been executed yet"}`)
				return
			}
			if polls == 2 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			var from int
			fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-", &from)
			if from >= len(log) {
				w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
				return
			}
			w.WriteHeader(http.StatusPartialContent)
			io.WriteString(w, log[from:])
			return
		}
		polls++
		status := "running"
		if polls > endAfter {
			status = "success"
		}
		fmt.Fprintf(w, `{"id":9,"status":%q}`, status)
	})
	c.version = Version{17, 0, 1}
	return c
}

func TestFollowWorkflowJobLog(t *testing.T) {
	c := followServer(t, []string{"", "", "", "one\n", "two\n", "two\n", "three\n"}, 6)
	var out bytes.Buffer
	err := c.CI().Runs().FollowJobLog(context.Background(), "a", "b", 9, &out, gitoperationslib.FollowLogOptions{Interval: time.Millisecond})
	if err != nil || out.String() != "one\ntwo\ntwo\nthree\n" {
		t.Errorf("followed %q, %v", out.String(), err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	c = followServer(t, []string{"x\n"}, 1<<30)
	if err := c.CI().Runs().FollowJobLog(ctx, "a", "b", 9, io.Discard, gitoperationslib.FollowLogOptions{Interval: time.Millisecond}); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("a job that never ends: %v", err)
	}

	c.version = Version{16, 0, 7}
	if err := c.CI().Runs().FollowJobLog(context.Background(), "a", "b", 9, io.Discard, gitoperationslib.FollowLogOptions{}); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("before 17: %v", err)
	}
}
