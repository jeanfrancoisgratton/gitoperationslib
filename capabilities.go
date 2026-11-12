package gitoperationslib

import "strings"

// Capability is a bit set of features a Client supports. Every area is reached
// through the Client whatever its capabilities: what a backend lacks answers
// ErrNotSupported. Capabilities tell beforehand.
type Capability uint64

const (
	CapRepos   Capability = 1 << iota
	CapMirrors            // push mirrors and syncing mirrors (Mirrors())
	CapMigrate            // reserved
	CapAdmin              // managing users, organisations and teams (Users(), Orgs(), Teams())
	CapWebhooks
	CapSystemWebhooks // system and default webhooks (Site())
	CapReleases
	CapPullRequests
	CapExternalAssets
	CapBranches
	CapTags
	CapBranchProtection
	CapTagProtection
	CapIssues
	CapDirectory // reading users, organisations and teams (Users(), Orgs(), Teams())
	// Added in 1.1.0. New bits are only ever appended, so existing values
	// never change.
	CapAccount // the authenticated user's own account (Account())
	CapTokens  // access token management (Account().Tokens())
	CapSecrets // CI secrets and variables (CI().Secrets(), CI().Variables())
	CapCommits // commit history (Commits())
	// Added in 1.2.0.
	CapOrgAvatars // organisation avatars (Orgs().SetAvatar)
	// Added in 1.3.0.
	CapUserWebhooks   // the authenticated user's webhooks (Account().Webhooks())
	CapCommitDiffs    // a commit's raw diff (Commits().Diff)
	CapPRContents     // a pull request's diff, files and commits (PullRequests().Diff…)
	CapReviewComments // comments on a pull request's lines (PullRequests().ListReviewComments…)
	CapCollaborators  // a repository's collaborators (Collaborators())
	CapGitHooks       // a repository's server-side git hooks (GitHooks())
	CapHookDeliveries // a webhook's delivery history (Webhooks().ListDeliveries…)
	CapCIEnvironments // secrets and variables of an environment (EnvCI)
	CapOrgSecrets     // organisation secrets limited to some repositories (Secrets.SetWithAccess…)
	// CapCollaboratorInvitations: invitations not yet accepted (Collaborators().ListInvitations).
	CapCollaboratorInvitations
	// Added in 1.5.0.
	CapUserFilters // listing users by criteria (Users().Search)
	CapIssueLocks  // locking and unlocking issues (Issues().Lock, Unlock)
	CapAdminTokens // managing other users' access tokens (Users().Tokens(login))
	// Added in 1.6.0.
	CapOrgLabels         // an organisation's labels (Orgs().Labels())
	CapLabelsByID        // repository labels addressed by ID (Labels().EditByID, DeleteByID)
	CapWorkflowRuns      // CI workflow runs, jobs and logs (CI().Runs())
	CapWorkflowArtifacts // CI workflow artifacts (CI().Artifacts())
	// Added in 1.8.0.
	CapRunners           // CI runners' records (CI().Runners())
	CapWorkflowLogFollow // following a job's log (CI().Runs().FollowJobLog)
	// Added in 1.9.0.
	CapCommitStatuses // a commit's CI state (Commits().Status, SetStatus)
)

var capabilityNames = []struct {
	cap  Capability
	name string
}{
	{CapRepos, "repos"},
	{CapMirrors, "mirrors"},
	{CapMigrate, "migrate"},
	{CapAdmin, "admin"},
	{CapWebhooks, "webhooks"},
	{CapSystemWebhooks, "system-webhooks"},
	{CapReleases, "releases"},
	{CapPullRequests, "pull-requests"},
	{CapExternalAssets, "external-assets"},
	{CapBranches, "branches"},
	{CapTags, "tags"},
	{CapBranchProtection, "branch-protection"},
	{CapTagProtection, "tag-protection"},
	{CapIssues, "issues"},
	{CapDirectory, "directory"},
	{CapAccount, "account"},
	{CapTokens, "tokens"},
	{CapSecrets, "secrets"},
	{CapCommits, "commits"},
	{CapOrgAvatars, "org-avatars"},
	{CapUserWebhooks, "user-webhooks"},
	{CapCommitDiffs, "commit-diffs"},
	{CapPRContents, "pr-contents"},
	{CapReviewComments, "review-comments"},
	{CapCollaborators, "collaborators"},
	{CapGitHooks, "git-hooks"},
	{CapHookDeliveries, "hook-deliveries"},
	{CapCIEnvironments, "ci-environments"},
	{CapOrgSecrets, "org-secrets"},
	{CapCollaboratorInvitations, "collaborator-invitations"},
	{CapUserFilters, "user-filters"},
	{CapIssueLocks, "issue-locks"},
	{CapAdminTokens, "admin-tokens"},
	{CapOrgLabels, "org-labels"},
	{CapLabelsByID, "labels-by-id"},
	{CapWorkflowRuns, "workflow-runs"},
	{CapWorkflowArtifacts, "workflow-artifacts"},
	{CapRunners, "runners"},
	{CapWorkflowLogFollow, "workflow-log-follow"},
	{CapCommitStatuses, "commit-statuses"},
}

// Has reports whether c includes every capability in want.
func (c Capability) Has(want Capability) bool { return c&want == want }

// String lists the named capabilities in c, separated by "|".
func (c Capability) String() string {
	var names []string
	for _, n := range capabilityNames {
		if c.Has(n.cap) {
			names = append(names, n.name)
		}
	}
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, "|")
}
