package github

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func TestRunnerScopes(t *testing.T) {
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("unexpected %s %s", r.Method, r.URL) })
	ctx := context.Background()
	for _, scope := range []gitoperationslib.RunnerScope{gitoperationslib.UserRunners(), gitoperationslib.InstanceRunners()} {
		if _, err := c.CI().Runners().List(ctx, scope, gitoperationslib.ListRunnersOptions{}); !errors.Is(err, gitoperationslib.ErrNotSupported) {
			t.Errorf("%+v: %v", scope, err)
		}
	}
	repo := gitoperationslib.RepoRunners("acme", "widget")
	if _, err := c.CI().Runners().List(ctx, repo, gitoperationslib.ListRunnersOptions{Inherited: true}); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("inherited: %v", err)
	}
	if _, err := c.CI().Runners().Register(ctx, repo, gitoperationslib.RegisterRunnerOptions{Name: "lasting"}); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("a lasting runner: %v", err)
	}
}

func TestRunnersGitHub(t *testing.T) {
	var sent string
	deleted := false
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "generate-jitconfig"):
			var buf bytes.Buffer
			buf.ReadFrom(r.Body)
			sent = buf.String()
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"runner":{"id":4,"name":"jit"},"encoded_jit_config":"abc"}`)
		case strings.HasSuffix(r.URL.Path, "registration-token"):
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"token":"reg","expires_at":"2030-01-01T00:00:00Z"}`)
		case r.Method == http.MethodDelete:
			deleted = true
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/runners/404"):
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		default:
			fmt.Fprint(w, `{"id":4,"name":"jit","status":"online","busy":true,"labels":[{"name":"self-hosted"}]}`)
		}
	})
	ctx := context.Background()
	repo := gitoperationslib.RepoRunners("acme", "widget")
	reg, err := c.CI().Runners().Register(ctx, repo, gitoperationslib.RegisterRunnerOptions{Name: "jit", Ephemeral: true})
	if err != nil || reg.ID != 4 || reg.JITConfig != "abc" || !strings.Contains(sent, `"labels":["self-hosted"]`) {
		t.Errorf("Register = %+v, %v, sent %s", reg, err, sent)
	}
	got, err := c.CI().Runners().Get(ctx, repo, 4)
	if err != nil || !got.Online || !got.Busy || got.Labels[0] != "self-hosted" {
		t.Errorf("Get = %+v, %v", got, err)
	}
	if tok, err := c.CI().Runners().RegistrationToken(ctx, repo); err != nil || tok.Token != "reg" || tok.ExpiresAt.IsZero() {
		t.Errorf("token = %+v, %v", tok, err)
	}
	// GitHub answers 204 for a runner that does not exist: the library asks first.
	if err := c.CI().Runners().Delete(ctx, repo, 404); !gitoperationslib.IsNotFound(err) || deleted {
		t.Errorf("Delete(missing) = %v, deleted %v", err, deleted)
	}
	if err := c.CI().Runners().Delete(ctx, repo, 4); err != nil || !deleted {
		t.Errorf("Delete = %v", err)
	}
}

func TestFollowWorkflowJobLogGitHub(t *testing.T) {
	polls := 0
	var logs string
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/stored-log"):
			fmt.Fprint(w, "the whole log\n")
		case strings.HasSuffix(r.URL.Path, "/logs"):
			// Stored a little after the end.
			if polls < 4 {
				polls++
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"message":"Not Found"}`)
				return
			}
			http.Redirect(w, r, "http://"+r.Host+"/stored-log", http.StatusFound)
		default:
			polls++
			status, conclusion := "in_progress", ""
			if polls > 2 {
				status, conclusion = "completed", "success"
			}
			fmt.Fprintf(w, `{"id":9,"status":%q,"conclusion":%q}`, status, conclusion)
		}
	})
	var out bytes.Buffer
	if err := c.CI().Runs().FollowJobLog(context.Background(), "a", "b", 9, &out, gitoperationslib.FollowLogOptions{Interval: time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	logs = out.String()
	if logs != "the whole log\n" {
		t.Errorf("log = %q", logs)
	}
}
