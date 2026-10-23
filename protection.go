package gitoperationslib

// Allowlist names users and teams exempt from a restriction. On GitHub,
// rules are rulesets whose exemptions ("bypass actors") can be teams but not
// individual users, so Users returns ErrNotSupported there.
type Allowlist struct {
	Users []string `json:"users"` // logins
	Teams []string `json:"teams"` // team names (GitHub: team slugs; organisations only)
}

// BranchProtection is a protection rule for the branches matching Pattern.
// On GitHub it is a branch ruleset; on Forgejo a branch protection rule.
//
// Zero values are the protected choice: deletion and force pushes are
// blocked unless explicitly allowed.
type BranchProtection struct {
	ID int64 `json:"id,omitzero"` // GitHub ruleset ID; 0 on Forgejo
	// Pattern is a branch name or glob ("main", "release/*"). Rules are
	// addressed by it.
	Pattern string `json:"pattern"`

	RequiredApprovals     int  `json:"requiredApprovals"`
	DismissStaleApprovals bool `json:"dismissStaleApprovals"` // a new push dismisses earlier approvals
	// RequiredStatusChecks lists the status check contexts that must pass.
	RequiredStatusChecks []string `json:"requiredStatusChecks"`
	// RequireUpToDate requires the branch to be up to date with the base
	// before merging (GitHub strict checks; Forgejo blocks outdated
	// branches).
	RequireUpToDate      bool `json:"requireUpToDate"`
	RequireSignedCommits bool `json:"requireSignedCommits"`
	// ApplyToAdmins enforces the rule for repository administrators too.
	ApplyToAdmins bool `json:"applyToAdmins"`
	// PushAllowlist restricts direct pushes to the listed users and teams;
	// nil lets anyone with write access push. An empty Allowlist blocks
	// direct pushes entirely (changes then go through pull requests).
	PushAllowlist *Allowlist `json:"pushAllowlist,omitzero"`

	AllowDeletion  bool `json:"allowDeletion"`  // Forgejo: not supported (protected branches cannot be deleted)
	AllowForcePush bool `json:"allowForcePush"` // Forgejo: not supported

	// GitHub only.
	RequireLinearHistory          bool `json:"requireLinearHistory,omitzero"`
	RequireConversationResolution bool `json:"requireConversationResolution,omitzero"`

	// Forgejo only.
	MergeAllowlist                *Allowlist `json:"mergeAllowlist,omitzero"`     // who may merge pull requests; nil: anyone with write access
	ApprovalsAllowlist            *Allowlist `json:"approvalsAllowlist,omitzero"` // whose approvals count; nil: anyone with write access
	BlockOnRejectedReviews        bool       `json:"blockOnRejectedReviews,omitzero"`
	BlockOnOfficialReviewRequests bool       `json:"blockOnOfficialReviewRequests,omitzero"`
	ProtectedFilePatterns         string     `json:"protectedFilePatterns,omitzero"` // semicolon-separated globs
	UnprotectedFilePatterns       string     `json:"unprotectedFilePatterns,omitzero"`
}

// TagProtection restricts creating, updating and deleting the tags matching
// Pattern ("v*") to the users and teams in Allowlist. On GitHub it is a tag
// ruleset, and an empty Allowlist blocks everyone, administrators included.
// Forgejo requires at least one allowed user or team.
type TagProtection struct {
	ID        int64     `json:"id"`
	Pattern   string    `json:"pattern"`
	Allowlist Allowlist `json:"allowlist"`
}
