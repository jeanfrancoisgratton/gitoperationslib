package gitoperationslib

import "strings"

// Capability is a bit set of features a Client supports.
type Capability uint64

const (
	CapRepos   Capability = 1 << iota
	CapMirrors            // push mirrors and syncing mirrors (Mirrors)
	CapMigrate            // reserved
	CapAdmin
	CapWebhooks
	CapSystemWebhooks // system and default webhooks (SiteHooks)
	CapReleases
	CapPullRequests
	CapExternalAssets
	CapBranches
	CapTags
	CapBranchProtection
	CapTagProtection
	CapIssues
	CapDirectory
	// Added in 1.1.0. New bits are only ever appended, so existing values
	// never change.
	CapAccount // the authenticated user's own account (Account)
	CapTokens  // access token management (Account.Tokens)
	CapSecrets // CI secrets and variables (CI)
	CapCommits // commit history (History)
	// Added in 1.2.0.
	CapOrgAvatars // organisation avatars (OrgAvatars)
	// Added in 1.3.0.
	CapUserWebhooks   // the authenticated user's webhooks (UserHooks)
	CapCommitDiffs    // a commit's raw diff (CommitDiffs)
	CapPRContents     // a pull request's diff, files and commits (PRContents)
	CapReviewComments // comments on a pull request's lines (ReviewComments)
	CapCollaborators  // a repository's collaborators (Collaborators)
	CapGitHooks       // a repository's server-side git hooks (GitHooks)
	CapHookDeliveries // a webhook's delivery history (HookDeliveries)
	CapCIEnvironments // secrets and variables of an environment (EnvCI)
	CapOrgSecrets     // organisation secrets limited to some repositories (OrgSecrets)
	// CapCollaboratorInvitations: invitations not yet accepted (CollaboratorInvitations).
	CapCollaboratorInvitations
	// Added in 1.5.0.
	CapUserFilters // listing users by criteria (UserFilters)
	CapIssueLocks  // locking and unlocking issues (IssueLocks)
	CapAdminTokens // managing other users' access tokens (AdminTokens)
	// Added in 1.6.0.
	CapOrgLabels  // an organisation's labels (OrgLabels)
	CapLabelsByID // repository labels addressed by ID (LabelsByID)
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
