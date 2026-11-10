package gitoperationslib

import "time"

// Repo is a repository, modelled on the fields the forges share.
type Repo struct {
	ID            int64      `json:"id"`
	Owner         string     `json:"owner"`
	Name          string     `json:"name"`
	FullName      string     `json:"fullName"` // "owner/name"
	Description   string     `json:"description"`
	Website       string     `json:"website"` // Forgejo "website", GitHub "homepage"
	Visibility    Visibility `json:"visibility"`
	DefaultBranch string     `json:"defaultBranch"`
	Fork          bool       `json:"fork"`
	Mirror        bool       `json:"mirror"`
	Archived      bool       `json:"archived"`
	Template      bool       `json:"template"`
	Empty         bool       `json:"empty,omitzero"` // not reported by GitHub (always false there)
	// Features says which features are on. A nil field is one the forge does not
	// have, or does not report here: see RepoFeatures.
	Features RepoFeatures `json:"features,omitzero"`
	// Merge, IssueTracker, Wiki and Actions are settings; nil fields are what the
	// forge lacks or does not report there. GitHub reports Merge from Repos().Get
	// only, and Actions settings take it two more requests there.
	Merge        MergeSettings        `json:"merge,omitzero"`
	IssueTracker IssueTrackerSettings `json:"issueTracker,omitzero"`
	Wiki         WikiSettings         `json:"wiki,omitzero"`
	Actions      ActionsSettings      `json:"actions,omitzero"`
	HTMLURL      string               `json:"htmlUrl"`
	CloneURL     string               `json:"cloneUrl"`
	SSHURL       string               `json:"sshUrl"`
	CreatedAt    time.Time            `json:"createdAt,omitzero"`
	UpdatedAt    time.Time            `json:"updatedAt,omitzero"`
}

// CreateRepoOptions describes a repository to create.
type CreateRepoOptions struct {
	// Owner is the user or organisation that will own the repository; the
	// backend determines which. Empty means the authenticated user. Naming a
	// user other than the authenticated one requires an admin token (Forgejo)
	// and is not supported on GitHub.
	Owner         string     `json:"owner,omitzero"`
	Name          string     `json:"name"`
	Description   string     `json:"description,omitzero"`
	Visibility    Visibility `json:"visibility,omitzero"` // zero value: backend default
	DefaultBranch string     `json:"defaultBranch,omitzero"`
	Template      bool       `json:"template,omitzero"`
	// AutoInit creates an initial commit, using Gitignores and License if set.
	AutoInit bool `json:"autoInit,omitzero"`
	// Gitignores lists gitignore template names. GitHub accepts at most one
	// and returns ErrNotSupported otherwise.
	Gitignores []string `json:"gitignores,omitzero"`
	License    string   `json:"license,omitzero"` // license template name
	// Features turns features on or off; nil fields keep the server's default.
	// Forgejo and GitHub may need more requests after the creation, and if one of
	// those fails, Create returns the new repository with the error.
	Features RepoFeatures `json:"features,omitzero"`
}

// RepoFeatures are a repository's optional features: Forgejo's units, GitHub's
// features and Actions. Reported in Repo, nil means the forge does not have it,
// or does not report it there. In options, nil leaves it as it is.
//
// Setting one the forge does not have returns ErrNotSupported, before any change.
// A server can also answer success and keep its own value, such as Forgejo with
// Actions turned off for the whole instance, or GitHub's wiki on a private
// repository of a free account: Create and Edit then return the repository with
// an error matching ErrNotSupported that names what was not applied.
type RepoFeatures struct {
	// Actions is CI workflows: Forgejo Actions, GitHub Actions. GitHub reports it
	// from Repos().Get only, as it takes one more request; it stays nil in lists,
	// and when the token cannot read it.
	Actions      *bool `json:"actions,omitzero"`
	Issues       *bool `json:"issues,omitzero"`
	Wiki         *bool `json:"wiki,omitzero"`
	PullRequests *bool `json:"pullRequests,omitzero"`
	Projects     *bool `json:"projects,omitzero"`
	Releases     *bool `json:"releases,omitzero"`    // Forgejo only
	Packages     *bool `json:"packages,omitzero"`    // Forgejo only
	Discussions  *bool `json:"discussions,omitzero"` // GitHub only
}

// EditRepoOptions changes repository settings. Nil fields are left unchanged;
// use Ptr to set a value.
type EditRepoOptions struct {
	Name          *string     `json:"name,omitzero"`
	Description   *string     `json:"description,omitzero"`
	Website       *string     `json:"website,omitzero"`
	Visibility    *Visibility `json:"visibility,omitzero"`
	DefaultBranch *string     `json:"defaultBranch,omitzero"`
	Archived      *bool       `json:"archived,omitzero"`
	Template      *bool       `json:"template,omitzero"`
	// Features turns features on or off; nil fields leave them as they are. See
	// RepoFeatures.
	Features RepoFeatures `json:"features,omitzero"`
	// Merge, IssueTracker, Wiki and Actions change settings; nil fields leave them
	// as they are. A setting the forge does not have returns ErrNotSupported, before
	// any change, and the result is read back as for Features.
	Merge        MergeSettings        `json:"merge,omitzero"`
	IssueTracker IssueTrackerSettings `json:"issueTracker,omitzero"`
	Wiki         WikiSettings         `json:"wiki,omitzero"`
	Actions      ActionsSettings      `json:"actions,omitzero"`
}

