package bitbucket

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// The areas Bitbucket lacks must answer ErrNotSupported from every method,
// without a request: a new method is checked as soon as it exists.
func TestUnsupportedAreasBitbucket(t *testing.T) {
	c := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL)
	})
	errorType := reflect.TypeFor[error]()
	for name, area := range map[string]any{
		"Releases": c.Releases(), "TagProtections": c.TagProtections(), "Issues": c.Issues(),
		"Labels": c.Labels(), "Milestones": c.Milestones(), "Users": c.Users(), "Orgs": c.Orgs(),
		"Teams": c.Teams(), "Mirrors": c.Mirrors(), "GitHooks": c.GitHooks(), "Runs": c.CI().Runs(),
		"Artifacts": c.CI().Artifacts(), "Runners": c.CI().Runners(),
	} {
		v := reflect.ValueOf(area)
		for i := range v.NumMethod() {
			method := v.Type().Method(i)
			if name == "Users" && method.Name == "Me" { // Bitbucket has this one
				continue
			}
			ft := v.Method(i).Type()
			args := make([]reflect.Value, ft.NumIn())
			for j := range args {
				args[j] = reflect.Zero(ft.In(j))
			}
			out := v.Method(i).Call(args)
			last := out[len(out)-1]
			if last.Kind() == reflect.Interface && !last.Type().Implements(errorType) {
				continue // an accessor, such as Orgs.Labels: its own methods are generated alike
			}
			if !last.Type().Implements(errorType) {
				t.Errorf("%s.%s does not return an error last", name, method.Name)
				continue
			}
			err, _ := last.Interface().(error)
			if !errors.Is(err, gitoperationslib.ErrNotSupported) {
				t.Errorf("%s.%s: err = %v, want ErrNotSupported", name, method.Name, err)
			}
		}
	}
}

func TestGPGVerifyNotSupportedBitbucket(t *testing.T) {
	c := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("unexpected request %s", r.URL) })
	if _, err := c.Account().GPGKeys().Verify(context.Background(), "k", "s"); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("err = %v", err)
	}
}

func TestClientBitbucket(t *testing.T) {
	var agents []string
	attempts := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		attempts++
		agents = append(agents, r.Header.Get("User-Agent"))
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, `{"error":{"message":"down"}}`)
	})
	srv := newServer(t, mux)
	c, err := New(Token("tok"), WithBaseURL(srv+"/2.0/"), WithUserAgent("gitclt/1"), WithRetry(gitoperationslib.NoRetry))
	if err != nil {
		t.Fatal(err)
	}
	if c.Backend() != gitoperationslib.BackendBitbucket {
		t.Errorf("Backend = %v", c.Backend())
	}
	caps := c.Capabilities()
	if !caps.Has(gitoperationslib.CapRepos|gitoperationslib.CapPullRequests|gitoperationslib.CapAccount) || caps.Has(gitoperationslib.CapIssues) || caps.Has(gitoperationslib.CapReleases) {
		t.Errorf("Capabilities = %s", caps)
	}

	_, err = c.Repos().Get(context.Background(), "acme", "widget")
	if gitoperationslib.StatusCode(err) != http.StatusServiceUnavailable {
		t.Errorf("err = %v, want the 503", err)
	}
	if attempts != 1 {
		t.Errorf("attempts = %d: NoRetry must not retry", attempts)
	}
	if fmt.Sprint(agents) != "[gitclt/1]" {
		t.Errorf("User-Agent = %v", agents)
	}
}
