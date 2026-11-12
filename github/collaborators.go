package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	gh "github.com/google/go-github/v93/github"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

var (
	_ gitoperationslib.Collaborators = collaborators{}
)

// Collaborators implements gitoperationslib.Client.
func (c *Client) Collaborators() gitoperationslib.Collaborators { return collaborators{c} }

type collaborators struct{ c *Client }

// collaboratorPermissions translates to the names GitHub's requests take, which
// are not those its responses give.
var collaboratorPermissions = map[gitoperationslib.CollaboratorPermission]string{
	gitoperationslib.CollaboratorRead:     "pull",
	gitoperationslib.CollaboratorTriage:   "triage",
	gitoperationslib.CollaboratorWrite:    "push",
	gitoperationslib.CollaboratorMaintain: "maintain",
	gitoperationslib.CollaboratorAdmin:    "admin",
}

// List implements gitoperationslib.Collaborators. It asks for
// direct collaborators: GitHub's default adds everyone with access through the
// organisation, whom the other forges do not list.
func (svc collaborators) List(ctx context.Context, owner, repo string, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Collaborator], error) {
	c := svc.c
	users, resp, err := c.gh.Repositories.ListCollaborators(ctx, owner, repo, &gh.ListCollaboratorsOptions{Affiliation: "direct", ListOptions: ghList(opts)})
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Collaborator]{}, convertError(err)
	}
	return toPage(users, resp, func(u *gh.User) gitoperationslib.Collaborator {
		return gitoperationslib.Collaborator{Login: u.GetLogin(), Permission: gitoperationslib.CollaboratorPermission(u.GetRoleName())}
	}), nil
}

// Get implements gitoperationslib.Collaborators. The permission
// endpoint alone answers for anybody, with none for a stranger.
func (svc collaborators) Get(ctx context.Context, owner, repo, user string) (*gitoperationslib.Collaborator, error) {
	c := svc.c
	if user == "" {
		return nil, errors.New("github: repos.get_collaborator: user is required")
	}
	is, _, err := c.gh.Repositories.IsCollaborator(ctx, owner, repo, user)
	if err != nil {
		return nil, convertError(err)
	}
	if !is {
		return nil, &gitoperationslib.APIError{
			Backend: gitoperationslib.BackendGitHub, StatusCode: http.StatusNotFound,
			Message: fmt.Sprintf("%s is not a collaborator of %s/%s", user, owner, repo),
		}
	}
	level, _, err := c.gh.Repositories.GetPermissionLevel(ctx, owner, repo, user)
	if err != nil {
		return nil, convertError(err)
	}
	return &gitoperationslib.Collaborator{Login: user, Permission: gitoperationslib.CollaboratorPermission(level.GetRoleName())}, nil
}

// Add implements gitoperationslib.Collaborators. GitHub answers
// HTTP 201 with an invitation, and 204 when the user got access at once.
func (svc collaborators) Add(ctx context.Context, owner, repo, user string, permission gitoperationslib.CollaboratorPermission) (bool, error) {
	c := svc.c
	if user == "" {
		return false, errors.New("github: repos.add_collaborator: user is required")
	}
	name, ok := collaboratorPermissions[permission]
	if !ok {
		return false, fmt.Errorf("github: repos.add_collaborator: invalid permission %q", permission)
	}
	_, resp, err := c.gh.Repositories.AddCollaborator(ctx, owner, repo, user, gh.AddCollaboratorRequest{Permission: gh.Ptr(name)})
	if err != nil {
		return false, convertError(err)
	}
	return resp.StatusCode == http.StatusCreated, nil
}

// ListInvitations implements gitoperationslib.Collaborators.
func (svc collaborators) ListInvitations(ctx context.Context, owner, repo string, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.CollaboratorInvitation], error) {
	c := svc.c
	lo := ghList(opts)
	list, resp, err := c.gh.Repositories.ListInvitations(ctx, owner, repo, &lo)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.CollaboratorInvitation]{}, convertError(err)
	}
	return toPage(list, resp, func(i *gh.RepositoryInvitation) gitoperationslib.CollaboratorInvitation {
		return gitoperationslib.CollaboratorInvitation{
			ID: i.GetID(), Login: i.GetInvitee().GetLogin(), Permission: gitoperationslib.CollaboratorPermission(i.GetPermissions()),
			Expired: i.GetExpired(), HTMLURL: i.GetHTMLURL(), CreatedAt: i.GetCreatedAt().Time,
		}
	}), nil
}

// Remove implements gitoperationslib.Collaborators. The removal does
// not touch an invitation, which the user could still accept afterwards.
func (svc collaborators) Remove(ctx context.Context, owner, repo, user string) error {
	c := svc.c
	if user == "" {
		return errors.New("github: repos.remove_collaborator: user is required")
	}
	if _, err := c.gh.Repositories.RemoveCollaborator(ctx, owner, repo, user); err != nil {
		return convertError(err)
	}
	opts := gh.ListOptions{PerPage: 100}
	for {
		invitations, resp, err := c.gh.Repositories.ListInvitations(ctx, owner, repo, &opts)
		if err != nil {
			return convertError(err)
		}
		for _, inv := range invitations {
			if strings.EqualFold(inv.GetInvitee().GetLogin(), user) {
				if _, err := c.gh.Repositories.DeleteInvitation(ctx, owner, repo, inv.GetID()); err != nil {
					return convertError(err)
				}
			}
		}
		if resp.NextPage == 0 {
			return nil
		}
		opts.Page = resp.NextPage
	}
}
