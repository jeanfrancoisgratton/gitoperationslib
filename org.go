package gitoperationslib

// Org is an organisation.
type Org struct {
	ID          int64           `json:"id"`
	Name        string          `json:"name"`
	FullName    string          `json:"fullName"`
	Description string          `json:"description"`
	Website     string          `json:"website"`
	Location    string          `json:"location"`
	Email       string          `json:"email"`
	Visibility  OwnerVisibility `json:"visibility"`
	AvatarURL   string          `json:"avatarUrl"`
	HTMLURL     string          `json:"htmlUrl,omitzero"` // GitHub only
	// RepoAdminChangeTeamAccess lets repository admins change team access.
	RepoAdminChangeTeamAccess bool `json:"repoAdminChangeTeamAccess,omitzero"`
}

// CreateOrgOptions describes an organisation to create.
type CreateOrgOptions struct {
	// Owner is the user who will own the organisation. Empty means the
	// authenticated user; another user requires a site-admin token.
	Owner                     string          `json:"owner,omitzero"`
	Name                      string          `json:"name"` // required
	FullName                  string          `json:"fullName,omitzero"`
	Description               string          `json:"description,omitzero"`
	Website                   string          `json:"website,omitzero"`
	Location                  string          `json:"location,omitzero"`
	Email                     string          `json:"email,omitzero"`
	Visibility                OwnerVisibility `json:"visibility,omitzero"` // zero value: backend default
	RepoAdminChangeTeamAccess bool            `json:"repoAdminChangeTeamAccess,omitzero"`
}

// EditOrgOptions changes an organisation. Nil fields are left unchanged.
type EditOrgOptions struct {
	FullName                  *string          `json:"fullName,omitzero"`
	Description               *string          `json:"description,omitzero"`
	Website                   *string          `json:"website,omitzero"`
	Location                  *string          `json:"location,omitzero"`
	Email                     *string          `json:"email,omitzero"`
	Visibility                *OwnerVisibility `json:"visibility,omitzero"`
	RepoAdminChangeTeamAccess *bool            `json:"repoAdminChangeTeamAccess,omitzero"`
}
