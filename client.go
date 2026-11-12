package gitoperationslib

// Client is the entry point to a forge backend. Every area is reached through
// it; an operation a backend, or its server's version, cannot perform returns
// an error matching ErrNotSupported. Capabilities tells beforehand.
type Client interface {
	Backend() Backend
	Capabilities() Capability

	Repos() Repos
	Branches() Branches
	Tags() Tags
	BranchProtections() BranchProtections
	TagProtections() TagProtections
	// Commits reads a repository's commits, their diffs and CI statuses.
	Commits() Commits
	Collaborators() Collaborators
	Mirrors() Mirrors
	GitHooks() GitHooks
	Webhooks() Webhooks
	Releases() Releases

	PullRequests() PullRequests
	Issues() Issues
	Labels() Labels
	Milestones() Milestones

	// CI groups CI secrets and variables, workflow runs, artifacts and runners.
	CI() CI

	// Account is the authenticated user's own account.
	Account() Account
	Users() Users
	Orgs() Orgs
	Teams() Teams
	// Site administers a Forgejo instance: system and default webhooks.
	Site() Site
}
