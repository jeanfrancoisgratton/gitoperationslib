package forgejo

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func TestRepoFeatures(t *testing.T) {
	actions := true // what the server keeps
	var methods []string
	var patched map[string]any
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		if r.Method == http.MethodPatch {
			json.NewDecoder(r.Body).Decode(&patched)
			if v, ok := patched["has_actions"].(bool); ok && v {
				// The instance has Actions turned off: answers success, keeps false.
				actions = actions && false
			} else if ok {
				actions = v
			}
		}
		body, _ := json.Marshal(map[string]any{"name": "widget", "full_name": "me/widget", "owner": map[string]string{"login": "me"},
			"has_actions": actions, "has_issues": true, "has_wiki": true, "has_pull_requests": true, "has_projects": true, "has_releases": true, "has_packages": true})
		w.Write(body)
	})
	ctx := context.Background()
	off := gitoperationslib.Ptr(false)

	// Already as asked: no second request.
	repo, err := c.Repos().Create(ctx, gitoperationslib.CreateRepoOptions{Name: "widget", Features: gitoperationslib.RepoFeatures{Wiki: gitoperationslib.Ptr(true)}})
	if err != nil || len(methods) != 1 || repo.Features.Releases == nil || !*repo.Features.Releases {
		t.Errorf("Create = %+v, %v, requests %v", repo, err, methods)
	}
	methods = nil
	repo, err = c.Repos().Create(ctx, gitoperationslib.CreateRepoOptions{Name: "widget", Features: gitoperationslib.RepoFeatures{Actions: off}})
	if err != nil || len(methods) != 2 || patched["has_actions"] != false || *repo.Features.Actions {
		t.Errorf("Create(actions off) = %+v, %v, requests %v, patched %v", repo.Features, err, methods, patched)
	}
	repo, err = c.Repos().Edit(ctx, "me", "widget", gitoperationslib.EditRepoOptions{Features: gitoperationslib.RepoFeatures{Actions: gitoperationslib.Ptr(true)}})
	if !errors.Is(err, gitoperationslib.ErrNotSupported) || repo == nil {
		t.Errorf("a feature the server kept off: %+v, %v", repo, err)
	}
	methods = nil
	if _, err := c.Repos().Edit(ctx, "me", "widget", gitoperationslib.EditRepoOptions{Features: gitoperationslib.RepoFeatures{Discussions: off}}); !errors.Is(err, gitoperationslib.ErrNotSupported) || len(methods) != 0 {
		t.Errorf("discussions: %v, requests %v", err, methods)
	}
	io.Discard.Write(nil)
}
