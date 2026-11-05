package forgejo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

var _ gitoperationslib.Collaborators = (*Client)(nil)

// collaboratorError converts the error of a call naming a user. Forgejo answers
// HTTP 422 for an unknown user (checked on 15.0.11), the other forges 404.
func collaboratorError(err error) error {
	var apiErr *gitoperationslib.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnprocessableEntity && strings.Contains(apiErr.Message, "user does not exist") {
		apiErr.StatusCode = http.StatusNotFound
	}
	return err
}

// collaboratorPermission reads user's level. HTTP 403 means only the repository's
// admins may read it, and user is not the caller. It answers for anybody, not
// just collaborators: read on a public repository, and owner for an owner.
func (c *Client) collaboratorPermission(ctx context.Context, owner, repo, user string) (gitoperationslib.CollaboratorPermission, error) {
	var out struct {
		Permission gitoperationslib.CollaboratorPermission `json:"permission"`
	}
	if _, err := c.api.Do(ctx, http.MethodGet, repoPath(owner, repo, "collaborators", user, "permission"), nil, nil, &out); err != nil {
		return "", err
	}
	return out.Permission, nil
}

// ListCollaborators implements gitoperationslib.Collaborators. Forgejo lists no
// permissions, so each takes a request; the caller's being refused one means
// they are not an admin, and there is no point in asking for the others.
func (c *Client) ListCollaborators(ctx context.Context, owner, repo string, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Collaborator], error) {
	type user struct {
		Login string `json:"login"`
	}
	page, err := listAt(c, repoPath(owner, repo, "collaborators"), func(u *user) gitoperationslib.Collaborator {
		return gitoperationslib.Collaborator{Login: u.Login}
	})(ctx, opts)
	if err != nil {
		return page, err
	}
	for i := range page.Items {
		permission, err := c.collaboratorPermission(ctx, owner, repo, page.Items[i].Login)
		if gitoperationslib.StatusCode(err) == http.StatusForbidden {
			break
		}
		if err != nil {
			return gitoperationslib.Page[gitoperationslib.Collaborator]{}, err
		}
		page.Items[i].Permission = permission
	}
	return page, nil
}

// GetCollaborator implements gitoperationslib.Collaborators.
func (c *Client) GetCollaborator(ctx context.Context, owner, repo, user string) (*gitoperationslib.Collaborator, error) {
	if user == "" {
		return nil, errors.New("forgejo: repos.get_collaborator: user is required")
	}
	if _, err := c.api.Do(ctx, http.MethodGet, repoPath(owner, repo, "collaborators", user), nil, nil, nil); err != nil {
		return nil, collaboratorError(err)
	}
	out := &gitoperationslib.Collaborator{Login: user}
	permission, err := c.collaboratorPermission(ctx, owner, repo, user)
	if err != nil && gitoperationslib.StatusCode(err) != http.StatusForbidden {
		return nil, err
	}
	out.Permission = permission
	return out, nil
}

// AddCollaborator implements gitoperationslib.Collaborators. The permission is
// checked here because Forgejo takes any value, and stores write for those it
// does not know (checked on 15.0.11).
func (c *Client) AddCollaborator(ctx context.Context, owner, repo, user string, permission gitoperationslib.CollaboratorPermission) (bool, error) {
	if user == "" {
		return false, errors.New("forgejo: repos.add_collaborator: user is required")
	}
	switch permission {
	case gitoperationslib.CollaboratorRead, gitoperationslib.CollaboratorWrite, gitoperationslib.CollaboratorAdmin:
	case gitoperationslib.CollaboratorTriage, gitoperationslib.CollaboratorMaintain:
		return false, gitoperationslib.NotSupported(gitoperationslib.BackendForgejo, "repos.add_collaborator", "the "+string(permission)+" permission")
	default:
		return false, fmt.Errorf("forgejo: repos.add_collaborator: invalid permission %q", permission)
	}
	body := map[string]gitoperationslib.CollaboratorPermission{"permission": permission}
	_, err := c.api.Do(ctx, http.MethodPut, repoPath(owner, repo, "collaborators", user), nil, body, nil)
	return false, collaboratorError(err)
}

// RemoveCollaborator implements gitoperationslib.Collaborators.
func (c *Client) RemoveCollaborator(ctx context.Context, owner, repo, user string) error {
	if user == "" {
		return errors.New("forgejo: repos.remove_collaborator: user is required")
	}
	_, err := c.api.Do(ctx, http.MethodDelete, repoPath(owner, repo, "collaborators", user), nil, nil, nil)
	return collaboratorError(err)
}
