package bitbucket

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/bitbucketapi"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/unsupported"
)

var _ gitoperationslib.Collaborators = collaborators{}

// Collaborators implements gitoperationslib.Client. Bitbucket gives access at
// once: there are no invitations to list.
func (c *Client) Collaborators() gitoperationslib.Collaborators {
	return collaborators{c, unsupported.Collaborators{Backend: gitoperationslib.BackendBitbucket, Reason: "Bitbucket gives access at once"}}
}

type collaborators struct {
	c *Client
	unsupported.Collaborators
}

type userPermissionJSON struct {
	Permission string   `json:"permission"`
	User       userJSON `json:"user"`
}

func (j *userPermissionJSON) toCollaborator() gitoperationslib.Collaborator {
	return gitoperationslib.Collaborator{
		Login: j.User.login(), AccountID: j.User.AccountID, Permission: gitoperationslib.CollaboratorPermission(j.Permission),
	}
}

// collaboratorPath is the permission of user, who is only looked up in the
// workspace when given by nickname: an account ID or UUID can be an outsider.
func (c *Client) collaboratorPath(ctx context.Context, owner, repo, user string) (string, error) {
	if user == "" {
		return "", errors.New("bitbucket: repos.collaborator: user is required")
	}
	if !strings.HasPrefix(user, "{") && !strings.Contains(user, ":") {
		var err error
		if user, err = c.resolveUser(ctx, owner, user); err != nil {
			return "", err
		}
	}
	return repoPath(owner, repo, "permissions-config", "users", user), nil
}

// List implements gitoperationslib.Collaborators.
func (svc collaborators) List(ctx context.Context, owner, repo string, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Collaborator], error) {
	c := svc.c
	values, next, err := bitbucketapi.ListPaged[userPermissionJSON](ctx, c.api, repoPath(owner, repo, "permissions-config", "users"), bitbucketapi.ListQuery(opts))
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Collaborator]{}, err
	}
	out := gitoperationslib.Page[gitoperationslib.Collaborator]{Items: make([]gitoperationslib.Collaborator, len(values)), NextPage: next}
	for i := range values {
		out.Items[i] = values[i].toCollaborator()
	}
	return out, nil
}

// Get implements gitoperationslib.Collaborators. Bitbucket answers
// HTTP 200 with the permission none for a user without access (checked
// 2026-11-05).
func (svc collaborators) Get(ctx context.Context, owner, repo, user string) (*gitoperationslib.Collaborator, error) {
	c := svc.c
	path, err := c.collaboratorPath(ctx, owner, repo, user)
	if err != nil {
		return nil, err
	}
	var out userPermissionJSON
	if _, err := c.api.Do(ctx, http.MethodGet, path, nil, nil, &out); err != nil {
		return nil, err
	}
	if out.Permission == "none" {
		return nil, &gitoperationslib.APIError{
			Backend: gitoperationslib.BackendBitbucket, StatusCode: http.StatusNotFound,
			Message: fmt.Sprintf("%s is not a collaborator of %s/%s", user, owner, repo),
		}
	}
	collaborator := out.toCollaborator()
	return &collaborator, nil
}

// Add implements gitoperationslib.Collaborators.
func (svc collaborators) Add(ctx context.Context, owner, repo, user string, permission gitoperationslib.CollaboratorPermission) (bool, error) {
	c := svc.c
	switch permission {
	case gitoperationslib.CollaboratorRead, gitoperationslib.CollaboratorWrite, gitoperationslib.CollaboratorAdmin:
	case gitoperationslib.CollaboratorTriage, gitoperationslib.CollaboratorMaintain:
		return false, notSupported("repos.add_collaborator", "the "+string(permission)+" permission")
	default:
		return false, fmt.Errorf("bitbucket: repos.add_collaborator: invalid permission %q", permission)
	}
	path, err := c.collaboratorPath(ctx, owner, repo, user)
	if err != nil {
		return false, err
	}
	body := map[string]gitoperationslib.CollaboratorPermission{"permission": permission}
	_, err = c.api.Do(ctx, http.MethodPut, path, nil, body, nil)
	return false, err
}

// Remove implements gitoperationslib.Collaborators.
func (svc collaborators) Remove(ctx context.Context, owner, repo, user string) error {
	c := svc.c
	path, err := c.collaboratorPath(ctx, owner, repo, user)
	if err != nil {
		return err
	}
	_, err = c.api.Do(ctx, http.MethodDelete, path, nil, nil, nil)
	return err
}
