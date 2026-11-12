package gitoperationslib

import (
	"context"
)

// Teams reads and manages an organisation's teams, their members and
// repositories. Teams are addressed by organisation and name (case-insensitive
// on Forgejo, the slug on GitHub), the only addressing the forges share;
// Team.ID keeps Forgejo's ID. The reads (List, Get, Info, Members, Repos) work
// with whatever rights the token has (CapDirectory): Forgejo shows teams to
// organisation members only, GitHub hides secret teams from non-members. The
// rest is Forgejo's (CapAdmin); there each call looks the team up first.
type Teams interface {
	List(ctx context.Context, org string, opts ListOptions) (Page[Team], error)
	Get(ctx context.Context, org, name string) (*Team, error)
	// Info returns a team with its members and repositories.
	Info(ctx context.Context, org, name string) (*TeamInfo, error)
	Members(ctx context.Context, org, name string, opts ListOptions) (Page[User], error)
	Repos(ctx context.Context, org, name string, opts ListOptions) (Page[Repo], error)

	Create(ctx context.Context, org string, opts CreateTeamOptions) (*Team, error)
	// Edit changes a team. Renaming the owner team or changing its access
	// returns ErrOwnerTeam.
	Edit(ctx context.Context, org, name string, opts EditTeamOptions) (*Team, error)
	// Delete removes a team. Deleting the owner team returns ErrOwnerTeam.
	Delete(ctx context.Context, org, name string) error
	IsMember(ctx context.Context, org, name, user string) (bool, error)
	// AddMember adds user to the team, which also makes them a member of
	// the organisation. If the instance adds members by invitation, and user
	// is not in the organisation yet, it returns an error matching
	// ErrInvitationPending. The invitation is sent all the same.
	AddMember(ctx context.Context, org, name, user string) error
	RemoveMember(ctx context.Context, org, name, user string) error
	AddRepo(ctx context.Context, org, name, owner, repo string) error
	RemoveRepo(ctx context.Context, org, name, owner, repo string) error
}

// TeamInfo is a team with its members and repositories.
type TeamInfo struct {
	Team
	Members []User `json:"members"`
	Repos   []Repo `json:"repos"`
	// Incomplete is nil when every part was read; see UserInfo.Incomplete.
	Incomplete error       `json:"-"`
	Missing    []PartError `json:"missing,omitzero"` // see UserInfo.Missing
}

// TeamPermission is a team's access level, overall or for one unit.
type TeamPermission string

const (
	TeamPermissionNone  TeamPermission = "none"
	TeamPermissionRead  TeamPermission = "read"
	TeamPermissionWrite TeamPermission = "write"
	TeamPermissionAdmin TeamPermission = "admin"
	// TeamPermissionOwner is reported for an organisation's owner team; it
	// cannot be assigned.
	TeamPermissionOwner TeamPermission = "owner"
)

// TeamUnit is a repository feature a team can be granted access to.
type TeamUnit string

const (
	TeamUnitCode           TeamUnit = "repo.code"
	TeamUnitIssues         TeamUnit = "repo.issues"
	TeamUnitExternalIssues TeamUnit = "repo.ext_issues"
	TeamUnitWiki           TeamUnit = "repo.wiki"
	TeamUnitExternalWiki   TeamUnit = "repo.ext_wiki"
	TeamUnitPulls          TeamUnit = "repo.pulls"
	TeamUnitReleases       TeamUnit = "repo.releases"
	TeamUnitProjects       TeamUnit = "repo.projects"
	TeamUnitPackages       TeamUnit = "repo.packages"
	TeamUnitActions        TeamUnit = "repo.actions"
)

// AllTeamUnits lists every TeamUnit.
var AllTeamUnits = []TeamUnit{
	TeamUnitCode, TeamUnitIssues, TeamUnitExternalIssues, TeamUnitWiki, TeamUnitExternalWiki,
	TeamUnitPulls, TeamUnitReleases, TeamUnitProjects, TeamUnitPackages, TeamUnitActions,
}

// Team is a team within an organisation.
type Team struct {
	ID   int64  `json:"id"`
	Org  string `json:"org"`
	Name string `json:"name"`
	// Slug is the URL-safe name GitHub addresses teams by; on Forgejo it is
	// the name.
	Slug        string `json:"slug"`
	Description string `json:"description"`
	// Permission is the overall level: admin or owner when the team has
	// that level on every unit, otherwise derived by the server from Units.
	Permission TeamPermission              `json:"permission"`
	Units      map[TeamUnit]TeamPermission `json:"units,omitzero"`
	// IncludesAllRepos gives the team access to every repository of the
	// organisation, current and future.
	IncludesAllRepos bool `json:"includesAllRepos,omitzero"`
	CanCreateOrgRepo bool `json:"canCreateOrgRepo,omitzero"`
}

// TeamAccess sets a team's permissions. Exactly one form applies:
//
//   - Permission admin: admin on every unit; Units must be empty.
//   - Units set: per-unit permissions (none, read or write); Permission
//     must be empty.
//   - Permission read or write with no Units: that level on every unit.
type TeamAccess struct {
	Permission TeamPermission              `json:"permission,omitzero"`
	Units      map[TeamUnit]TeamPermission `json:"units,omitzero"`
}

// CreateTeamOptions describes a team to create.
type CreateTeamOptions struct {
	Name             string     `json:"name"` // required
	Description      string     `json:"description,omitzero"`
	Access           TeamAccess `json:"access"` // required
	IncludesAllRepos bool       `json:"includesAllRepos,omitzero"`
	CanCreateOrgRepo bool       `json:"canCreateOrgRepo,omitzero"`
}

// EditTeamOptions changes a team. Nil fields are left unchanged.
type EditTeamOptions struct {
	Name             *string     `json:"name,omitzero"`
	Description      *string     `json:"description,omitzero"`
	Access           *TeamAccess `json:"access,omitzero"`
	IncludesAllRepos *bool       `json:"includesAllRepos,omitzero"`
	CanCreateOrgRepo *bool       `json:"canCreateOrgRepo,omitzero"`
}
