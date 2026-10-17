package gitoperationslib

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
	ID          int64
	Org         string
	Name        string
	Description string
	// Permission is the overall level: admin or owner when the team has
	// that level on every unit, otherwise derived by the server from Units.
	Permission TeamPermission
	Units      map[TeamUnit]TeamPermission
	// IncludesAllRepos gives the team access to every repository of the
	// organisation, current and future.
	IncludesAllRepos bool
	CanCreateOrgRepo bool
}

// TeamAccess sets a team's permissions. Exactly one form applies:
//
//   - Permission admin: admin on every unit; Units must be empty.
//   - Units set: per-unit permissions (none, read or write); Permission
//     must be empty.
//   - Permission read or write with no Units: that level on every unit.
type TeamAccess struct {
	Permission TeamPermission
	Units      map[TeamUnit]TeamPermission
}

// CreateTeamOptions describes a team to create.
type CreateTeamOptions struct {
	Name             string // required
	Description      string
	Access           TeamAccess // required
	IncludesAllRepos bool
	CanCreateOrgRepo bool
}

// EditTeamOptions changes a team. Nil fields are left unchanged.
type EditTeamOptions struct {
	Name             *string
	Description      *string
	Access           *TeamAccess
	IncludesAllRepos *bool
	CanCreateOrgRepo *bool
}
