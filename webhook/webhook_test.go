package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// fixture is a delivery captured from a real server (see testdata/*.json).
type fixture struct {
	Secret  string            `json:"secret"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

func load(t *testing.T, name string) (fixture, *http.Request) {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var f fixture
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/hook", strings.NewReader(f.Body))
	for k, v := range f.Headers {
		r.Header.Set(k, v)
	}
	return f, r
}

func TestParseFixtures(t *testing.T) {
	tests := []struct {
		fixture, name, action, repo string
		forge                       gitoperationslib.Backend
	}{
		{"forgejo-push", "push", "", "gitopslib-admin/probe-hook", gitoperationslib.BackendForgejo},
		{"forgejo-pull_request", "pull_request", "opened", "gitopslib-admin/probe-hook", gitoperationslib.BackendForgejo},
		{"forgejo-release", "release", "published", "gitopslib-admin/probe-hook", gitoperationslib.BackendForgejo},
		{"forgejo-create", "create", "", "gitopslib-admin/probe-hook", gitoperationslib.BackendForgejo},
		{"github-ping", "ping", "", "jeanfrancoisgratton/gitopslib-it-hookprobe", gitoperationslib.BackendGitHub},
		{"github-push", "push", "", "jeanfrancoisgratton/gitopslib-it-hookprobe", gitoperationslib.BackendGitHub},
		{"github-release", "release", "published", "jeanfrancoisgratton/gitopslib-it-hookprobe", gitoperationslib.BackendGitHub},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			f, r := load(t, tt.fixture)
			ev, err := Parse(r, []byte(f.Secret))
			if err != nil {
				t.Fatal(err)
			}
			if ev.Forge != tt.forge || ev.Name != tt.name || ev.Action != tt.action || ev.Repo != tt.repo ||
				ev.Delivery == "" || ev.Sender == "" || len(ev.Payload) == 0 {
				t.Errorf("event = %+v", ev)
			}
		})
	}
}

func TestVerify(t *testing.T) {
	f, r := load(t, "forgejo-push")
	body := []byte(f.Body)

	if err := Verify(body, r.Header, []byte("wrong")); !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("wrong secret: %v", err)
	}
	tampered := bytes.Replace(body, []byte("refs/heads/main"), []byte("refs/heads/evil"), 1)
	if err := Verify(tampered, r.Header, []byte(f.Secret)); !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("tampered body: %v", err)
	}
	if err := Verify(body, r.Header, nil); err == nil {
		t.Error("empty secret accepted")
	}

	// Forgejo's own header is used when X-Hub-Signature-256 is absent.
	h := r.Header.Clone()
	h.Del("X-Hub-Signature-256")
	if err := Verify(body, h, []byte(f.Secret)); err != nil {
		t.Errorf("X-Forgejo-Signature only: %v", err)
	}
	h.Del("X-Forgejo-Signature")
	if err := Verify(body, h, []byte(f.Secret)); !errors.Is(err, ErrMissingSignature) {
		t.Errorf("unsigned: %v", err)
	}

	for _, bad := range []string{"sha1=abcd", "sha256=not-hex", "sha256="} {
		h := http.Header{}
		h.Set("X-Hub-Signature-256", bad)
		if err := Verify(body, h, []byte(f.Secret)); !errors.Is(err, ErrInvalidSignature) {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

func TestDecodeUnknownForge(t *testing.T) {
	h := http.Header{}
	h.Set("X-Gitea-Event", "push") // Gitea is not supported
	h.Set("X-GitHub-Event", "push")
	if _, err := Decode(h, []byte("{}")); !errors.Is(err, ErrUnknownForge) {
		t.Errorf("Gitea delivery: %v", err)
	}
	if _, err := Decode(http.Header{}, []byte("{}")); !errors.Is(err, ErrUnknownForge) {
		t.Errorf("no headers: %v", err)
	}
}

func TestFormEncodedDelivery(t *testing.T) {
	f, r := load(t, "forgejo-release")
	h := r.Header.Clone()
	h.Set("Content-Type", "application/x-www-form-urlencoded")
	ev, err := Decode(h, []byte("payload="+url.QueryEscape(f.Body)))
	if err != nil {
		t.Fatal(err)
	}
	if ev.Action != "published" || string(ev.Payload) != f.Body {
		t.Errorf("event = %+v", ev)
	}
	if _, err := Decode(h, []byte("other=1")); err == nil {
		t.Error("form without payload accepted")
	}
}

func parseFixture(t *testing.T, name string) *Event {
	t.Helper()
	f, r := load(t, name)
	ev, err := Parse(r, []byte(f.Secret))
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func TestPush(t *testing.T) {
	for _, name := range []string{"forgejo-push", "github-push"} {
		t.Run(name, func(t *testing.T) {
			push, err := parseFixture(t, name).Push()
			if err != nil {
				t.Fatal(err)
			}
			if push.Branch() == "" || push.Tag() != "" || push.After == "" || push.Pusher == "" ||
				push.CompareURL == "" || len(push.Commits) == 0 || push.HeadCommit == nil {
				t.Fatalf("push = %+v", push)
			}
			c := push.Commits[0]
			if c.ID == "" || c.Message == "" || c.Author.Name == "" || c.Timestamp.IsZero() {
				t.Errorf("commit = %+v", c)
			}
		})
	}
	if _, err := parseFixture(t, "github-ping").Push(); err == nil {
		t.Error("Push() on a ping succeeded")
	}
	if p := (&PushEvent{Ref: "refs/tags/v1"}); p.Tag() != "v1" || p.Branch() != "" {
		t.Errorf("tag ref: Tag=%q Branch=%q", p.Tag(), p.Branch())
	}
}

func TestPullRequest(t *testing.T) {
	ev, err := parseFixture(t, "forgejo-pull_request").PullRequest()
	if err != nil {
		t.Fatal(err)
	}
	pr := ev.PullRequest
	if ev.Action != "opened" || ev.Number == 0 || pr.Number != ev.Number || pr.Head.Ref != "feat" || pr.Base.Ref != "main" ||
		pr.Head.SHA == "" || pr.Author != "gitopslib-admin" || pr.State != gitoperationslib.PRStateOpen || pr.HTMLURL == "" {
		t.Errorf("pull request event = %+v", ev)
	}
}

func TestRelease(t *testing.T) {
	for _, name := range []string{"forgejo-release", "github-release"} {
		t.Run(name, func(t *testing.T) {
			ev, err := parseFixture(t, name).Release()
			if err != nil {
				t.Fatal(err)
			}
			if ev.Action != "published" || ev.Release.TagName != "v1" || ev.Release.Draft || ev.Release.PublishedAt.IsZero() || ev.Release.Author == "" {
				t.Errorf("release event = %+v", ev)
			}
		})
	}
}

func TestHandler(t *testing.T) {
	f, _ := load(t, "github-push")
	var got *Event
	h := Handler([]byte(f.Secret), func(_ context.Context, ev *Event) error {
		got = ev
		if ev.Name == "ping" {
			return errors.New("boom")
		}
		return nil
	})
	serve := func(name string, mutate func(*http.Request)) int {
		_, r := load(t, name)
		if mutate != nil {
			mutate(r)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}

	if code := serve("github-push", nil); code != http.StatusNoContent || got == nil || got.Name != "push" {
		t.Errorf("valid delivery: %d, %+v", code, got)
	}
	if code := serve("github-push", func(r *http.Request) { r.Header.Set("X-Hub-Signature-256", "sha256=00") }); code != http.StatusUnauthorized {
		t.Errorf("bad signature: %d", code)
	}
	if code := serve("github-push", func(r *http.Request) { r.Header.Del("X-Hub-Signature-256"); r.Header.Del("X-Hub-Signature") }); code != http.StatusUnauthorized {
		t.Errorf("unsigned: %d", code)
	}
	if code := serve("github-push", func(r *http.Request) { r.Header.Del("X-GitHub-Event") }); code != http.StatusBadRequest {
		t.Errorf("unknown forge: %d", code)
	}
	if code := serve("github-push", func(r *http.Request) { r.Method = http.MethodGet }); code != http.StatusMethodNotAllowed {
		t.Errorf("GET: %d", code)
	}
	// The ping fixture is signed with the same secret; fn fails on it.
	if code := serve("github-ping", nil); code != http.StatusInternalServerError {
		t.Errorf("failing handler: %d", code)
	}
}

// The Bitbucket fixtures are synthetic (built from Bitbucket's documented
// payload format): no public endpoint was available to capture deliveries.
func TestBitbucketDeliveries(t *testing.T) {
	push := parseFixture(t, "bitbucket-push")
	if push.Forge != gitoperationslib.BackendBitbucket || push.Name != "push" || push.Key != "repo:push" ||
		push.Repo != "acme/widget" || push.Sender != "alice" || push.Delivery == "" {
		t.Errorf("push event = %+v", push)
	}
	p, err := push.Push()
	if err != nil {
		t.Fatal(err)
	}
	if p.Branch() != "main" || p.After != strings.Repeat("b", 40) || p.Before == "" || p.Pusher != "alice" || p.CompareURL == "" ||
		len(p.Commits) != 2 || p.Commits[0].ID != strings.Repeat("a", 40) || p.HeadCommit == nil || p.HeadCommit.ID != strings.Repeat("b", 40) {
		t.Fatalf("push = %+v", p)
	}
	if c := p.Commits[0]; c.Author.Name != "Alice Example" || c.Author.Email != "alice@example.invalid" || c.Author.Username != "alice" || c.Timestamp.IsZero() {
		t.Errorf("commit author = %+v", c)
	}
	if _, err := push.PullRequest(); err == nil {
		t.Error("PullRequest() on a push succeeded")
	}

	ev := parseFixture(t, "bitbucket-pullrequest")
	if ev.Name != "pull_request" || ev.Action != "closed" || ev.Key != "pullrequest:fulfilled" {
		t.Errorf("pull request event = %+v", ev)
	}
	pr, err := ev.PullRequest()
	if err != nil {
		t.Fatal(err)
	}
	if pr.Number != 7 || pr.Action != "closed" || !pr.PullRequest.Merged || pr.PullRequest.State != gitoperationslib.PRStateClosed ||
		pr.PullRequest.Head.Ref != "feature/sign-in" || pr.PullRequest.Base.Ref != "main" || pr.PullRequest.MergedBy != "alice" ||
		pr.PullRequest.MergeCommitSHA != strings.Repeat("c", 12) {
		t.Errorf("pull request = %+v", pr)
	}
	if _, err := ev.Release(); err == nil {
		t.Error("Release() on Bitbucket succeeded")
	}
}

func TestBitbucketSignature(t *testing.T) {
	f, r := load(t, "bitbucket-push")
	body := []byte(f.Body)
	if err := Verify(body, r.Header, []byte(f.Secret)); err != nil {
		t.Fatal(err)
	}
	if err := Verify(body, r.Header, []byte("wrong")); !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("wrong secret: %v", err)
	}
	// GitHub's legacy SHA-1 header alone is not accepted.
	h := http.Header{}
	h.Set("X-Hub-Signature", "sha1=0123")
	if err := Verify(body, h, []byte(f.Secret)); !errors.Is(err, ErrMissingSignature) {
		t.Errorf("sha1 only: %v", err)
	}
}
