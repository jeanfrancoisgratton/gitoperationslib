package forgejo

import (
	"context"
	"errors"
	"net/http"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

var _ gitoperationslib.GitHooks = gitHooks{}

// GitHooks implements gitoperationslib.Client.
func (c *Client) GitHooks() gitoperationslib.GitHooks { return gitHooks{c} }

type gitHooks struct{ c *Client }

type gitHookJSON struct {
	Name     string `json:"name"`
	IsActive bool   `json:"is_active"`
	Content  string `json:"content"`
}

func (j *gitHookJSON) toGitHook() *gitoperationslib.GitHook {
	return &gitoperationslib.GitHook{Name: j.Name, Active: j.IsActive, Content: j.Content}
}

// List implements gitoperationslib.GitHooks.
func (svc gitHooks) List(ctx context.Context, owner, repo string) ([]gitoperationslib.GitHook, error) {
	c := svc.c
	var out []gitHookJSON
	if _, err := c.api.Do(ctx, http.MethodGet, repoPath(owner, repo, "hooks", "git"), nil, nil, &out); err != nil {
		return nil, err
	}
	hooks := make([]gitoperationslib.GitHook, len(out))
	for i := range out {
		hooks[i] = *out[i].toGitHook()
	}
	return hooks, nil
}

// Get implements gitoperationslib.GitHooks.
func (svc gitHooks) Get(ctx context.Context, owner, repo, name string) (*gitoperationslib.GitHook, error) {
	c := svc.c
	if name == "" {
		return nil, errors.New("forgejo: git_hooks.get: name is required")
	}
	var out gitHookJSON
	if _, err := c.api.Do(ctx, http.MethodGet, repoPath(owner, repo, "hooks", "git", name), nil, nil, &out); err != nil {
		return nil, err
	}
	return out.toGitHook(), nil
}

// Set implements gitoperationslib.GitHooks. Empty content is refused: the
// server would take it for a request to delete the hook, and answer with success
// (checked on 15.0.11).
func (svc gitHooks) Set(ctx context.Context, owner, repo, name, content string) (*gitoperationslib.GitHook, error) {
	c := svc.c
	switch {
	case name == "":
		return nil, errors.New("forgejo: git_hooks.set: name is required")
	case content == "":
		return nil, errors.New("forgejo: git_hooks.set: content is required; to remove a hook, use DeleteGitHook")
	}
	var out gitHookJSON
	body := map[string]string{"content": content}
	if _, err := c.api.Do(ctx, http.MethodPatch, repoPath(owner, repo, "hooks", "git", name), nil, body, &out); err != nil {
		return nil, err
	}
	return out.toGitHook(), nil
}

// Delete implements gitoperationslib.GitHooks.
func (svc gitHooks) Delete(ctx context.Context, owner, repo, name string) error {
	c := svc.c
	if name == "" {
		return errors.New("forgejo: git_hooks.delete: name is required")
	}
	_, err := c.api.Do(ctx, http.MethodDelete, repoPath(owner, repo, "hooks", "git", name), nil, nil, nil)
	return err
}
