package gitoperationslib

// Org is an organisation.
type Org struct {
	ID          int64
	Name        string
	FullName    string
	Description string
	Website     string
	Location    string
	Email       string
	Visibility  OwnerVisibility
	// RepoAdminChangeTeamAccess lets repository admins change team access.
	RepoAdminChangeTeamAccess bool
}

// CreateOrgOptions describes an organisation to create.
type CreateOrgOptions struct {
	// Owner is the user who will own the organisation. Empty means the
	// authenticated user; another user requires a site-admin token.
	Owner                     string
	Name                      string // required
	FullName                  string
	Description               string
	Website                   string
	Location                  string
	Email                     string
	Visibility                OwnerVisibility // zero value: backend default
	RepoAdminChangeTeamAccess bool
}

// EditOrgOptions changes an organisation. Nil fields are left unchanged.
type EditOrgOptions struct {
	FullName                  *string
	Description               *string
	Website                   *string
	Location                  *string
	Email                     *string
	Visibility                *OwnerVisibility
	RepoAdminChangeTeamAccess *bool
}
