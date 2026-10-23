package gitoperationslib

import (
	"context"
	"time"
)

// Directory reads users, organisations and teams, with whatever rights the
// token has: a site admin sees everything, other users see what they could
// see in the web interface. Forgejo and GitHub implement it (CapDirectory);
// Bitbucket implements Me only and returns ErrNotSupported elsewhere.
//
// Teams are addressed by organisation and name (GitHub: the team slug), the
// only addressing both forges share. Account management (creating, editing,
// deleting) is in Admin.
type Directory interface {
	// Me returns the authenticated user as the credentials identify it, with
	// everything the token may read about the account: private profile
	// fields, e-mail addresses, memberships, repositories and keys. It is
	// the only Directory method Bitbucket implements.
	Me(ctx context.Context) (*CurrentUser, error)
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
	Orgs  []Org  `json:"orgs"`  // organisations the user belongs to
	Repos []Repo `json:"repos"` // repositories the user owns
	// Incomplete is nil when every part was read. Otherwise it describes the
	// parts the server refused (401, 403, 404) or the backend cannot list
	// (ErrNotSupported); those fields are left empty. It is not serialised:
	// Missing carries the same information as data.
	Incomplete error `json:"-"`
	// Missing lists the parts recorded in Incomplete, one entry each.
	Missing []PartError `json:"missing,omitzero"`
}

// PartError is a part of an info call that could not be read.
type PartError struct {
	Part    string `json:"part"`            // "organisations", "teams", "e-mail addresses"…
	Status  int    `json:"status,omitzero"` // HTTP status of the refusal; 0 when the backend cannot read the part
	Message string `json:"message"`
}

// CurrentUser is the authenticated user, as returned by Directory.Me. Fields
// a backend does not report are left empty.
type CurrentUser struct {
	User
	// LoginName is the name used to sign in on an external authentication
	// source (LDAP, OAuth…), and AuthSourceID that source; 0 for a local
	// account. Forgejo only.
	LoginName    string `json:"loginName,omitzero"`
	AuthSourceID int64  `json:"authSourceId,omitzero"`
	UUID         string `json:"uuid,omitzero"`      // Bitbucket only
	AccountID    string `json:"accountId,omitzero"` // Atlassian account ID; Bitbucket only
	// TwoFactorAuth reports whether two-factor authentication is enabled;
	// nil when the backend does not tell. GitHub and Bitbucket only.
	TwoFactorAuth *bool `json:"twoFactorAuth,omitzero"`
	// Type is the account type ("User", "Bot"…); GitHub only.
	Type              string    `json:"type,omitzero"`
	Plan              string    `json:"plan,omitzero"`              // GitHub only
	Hireable          bool      `json:"hireable,omitzero"`          // GitHub only
	TwitterUsername   string    `json:"twitterUsername,omitzero"`   // GitHub only
	PublicRepos       int       `json:"publicRepos,omitzero"`       // GitHub only
	TotalPrivateRepos int64     `json:"totalPrivateRepos,omitzero"` // GitHub only
	OwnedPrivateRepos int64     `json:"ownedPrivateRepos,omitzero"` // GitHub only
	PublicGists       int       `json:"publicGists,omitzero"`       // GitHub only
	PrivateGists      int       `json:"privateGists,omitzero"`      // GitHub only
	DiskUsage         int       `json:"diskUsage,omitzero"`         // in KiB; GitHub only
	Collaborators     int       `json:"collaborators,omitzero"`     // GitHub only
	UpdatedAt         time.Time `json:"updatedAt,omitzero"`         // GitHub only
	// Settings are the account preferences; Forgejo only.
	Settings *UserSettings `json:"settings,omitzero"`

	Emails []Email `json:"emails"`
	Orgs   []Org   `json:"orgs"` // organisations the user belongs to, private memberships included
	Teams  []Team  `json:"teams"`
	// Repos are the repositories the user owns or reaches as a collaborator
	// or organisation member, private ones included.
	Repos   []Repo   `json:"repos"`
	SSHKeys []SSHKey `json:"sshKeys"`
	GPGKeys []GPGKey `json:"gpgKeys"`
	// Incomplete is nil when every part was read; see UserInfo.Incomplete.
	// A token lacking a scope (GitHub's user:email, read:public_key…)
	// leaves the matching part empty and recorded here.
	Incomplete error       `json:"-"`
	Missing    []PartError `json:"missing,omitzero"` // see UserInfo.Missing
}

// OrgInfo is an organisation with the teams, members and repositories the
// caller can see.
type OrgInfo struct {
	Org
	Teams   []Team `json:"teams"`
	Members []User `json:"members"` // non-members only see public members
	Repos   []Repo `json:"repos"`
	// Incomplete is nil when every part was read; see UserInfo.Incomplete.
	Incomplete error       `json:"-"`
	Missing    []PartError `json:"missing,omitzero"` // see UserInfo.Missing
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
