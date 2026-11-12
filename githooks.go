package gitoperationslib

import (
	"context"
)

// GitHooks manages the git hooks of a repository: scripts the server runs when
// it receives a push, for instance to refuse it. Forgejo only (CapGitHooks).
//
// A hook runs on the server with the server's rights, so editing one amounts to
// running code there. Forgejo therefore refuses all of these with HTTP 403, even
// for a site admin, unless the instance sets DISABLE_GIT_HOOKS=false, and then
// allows only site admins and users given that right.
type GitHooks interface {
	// List returns the hooks a repository can have, in use or not:
	// pre-receive, update and post-receive. There are no pages.
	List(ctx context.Context, owner, repo string) ([]GitHook, error)
	// Get is HTTP 404 for a name that is not a git hook, and returns one
	// that is not in use with Active false.
	Get(ctx context.Context, owner, repo, name string) (*GitHook, error)
	// Set writes a hook's script, which puts it in use at once. A script
	// that exits with an error can refuse every push, the server's own included.
	Set(ctx context.Context, owner, repo, name, content string) (*GitHook, error)
	// Delete takes a hook out of use. It is not an error if it was not in
	// use.
	Delete(ctx context.Context, owner, repo, name string) error
}

// GitHook is a server-side git hook.
type GitHook struct {
	Name    string `json:"name"`
	Active  bool   `json:"active"`
	Content string `json:"content,omitzero"` // empty when not Active
}
