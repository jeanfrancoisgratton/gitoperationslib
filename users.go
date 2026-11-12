package gitoperationslib

import (
	"context"
	"time"
)

// UserFilter selects users. Nil fields do not filter.
type UserFilter struct {
	// TwoFactor selects users with, or without, two-factor authentication. It
	// needs Forgejo 16 (ErrNotSupported on older servers, which would ignore it
	// and return everyone).
	TwoFactor *bool `json:"twoFactor,omitzero"`
}

// Users reads and manages user accounts. Me, Get and Info read with whatever
// rights the token has (CapDirectory; Bitbucket has Me only): a site admin
// sees everything, other users what the web interface would show them. The
// other methods administer accounts, Forgejo only (CapAdmin), and need a
// site-admin token.
type Users interface {
	// Me returns the authenticated user as the credentials identify it, with
	// everything the token may read about the account: private profile
	// fields, e-mail addresses, memberships, repositories and keys.
	Me(ctx context.Context) (*CurrentUser, error)
	// Get returns a user account. Fields the server hides from the caller
	// (e-mail, admin flags, last login…) are left empty.
	Get(ctx context.Context, login string) (*User, error)
	// Info returns a user with the organisations they belong to and the
	// repositories they own, as far as the caller can see them.
	Info(ctx context.Context, login string) (*UserInfo, error)

	List(ctx context.Context, opts ListOptions) (Page[User], error)
	// Search lists users by criteria (CapUserFilters).
	Search(ctx context.Context, filter UserFilter, opts ListOptions) (Page[User], error)
	Create(ctx context.Context, opts CreateUserOptions) (*User, error)
	// Edit applies opts. If the server ignores a visibility change (Forgejo
	// 15.0.x cannot switch a user back to public), Edit returns the updated
	// user together with an error matching ErrNotSupported.
	Edit(ctx context.Context, login string, opts EditUserOptions) (*User, error)
	// Rename changes a user's login. The old login keeps redirecting to the
	// new one, so Get(oldLogin) returns the renamed user.
	Rename(ctx context.Context, login, newLogin string) error
	Delete(ctx context.Context, login string, opts DeleteUserOptions) error
	// Tokens manages the access tokens of the user called login with an
	// ordinary site-admin token, where Account().Tokens() needs the user's
	// password (CapAdminTokens; Forgejo 16 and later).
	Tokens(login string) Tokens
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

// CurrentUser is the authenticated user, as returned by Users().Me. Fields
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

// User is a user account.
type User struct {
	ID            int64           `json:"id"`
	Login         string          `json:"login"`
	FullName      string          `json:"fullName"`
	Email         string          `json:"email"`
	Description   string          `json:"description"`
	Website       string          `json:"website"`
	Location      string          `json:"location"`
	Visibility    OwnerVisibility `json:"visibility"`
	IsAdmin       bool            `json:"isAdmin"`
	Active        bool            `json:"active"`
	Restricted    bool            `json:"restricted,omitzero"`
	ProhibitLogin bool            `json:"prohibitLogin,omitzero"`
	HTMLURL       string          `json:"htmlUrl"`
	AvatarURL     string          `json:"avatarUrl"`
	Company       string          `json:"company,omitzero"`  // GitHub only
	Pronouns      string          `json:"pronouns,omitzero"` // Forgejo only
	Language      string          `json:"language,omitzero"` // interface language; Forgejo only
	Followers     int             `json:"followers"`
	Following     int             `json:"following"`
	StarredRepos  int             `json:"starredRepos,omitzero"` // Forgejo only
	CreatedAt     time.Time       `json:"createdAt,omitzero"`
	LastLogin     time.Time       `json:"lastLogin,omitzero"` // zero if the user never signed in
}

// UserSettings are a Forgejo user's account preferences.
type UserSettings struct {
	Theme               string `json:"theme"`
	DiffViewStyle       string `json:"diffViewStyle"` // "unified" or "split"
	HideEmail           bool   `json:"hideEmail"`
	HideActivity        bool   `json:"hideActivity"`
	HidePronouns        bool   `json:"hidePronouns"`
	EnableRepoUnitHints bool   `json:"enableRepoUnitHints"`
}

// Email is one of the authenticated user's e-mail addresses.
type Email struct {
	Address  string `json:"address"`
	Primary  bool   `json:"primary"`
	Verified bool   `json:"verified"`
	// Visibility is "public" or "private"; GitHub only, and only for the
	// primary address.
	Visibility string `json:"visibility,omitzero"`
}

// SSHKey is a public SSH key of the authenticated user.
type SSHKey struct {
	ID          int64     `json:"id,omitzero"` // 0 on Bitbucket
	Title       string    `json:"title"`
	Key         string    `json:"key"`
	Fingerprint string    `json:"fingerprint,omitzero"` // not reported by GitHub
	ReadOnly    bool      `json:"readOnly,omitzero"`    // GitHub only
	CreatedAt   time.Time `json:"createdAt,omitzero"`
	UpdatedAt   time.Time `json:"updatedAt,omitzero"` // Forgejo 16 and later
	LastUsed    time.Time `json:"lastUsed,omitzero"`  // GitHub and Bitbucket only; zero if never used
	// Verified tells whether the key's owner proved they hold it (Forgejo 16
	// and later, GitHub).
	Verified bool `json:"verified,omitzero"`
}

// GPGKey is a GPG key of the authenticated user.
type GPGKey struct {
	ID                int64         `json:"id"`
	KeyID             string        `json:"keyId"`
	PublicKey         string        `json:"publicKey"`
	Emails            []GPGKeyEmail `json:"emails"`
	Subkeys           []GPGKey      `json:"subkeys,omitzero"`
	CanSign           bool          `json:"canSign"`
	CanEncryptComms   bool          `json:"canEncryptComms"`
	CanEncryptStorage bool          `json:"canEncryptStorage"`
	CanCertify        bool          `json:"canCertify"`
	Verified          bool          `json:"verified,omitzero"` // Forgejo only
	CreatedAt         time.Time     `json:"createdAt,omitzero"`
	ExpiresAt         time.Time     `json:"expiresAt,omitzero"` // zero if the key does not expire
}

// GPGKeyEmail is an e-mail address bound to a GPG key.
type GPGKeyEmail struct {
	Address  string `json:"address"`
	Verified bool   `json:"verified"`
}

// CreateUserOptions describes a user account to create.
type CreateUserOptions struct {
	Login    string `json:"login"` // required
	Email    string `json:"email"` // required
	Password string `json:"-"`
	FullName string `json:"fullName,omitzero"`
	// MustChangePassword forces a password change at first sign-in.
	// Nil means the Forgejo default, which is true.
	MustChangePassword *bool           `json:"mustChangePassword,omitzero"`
	Visibility         OwnerVisibility `json:"visibility,omitzero"` // zero value: backend default
	Restricted         bool            `json:"restricted,omitzero"`
	// SendNotify emails the user about the new account.
	SendNotify bool `json:"sendNotify,omitzero"`
}

// EditUserOptions changes a user account. Nil fields are left unchanged.
type EditUserOptions struct {
	Email                   *string          `json:"email,omitzero"`
	FullName                *string          `json:"fullName,omitzero"`
	Description             *string          `json:"description,omitzero"`
	Website                 *string          `json:"website,omitzero"`
	Location                *string          `json:"location,omitzero"`
	Password                *string          `json:"-"`
	MustChangePassword      *bool            `json:"mustChangePassword,omitzero"`
	Visibility              *OwnerVisibility `json:"visibility,omitzero"`
	Admin                   *bool            `json:"admin,omitzero"`
	Active                  *bool            `json:"active,omitzero"`
	ProhibitLogin           *bool            `json:"prohibitLogin,omitzero"`
	Restricted              *bool            `json:"restricted,omitzero"`
	AllowCreateOrganization *bool            `json:"allowCreateOrganization,omitzero"`
	MaxRepoCreation         *int             `json:"maxRepoCreation,omitzero"` // -1 means the instance default
}

// DeleteUserOptions controls user deletion.
type DeleteUserOptions struct {
	// Purge also deletes the user's repositories, organisation memberships
	// and other content. Without it, deleting a user who still owns
	// repositories fails.
	Purge bool `json:"purge,omitzero"`
}
