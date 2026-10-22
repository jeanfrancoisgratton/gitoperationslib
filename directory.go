package gitoperationslib

import "context"

// Directory reads users, organisations and teams, with whatever rights the
// token has: a site admin sees everything, other users see what they could
// see in the web interface. Forgejo and GitHub implement it (CapDirectory);
// Bitbucket returns ErrNotSupported.
//
// Teams are addressed by organisation and name (GitHub: the team slug), the
// only addressing both forges share. Account management (creating, editing,
// deleting) is in Admin.
type Directory interface {
	// User returns a user account. Fields the server hides from the caller
	// (e-mail, admin flags, last login…) are left empty.
	User(ctx context.Context, login string) (*User, error)
	// UserInfo returns a user with the organisations they belong to and the
	// repositories they own, as far as the caller can see them.
	UserInfo(ctx context.Context, login string) (*UserInfo, error)

	Org(ctx context.Context, name string) (*Org, error)
	// ListOrgs returns the organisations user belongs to; empty user means
	// the authenticated user (whose private memberships are included).
	ListOrgs(ctx context.Context, user string, opts ListOptions) (Page[Org], error)
	// ListAllOrgs returns every organisation visible to the caller. On
	// GitHub, NextPage is an opaque cursor rather than a page number: pass it
	// back unchanged (All does).
	ListAllOrgs(ctx context.Context, opts ListOptions) (Page[Org], error)
	// OrgInfo returns an organisation with its teams, members and
	// repositories, as far as the caller can see them.
	OrgInfo(ctx context.Context, name string) (*OrgInfo, error)
	// OrgMembers lists an organisation's members. Callers outside the
	// organisation see its public members only.
	OrgMembers(ctx context.Context, org string, opts ListOptions) (Page[User], error)

	// ListTeams lists an organisation's teams. Forgejo shows them to
	// organisation members only; GitHub hides secret teams from non-members.
	ListTeams(ctx context.Context, org string, opts ListOptions) (Page[Team], error)
	// Team returns the team of org called name (case-insensitive on
	// Forgejo; the slug on GitHub).
	Team(ctx context.Context, org, name string) (*Team, error)
	// TeamInfo returns a team with its members and repositories.
	TeamInfo(ctx context.Context, org, name string) (*TeamInfo, error)
	TeamMembers(ctx context.Context, org, name string, opts ListOptions) (Page[User], error)
	TeamRepos(ctx context.Context, org, name string, opts ListOptions) (Page[Repo], error)
}

// UserInfo is a user with the organisations and repositories the caller can
// see.
type UserInfo struct {
	User
	Orgs  []Org  // organisations the user belongs to
	Repos []Repo // repositories the user owns
	// Incomplete is nil when every part was read. Otherwise it describes the
	// parts the server refused (401, 403, 404) or the backend cannot list
	// (ErrNotSupported); those fields are left empty.
	Incomplete error
}

// OrgInfo is an organisation with the teams, members and repositories the
// caller can see.
type OrgInfo struct {
	Org
	Teams   []Team
	Members []User // non-members only see public members
	Repos   []Repo
	// Incomplete is nil when every part was read; see UserInfo.Incomplete.
	Incomplete error
}

// TeamInfo is a team with its members and repositories.
type TeamInfo struct {
	Team
	Members []User
	Repos   []Repo
	// Incomplete is nil when every part was read; see UserInfo.Incomplete.
	Incomplete error
}
