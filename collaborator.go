package gitoperationslib

import "context"

// CollaboratorPermission is a user's access level on a repository.
type CollaboratorPermission string

const (
	CollaboratorRead  CollaboratorPermission = "read"
	CollaboratorWrite CollaboratorPermission = "write"
	CollaboratorAdmin CollaboratorPermission = "admin"
	// CollaboratorTriage and CollaboratorMaintain only exist on GitHub.
	CollaboratorTriage   CollaboratorPermission = "triage"
	CollaboratorMaintain CollaboratorPermission = "maintain"
)

// Valid reports whether p is one of the permissions above.
func (p CollaboratorPermission) Valid() bool {
	switch p {
	case CollaboratorRead, CollaboratorWrite, CollaboratorAdmin, CollaboratorTriage, CollaboratorMaintain:
		return true
	}
	return false
}

// Collaborator is a user with their own access to a repository.
type Collaborator struct {
	Login string `json:"login"`
	// AccountID is the Atlassian account ID, on Bitbucket only.
	AccountID string `json:"accountId,omitzero"`
	// Permission is the user's level. On GitHub it can also be the name of an
	// organisation's custom role. On Forgejo it is empty when the caller may
	// not read it: only repository admins can, except for their own.
	Permission CollaboratorPermission `json:"permission,omitzero"`
}

// Collaborators manages the users with their own access to a repository, which
// takes admin access to it. The Forgejo, GitHub and Bitbucket clients implement
// it (CapCollaborators); find it with a type assertion:
//
//	c, ok := client.(gitoperationslib.Collaborators)
//
// On Bitbucket, a user is a nickname of a member of the workspace, an account
// ID or a {UUID}. To add someone from outside the workspace, use one of the
// last two.
type Collaborators interface {
	// ListCollaborators lists those who were given access directly, not
	// through a team. GitHub and Bitbucket usually include the repository's
	// owner, Forgejo does not. Forgejo takes one more request per collaborator
	// to read their permission.
	ListCollaborators(ctx context.Context, owner, repo string, opts ListOptions) (Page[Collaborator], error)
	// GetCollaborator returns a collaborator; the error is HTTP 404 for
	// anybody else. On GitHub it also finds those with access through the
	// organisation, and it takes two requests on Forgejo and GitHub.
	GetCollaborator(ctx context.Context, owner, repo, user string) (*Collaborator, error)
	// AddCollaborator gives user access to the repository, or changes the
	// permission of an existing collaborator. invited is true when GitHub sent
	// an invitation instead: the user has no access, and is not listed, until
	// they accept it.
	//
	// Forgejo and Bitbucket only take read, write and admin, and return
	// ErrNotSupported for the others. GitHub ignores the permission on a
	// repository owned by a user, as opposed to an organisation.
	AddCollaborator(ctx context.Context, owner, repo, user string, permission CollaboratorPermission) (invited bool, err error)
	// RemoveCollaborator removes user's access; on GitHub it also withdraws a
	// pending invitation, which takes more requests. Removing an existing user
	// who has no access is not an error.
	RemoveCollaborator(ctx context.Context, owner, repo, user string) error
}
