package gitoperationslib

import "time"

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
	ReadOnly    bool      `json:"readOnly,omitzero"`    // Forgejo and GitHub only
	CreatedAt   time.Time `json:"createdAt,omitzero"`
	LastUsed    time.Time `json:"lastUsed,omitzero"` // GitHub and Bitbucket only; zero if never used
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
