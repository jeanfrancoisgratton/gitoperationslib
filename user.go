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
	CreatedAt     time.Time
	LastLogin     time.Time // zero if the user never signed in
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