// MergeSettings say how pull requests can be merged.
type MergeSettings struct {
	MergeCommit *bool `json:"mergeCommit,omitzero"`
	Rebase      *bool `json:"rebase,omitzero"`
	// RebaseMerge is rebasing, then a merge commit (Forgejo only).
	RebaseMerge     *bool `json:"rebaseMerge,omitzero"`
	Squash          *bool `json:"squash,omitzero"`
	FastForwardOnly *bool `json:"fastForwardOnly,omitzero"` // Forgejo only
	// DefaultMethod is the one the web interface proposes (Forgejo only). Forgejo
	// accepts one the repository does not allow.
	DefaultMethod *MergeMethod `json:"defaultMethod,omitzero"`
	// DeleteBranch deletes the head branch after a merge: Forgejo's default for the
	// merge form, GitHub's automatic deletion.
	DeleteBranch *bool `json:"deleteBranch,omitzero"`
	// AutoMerge allows merging when checks pass (GitHub only; Forgejo always does).
	AutoMerge *bool `json:"autoMerge,omitzero"`
	// UpdateBranch offers to update a pull request's branch (GitHub only).
	UpdateBranch *bool `json:"updateBranch,omitzero"`
	// RebaseUpdate allows updating a pull request's branch by rebase (Forgejo only).
	RebaseUpdate *bool `json:"rebaseUpdate,omitzero"`
}

// IssueTrackerSettings set up a repository's issue tracker (Forgejo only).
type IssueTrackerSettings struct {
	TimeTracking *bool `json:"timeTracking,omitzero"`
	// TimeTrackingContributorsOnly lets only contributors track time.
	TimeTrackingContributorsOnly *bool `json:"timeTrackingContributorsOnly,omitzero"`
	Dependencies                 *bool `json:"dependencies,omitzero"` // between issues and pull requests
	// External sends issues to another tracker; the built-in one's issues are then
	// out of reach of the API (HTTP 404). In options, a zero URL goes back to the
	// built-in tracker.
	External *ExternalTracker `json:"external,omitzero"`
}

// ExternalTracker is an issue tracker outside the forge.
type ExternalTracker struct {
	URL string `json:"url"`
	// Format links an issue: "https://t.example.com/{user}/{repo}/{index}".
	Format  string             `json:"format,omitzero"`
	Style   ExternalIssueStyle `json:"style,omitzero"`   // zero: numeric
	Pattern string             `json:"pattern,omitzero"` // the regexp, for ExternalIssueRegexp
}

// ExternalIssueStyle is how an external tracker numbers issues.
type ExternalIssueStyle string

const (
	ExternalIssueNumeric      ExternalIssueStyle = "numeric"
	ExternalIssueAlphanumeric ExternalIssueStyle = "alphanumeric"
	ExternalIssueRegexp       ExternalIssueStyle = "regexp"
)

// WikiSettings set up a repository's wiki (Forgejo only).
type WikiSettings struct {
	// ExternalURL sends the wiki elsewhere. In options, "" goes back to the
	// built-in wiki.
	ExternalURL *string `json:"externalUrl,omitzero"`
	// EditableByAll lets every signed-in user edit it.
	EditableByAll *bool `json:"editableByAll,omitzero"`
}

// ActionsSettings set up GitHub Actions on a repository (GitHub only). The
// selected actions themselves are set in the web interface.
type ActionsSettings struct {
	Allowed *ActionsAllowed `json:"allowed,omitzero"`
	// TokenPermission is what the workflows' GITHUB_TOKEN may do by default.
	TokenPermission *ActionsTokenPermission `json:"tokenPermission,omitzero"`
	// ApprovePullRequests lets workflows approve pull requests.
	ApprovePullRequests *bool `json:"approvePullRequests,omitzero"`
}

// ActionsAllowed is which actions workflows may use.
type ActionsAllowed string

const (
	ActionsAllowedAll       ActionsAllowed = "all"
	ActionsAllowedLocalOnly ActionsAllowed = "local_only"
	ActionsAllowedSelected  ActionsAllowed = "selected"
)

// ActionsTokenPermission is the default access of a workflow's token.
type ActionsTokenPermission string

const (
	ActionsTokenRead  ActionsTokenPermission = "read"
	ActionsTokenWrite ActionsTokenPermission = "write"
)

// Ptr returns a pointer to v, for filling optional fields.
func Ptr[T any](v T) *T { return &v }
