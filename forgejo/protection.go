package forgejo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/forgejoapi"
)

type branchProtectionJSON struct {
	RuleName                      string   `json:"rule_name,omitempty"`
	RequiredApprovals             int      `json:"required_approvals"`
	DismissStaleApprovals         bool     `json:"dismiss_stale_approvals"`
	EnableStatusCheck             bool     `json:"enable_status_check"`
	StatusCheckContexts           []string `json:"status_check_contexts"`
	BlockOnOutdatedBranch         bool     `json:"block_on_outdated_branch"`
	RequireSignedCommits          bool     `json:"require_signed_commits"`
	ApplyToAdmins                 bool     `json:"apply_to_admins"`
	EnablePush                    bool     `json:"enable_push"`
	EnablePushWhitelist           bool     `json:"enable_push_whitelist"`
	PushWhitelistUsernames        []string `json:"push_whitelist_usernames"`
	PushWhitelistTeams            []string `json:"push_whitelist_teams"`
	EnableMergeWhitelist          bool     `json:"enable_merge_whitelist"`
	MergeWhitelistUsernames       []string `json:"merge_whitelist_usernames"`
	MergeWhitelistTeams           []string `json:"merge_whitelist_teams"`
	EnableApprovalsWhitelist      bool     `json:"enable_approvals_whitelist"`
	ApprovalsWhitelistUsernames   []string `json:"approvals_whitelist_username"`
	ApprovalsWhitelistTeams       []string `json:"approvals_whitelist_teams"`
	BlockOnRejectedReviews        bool     `json:"block_on_rejected_reviews"`
	BlockOnOfficialReviewRequests bool     `json:"block_on_official_review_requests"`
	ProtectedFilePatterns         string   `json:"protected_file_patterns"`
	UnprotectedFilePatterns       string   `json:"unprotected_file_patterns"`
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// allowlist converts an optional allowlist to Forgejo's enable flag and
// lists.
func allowlist(a *gitoperationslib.Allowlist) (enabled bool, users, teams []string) {
	if a == nil {
		return false, []string{}, []string{}
	}
	return true, nonNil(a.Users), nonNil(a.Teams)
}

func fromAllowlist(enabled bool, users, teams []string) *gitoperationslib.Allowlist {
	if !enabled {
		return nil
	}
	return &gitoperationslib.Allowlist{Users: nonNil(users), Teams: nonNil(teams)}
}

func toBranchProtectionJSON(r gitoperationslib.BranchProtection, op string) (branchProtectionJSON, error) {
	unsupported := map[string]bool{
		"allowing deletion of protected branches":     r.AllowDeletion,
		"allowing force pushes to protected branches": r.AllowForcePush,
		"requiring linear history":                    r.RequireLinearHistory,
		"requiring conversation resolution":           r.RequireConversationResolution,
	}
	for what, set := range unsupported {
		if set {
			return branchProtectionJSON{}, gitoperationslib.NotSupported(gitoperationslib.BackendForgejo, op, what)
		}
	}
	j := branchProtectionJSON{
		RuleName:                      r.Pattern,
		RequiredApprovals:             r.RequiredApprovals,
		DismissStaleApprovals:         r.DismissStaleApprovals,
		EnableStatusCheck:             len(r.RequiredStatusChecks) > 0,
		StatusCheckContexts:           nonNil(r.RequiredStatusChecks),
		BlockOnOutdatedBranch:         r.RequireUpToDate,
		RequireSignedCommits:          r.RequireSignedCommits,
		ApplyToAdmins:                 r.ApplyToAdmins,
		EnablePush:                    true,
		BlockOnRejectedReviews:        r.BlockOnRejectedReviews,
		BlockOnOfficialReviewRequests: r.BlockOnOfficialReviewRequests,
		ProtectedFilePatterns:         r.ProtectedFilePatterns,
		UnprotectedFilePatterns:       r.UnprotectedFilePatterns,
	}
	j.EnablePushWhitelist, j.PushWhitelistUsernames, j.PushWhitelistTeams = allowlist(r.PushAllowlist)
	j.EnableMergeWhitelist, j.MergeWhitelistUsernames, j.MergeWhitelistTeams = allowlist(r.MergeAllowlist)
	j.EnableApprovalsWhitelist, j.ApprovalsWhitelistUsernames, j.ApprovalsWhitelistTeams = allowlist(r.ApprovalsAllowlist)
	return j, nil
}

func (j *branchProtectionJSON) toBranchProtection() gitoperationslib.BranchProtection {
	r := gitoperationslib.BranchProtection{
		Pattern:                       j.RuleName,
		RequiredApprovals:             j.RequiredApprovals,
		DismissStaleApprovals:         j.DismissStaleApprovals,
		RequireUpToDate:               j.BlockOnOutdatedBranch,
		RequireSignedCommits:          j.RequireSignedCommits,
		ApplyToAdmins:                 j.ApplyToAdmins,
		PushAllowlist:                 fromAllowlist(j.EnablePushWhitelist, j.PushWhitelistUsernames, j.PushWhitelistTeams),
		MergeAllowlist:                fromAllowlist(j.EnableMergeWhitelist, j.MergeWhitelistUsernames, j.MergeWhitelistTeams),
		ApprovalsAllowlist:            fromAllowlist(j.EnableApprovalsWhitelist, j.ApprovalsWhitelistUsernames, j.ApprovalsWhitelistTeams),
		BlockOnRejectedReviews:        j.BlockOnRejectedReviews,
		BlockOnOfficialReviewRequests: j.BlockOnOfficialReviewRequests,
		ProtectedFilePatterns:         j.ProtectedFilePatterns,
		UnprotectedFilePatterns:       j.UnprotectedFilePatterns,
	}
	if j.EnableStatusCheck {
		r.RequiredStatusChecks = nonNil(j.StatusCheckContexts)
	}
	if !j.EnablePush { // no direct pushes at all
		r.PushAllowlist = &gitoperationslib.Allowlist{Users: []string{}, Teams: []string{}}
	}
	return r
}

func protectionPath(owner, repo string, rest ...string) string {
	return forgejoapi.Path(append([]string{"repos", owner, repo, "branch_protections"}, rest...)...)
}

// BranchProtections implements gitoperationslib.Client.
func (c *Client) BranchProtections() gitoperationslib.BranchProtections { return branchProtections{c} }

type branchProtections struct{ c *Client }

func (b branchProtections) List(ctx context.Context, owner, repo string) ([]gitoperationslib.BranchProtection, error) {
	var out []branchProtectionJSON
	if _, err := b.c.api.Do(ctx, http.MethodGet, protectionPath(owner, repo), nil, nil, &out); err != nil {
		return nil, err
	}
	rules := make([]gitoperationslib.BranchProtection, len(out))
	for i := range out {
		rules[i] = out[i].toBranchProtection()
	}
	return rules, nil
}

func (b branchProtections) Get(ctx context.Context, owner, repo, pattern string) (*gitoperationslib.BranchProtection, error) {
	var out branchProtectionJSON
	if _, err := b.c.api.Do(ctx, http.MethodGet, protectionPath(owner, repo, pattern), nil, nil, &out); err != nil {
		return nil, err
	}
	r := out.toBranchProtection()
	return &r, nil
}

func (b branchProtections) Create(ctx context.Context, owner, repo string, rule gitoperationslib.BranchProtection) (*gitoperationslib.BranchProtection, error) {
	if rule.Pattern == "" {
		return nil, errors.New("forgejo: branch_protections.create: pattern is required")
	}
	body, err := toBranchProtectionJSON(rule, "branch_protections.create")
	if err != nil {
		return nil, err
	}
	var out branchProtectionJSON
	if _, err := b.c.api.Do(ctx, http.MethodPost, protectionPath(owner, repo), nil, body, &out); err != nil {
		return nil, err
	}
	r := out.toBranchProtection()
	return &r, nil
}

func (b branchProtections) Update(ctx context.Context, owner, repo, pattern string, rule gitoperationslib.BranchProtection) (*gitoperationslib.BranchProtection, error) {
	const op = "branch_protections.update"
	if rule.Pattern != "" && rule.Pattern != pattern {
		return nil, gitoperationslib.NotSupported(gitoperationslib.BackendForgejo, op, "renaming a rule (delete and recreate it)")
	}
	body, err := toBranchProtectionJSON(rule, op)
	if err != nil {
		return nil, err
	}
	body.RuleName = "" // not accepted on edit
	var out branchProtectionJSON
	if _, err := b.c.api.Do(ctx, http.MethodPatch, protectionPath(owner, repo, pattern), nil, body, &out); err != nil {
		return nil, err
	}
	r := out.toBranchProtection()
	return &r, nil
}

func (b branchProtections) Delete(ctx context.Context, owner, repo, pattern string) error {
	_, err := b.c.api.Do(ctx, http.MethodDelete, protectionPath(owner, repo, pattern), nil, nil, nil)
	return err
}

type tagProtectionJSON struct {
	ID                 int64    `json:"id,omitempty"`
	NamePattern        string   `json:"name_pattern"`
	WhitelistUsernames []string `json:"whitelist_usernames"`
	WhitelistTeams     []string `json:"whitelist_teams"`
}

func (j *tagProtectionJSON) toTagProtection() gitoperationslib.TagProtection {
	return gitoperationslib.TagProtection{
		ID: j.ID, Pattern: j.NamePattern,
		Allowlist: gitoperationslib.Allowlist{Users: nonNil(j.WhitelistUsernames), Teams: nonNil(j.WhitelistTeams)},
	}
}

func tagProtectionPath(owner, repo string, rest ...string) string {
	return forgejoapi.Path(append([]string{"repos", owner, repo, "tag_protections"}, rest...)...)
}

// TagProtections implements gitoperationslib.Client.
func (c *Client) TagProtections() gitoperationslib.TagProtections { return tagProtections{c} }

type tagProtections struct{ c *Client }

func (t tagProtections) List(ctx context.Context, owner, repo string) ([]gitoperationslib.TagProtection, error) {
	var out []tagProtectionJSON
	if _, err := t.c.api.Do(ctx, http.MethodGet, tagProtectionPath(owner, repo), nil, nil, &out); err != nil {
		return nil, err
	}
	rules := make([]gitoperationslib.TagProtection, len(out))
	for i := range out {
		rules[i] = out[i].toTagProtection()
	}
	return rules, nil
}

// Get returns the first rule with exactly this pattern.
func (t tagProtections) Get(ctx context.Context, owner, repo, pattern string) (*gitoperationslib.TagProtection, error) {
	rules, err := t.List(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	for i := range rules {
		if rules[i].Pattern == pattern {
			return &rules[i], nil
		}
	}
	return nil, &gitoperationslib.APIError{
		Backend: gitoperationslib.BackendForgejo, StatusCode: http.StatusNotFound,
		Message: fmt.Sprintf("no tag protection for %q in %s/%s", pattern, owner, repo),
	}
}

func (t tagProtections) Create(ctx context.Context, owner, repo string, rule gitoperationslib.TagProtection) (*gitoperationslib.TagProtection, error) {
	if rule.Pattern == "" {
		return nil, errors.New("forgejo: tag_protections.create: pattern is required")
	}
	body := tagProtectionJSON{NamePattern: rule.Pattern, WhitelistUsernames: nonNil(rule.Allowlist.Users), WhitelistTeams: nonNil(rule.Allowlist.Teams)}
	var out tagProtectionJSON
	if _, err := t.c.api.Do(ctx, http.MethodPost, tagProtectionPath(owner, repo), nil, body, &out); err != nil {
		return nil, err
	}
	r := out.toTagProtection()
	return &r, nil
}

func (t tagProtections) Update(ctx context.Context, owner, repo, pattern string, rule gitoperationslib.TagProtection) (*gitoperationslib.TagProtection, error) {
	cur, err := t.Get(ctx, owner, repo, pattern)
	if err != nil {
		return nil, err
	}
	if rule.Pattern == "" {
		rule.Pattern = pattern
	}
	body := tagProtectionJSON{NamePattern: rule.Pattern, WhitelistUsernames: nonNil(rule.Allowlist.Users), WhitelistTeams: nonNil(rule.Allowlist.Teams)}
	var out tagProtectionJSON
	if _, err := t.c.api.Do(ctx, http.MethodPatch, tagProtectionPath(owner, repo, strconv.FormatInt(cur.ID, 10)), nil, body, &out); err != nil {
		return nil, err
	}
	r := out.toTagProtection()
	return &r, nil
}

func (t tagProtections) Delete(ctx context.Context, owner, repo, pattern string) error {
	cur, err := t.Get(ctx, owner, repo, pattern)
	if err != nil {
		return err
	}
	_, err = t.c.api.Do(ctx, http.MethodDelete, tagProtectionPath(owner, repo, strconv.FormatInt(cur.ID, 10)), nil, nil, nil)
	return err
}
