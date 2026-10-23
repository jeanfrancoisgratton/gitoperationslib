package gitoperationslib

import "time"

// User is a user account.
type User struct {
	ID            int64
	Login         string
	FullName      string
	Email         string
	Description   string
	Website       string
	Location      string
	Visibility    OwnerVisibility
	IsAdmin       bool
	Active        bool
	Restricted    bool
	ProhibitLogin bool
	HTMLURL       string
	AvatarURL     string
	Company       string // GitHub only
	Pronouns      string // Forgejo only
	Language      string // interface language; Forgejo only
	Followers     int
	Following     int
	StarredRepos  int // Forgejo only
	CreatedAt     time.Time
	LastLogin     time.Time // zero if the user never signed in
}

// UserSettings are a Forgejo user's account preferences.
type UserSettings struct {
	Theme               string
	DiffViewStyle       string // "unified" or "split"
	HideEmail           bool
	HideActivity        bool
	HidePronouns        bool
	EnableRepoUnitHints bool
}

// Email is one of the authenticated user's e-mail addresses.
type Email struct {
	Address  string
	Primary  bool
	Verified bool
	// Visibility is "public" or "private"; GitHub only, and only for the
	// primary address.
	Visibility string
}

// SSHKey is a public SSH key of the authenticated user.
type SSHKey struct {
	ID          int64 // 0 on Bitbucket
	Title       string
	Key         string
	Fingerprint string // not reported by GitHub
	ReadOnly    bool   // Forgejo and GitHub only
	CreatedAt   time.Time
	LastUsed    time.Time // GitHub and Bitbucket only; zero if never used
}

// GPGKey is a GPG key of the authenticated user.
type GPGKey struct {
	ID                int64
	KeyID             string
	PublicKey         string
	Emails            []GPGKeyEmail
	Subkeys           []GPGKey
	CanSign           bool
	CanEncryptComms   bool
	CanEncryptStorage bool
	CanCertify        bool
	Verified          bool // Forgejo only
	CreatedAt         time.Time
	ExpiresAt         time.Time // zero if the key does not expire
}

// GPGKeyEmail is an e-mail address bound to a GPG key.
type GPGKeyEmail struct {
	Address  string
	Verified bool
}

// CreateUserOptions describes a user account to create.
type CreateUserOptions struct {
	Login    string // required
	Email    string // required
	Password string
	FullName string
	// MustChangePassword forces a password change at first sign-in.
	// Nil means the Forgejo default, which is true.
	MustChangePassword *bool
	Visibility         OwnerVisibility // zero value: backend default
	Restricted         bool
	// SendNotify emails the user about the new account.
	SendNotify bool
}

// EditUserOptions changes a user account. Nil fields are left unchanged.
type EditUserOptions struct {
	Email                   *string
	FullName                *string
	Description             *string
	Website                 *string
	Location                *string
	Password                *string
	MustChangePassword      *bool
	Visibility              *OwnerVisibility
	Admin                   *bool
	Active                  *bool
	ProhibitLogin           *bool
	Restricted              *bool
	AllowCreateOrganization *bool
	MaxRepoCreation         *int // -1 means the instance default
}

// DeleteUserOptions controls user deletion.
type DeleteUserOptions struct {
	// Purge also deletes the user's repositories, organisation memberships
	// and other content. Without it, deleting a user who still owns
	// repositories fails.
	Purge bool
}
