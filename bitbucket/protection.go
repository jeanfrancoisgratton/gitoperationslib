package bitbucket

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/bitbucketapi"
)

// Bitbucket branch restriction kinds used by BranchProtection.
const (
	kindApprovals  = "require_approvals_to_merge"
	kindResetStale = "reset_pullrequest_approvals_on_change" // Premium plans
	kindDelete     = "delete"
	kindForce      = "force"
	kindPush       = "push"
	kindMerge      = "restrict_merges"
)

type groupJSON struct {
	Slug string `json:"slug"`
}

type restrictionJSON struct {
	ID              int64       `json:"id,omitempty"`
	Kind            string      `json:"kind"`
	Pattern         string      `json:"pattern"`
	BranchMatchKind string      `json:"branch_match_kind"`
	Value           *int        `json:"value,omitempty"`
	Users           []userJSON  `json:"users,omitempty"`
	Groups          []groupJSON `json:"groups,omitempty"`
}

// BranchProtections implements gitoperationslib.Client with branch
// restrictions: a rule is the set of restrictions sharing a pattern.
//
// Bitbucket has no setting matching ApplyToAdmins: it is ignored on write and
// reported as true. Resetting approvals on new commits
// (DismissStaleApprovals) needs a Premium plan.
func (c *Client) BranchProtections() gitoperationslib.BranchProtections {
	return branchProtections{c}
}

type branchProtections struct{ c *Client }

func restrictionsPath(owner, repo string, rest ...string) string {
	return repoPath(owner, repo, append([]string{"branch-restrictions"}, rest...)...)
}

func (b branchProtections) all(ctx context.Context, owner, repo string) ([]restrictionJSON, error) {
	all, err := bitbucketapi.All[restrictionJSON](ctx, b.c.api, restrictionsPath(owner, repo), nil)
	if err != nil {
		return nil, err
	}
	glob := all[:0]
	for _, r := range all {
		if r.BranchMatchKind == "" || r.BranchMatchKind == "glob" {
			glob = append(glob, r)
		}
	}
	return glob, nil
}

func toAllowlist(r restrictionJSON) *gitoperationslib.Allowlist {
	a := &gitoperationslib.Allowlist{Users: []string{}, Teams: []string{}}
	for i := range r.Users {
		a.Users = append(a.Users, r.Users[i].login())
	}
	for _, g := range r.Groups {
		a.Teams = append(a.Teams, g.Slug)
	}
	return a
}

// fromRestrictions builds the rule for pattern from its restrictions.
func fromRestrictions(pattern string, rs []restrictionJSON) gitoperationslib.BranchProtection {
	p := gitoperationslib.BranchProtection{Pattern: pattern, ApplyToAdmins: true, AllowDeletion: true, AllowForcePush: true}
	for _, r := range rs {
		switch r.Kind {
		case kindApprovals:
			if r.Value != nil {
				p.RequiredApprovals = *r.Value
			}
		case kindResetStale:
			p.DismissStaleApprovals = true
		case kindDelete:
			p.AllowDeletion = false
		case kindForce:
			p.AllowForcePush = false
		case kindPush:
			p.PushAllowlist = toAllowlist(r)
		case kindMerge:
			p.MergeAllowlist = toAllowlist(r)
		}
	}
	return p
}

func (b branchProtections) List(ctx context.Context, owner, repo string) ([]gitoperationslib.BranchProtection, error) {
	all, err := b.all(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	var order []string
	byPattern := map[string][]restrictionJSON{}
	for _, r := range all {
		if _, seen := byPattern[r.Pattern]; !seen {
			order = append(order, r.Pattern)
		}
		byPattern[r.Pattern] = append(byPattern[r.Pattern], r)
	}
	rules := make([]gitoperationslib.BranchProtection, len(order))
	for i, pattern := range order {
		rules[i] = fromRestrictions(pattern, byPattern[pattern])
	}
	return rules, nil
}

func (b branchProtections) forPattern(ctx context.Context, owner, repo, pattern string) ([]restrictionJSON, error) {
	all, err := b.all(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	var rs []restrictionJSON
	for _, r := range all {
		if r.Pattern == pattern {
			rs = append(rs, r)
		}
	}
	if len(rs) == 0 {
		return nil, &gitoperationslib.APIError{
			Backend: gitoperationslib.BackendBitbucket, StatusCode: http.StatusNotFound,
			Message: fmt.Sprintf("no branch restrictions for %q in %s/%s", pattern, owner, repo),
		}
	}
	return rs, nil
}

func (b branchProtections) Get(ctx context.Context, owner, repo, pattern string) (*gitoperationslib.BranchProtection, error) {
	rs, err := b.forPattern(ctx, owner, repo, pattern)
	if err != nil {
		return nil, err
	}
	p := fromRestrictions(pattern, rs)
	return &p, nil
}

// toRestrictions lists the restrictions a rule needs.
func (b branchProtections) toRestrictions(ctx context.Context, owner string, p gitoperationslib.BranchProtection, op string) ([]restrictionJSON, error) {
	unsupported := map[string]bool{
		"named status checks (Bitbucket only counts passing builds)": len(p.RequiredStatusChecks) > 0,
		"requiring up-to-date branches":                              p.RequireUpToDate,
		"requiring signed commits":                                   p.RequireSignedCommits,
		"requiring linear history":                                   p.RequireLinearHistory,
		"requiring conversation resolution":                          p.RequireConversationResolution,
		"approval allowlists":                                        p.ApprovalsAllowlist != nil,
		"blocking on rejected reviews":                               p.BlockOnRejectedReviews,
		"blocking on official review requests":                       p.BlockOnOfficialReviewRequests,
		"protected file patterns":                                    p.ProtectedFilePatterns != "" || p.UnprotectedFilePatterns != "",
	}
	for what, set := range unsupported {
		if set {
			return nil, notSupported(op, what)
		}
	}
	base := func(kind string) restrictionJSON {
		return restrictionJSON{Kind: kind, Pattern: p.Pattern, BranchMatchKind: "glob"}
	}
	var rs []restrictionJSON
	if p.RequiredApprovals > 0 {
		r := base(kindApprovals)
		r.Value = &p.RequiredApprovals
		rs = append(rs, r)
	}
	if p.DismissStaleApprovals {
		rs = append(rs, base(kindResetStale))
	}
	if !p.AllowDeletion {
		rs = append(rs, base(kindDelete))
	}
	if !p.AllowForcePush {
		rs = append(rs, base(kindForce))
	}
	for kind, list := range map[string]*gitoperationslib.Allowlist{kindPush: p.PushAllowlist, kindMerge: p.MergeAllowlist} {
		if list == nil {
			continue
		}
		r := base(kind)
		r.Users, r.Groups = []userJSON{}, []groupJSON{}
		for _, who := range list.Users {
			uuid, err := b.c.resolveUser(ctx, owner, who)
			if err != nil {
				return nil, err
			}
			r.Users = append(r.Users, userJSON{UUID: uuid})
		}
		for _, slug := range list.Teams {
			r.Groups = append(r.Groups, groupJSON{Slug: slug})
		}
		rs = append(rs, r)
	}
	if len(rs) == 0 {
		return nil, errors.New("bitbucket: " + op + ": the rule restricts nothing (Bitbucket stores rules as restrictions)")
	}
	return rs, nil
}

// create adds restrictions, removing the ones already added if one fails.
func (b branchProtections) create(ctx context.Context, owner, repo string, rs []restrictionJSON) error {
	var created []int64
	for _, r := range rs {
		var out restrictionJSON
		if _, err := b.c.api.Do(ctx, http.MethodPost, restrictionsPath(owner, repo), nil, r, &out); err != nil {
			for _, id := range created {
				_, _ = b.c.api.Do(ctx, http.MethodDelete, restrictionsPath(owner, repo, strconv.FormatInt(id, 10)), nil, nil, nil)
			}
			return fmt.Errorf("bitbucket: restriction %s: %w", r.Kind, err)
		}
		created = append(created, out.ID)
	}
	return nil
}

func (b branchProtections) Create(ctx context.Context, owner, repo string, rule gitoperationslib.BranchProtection) (*gitoperationslib.BranchProtection, error) {
	if rule.Pattern == "" {
		return nil, errors.New("bitbucket: branch_protections.create: pattern is required")
	}
	rs, err := b.toRestrictions(ctx, owner, rule, "branch_protections.create")
	if err != nil {
		return nil, err
	}
	if err := b.create(ctx, owner, repo, rs); err != nil {
		return nil, err
	}
	return b.Get(ctx, owner, repo, rule.Pattern)
}

// Update replaces the rule's restrictions (not atomically: it deletes the
// old ones, then creates the new ones).
func (b branchProtections) Update(ctx context.Context, owner, repo, pattern string, rule gitoperationslib.BranchProtection) (*gitoperationslib.BranchProtection, error) {
	if rule.Pattern == "" {
		rule.Pattern = pattern
	}
	rs, err := b.toRestrictions(ctx, owner, rule, "branch_protections.update")
	if err != nil {
		return nil, err
	}
	if err := b.Delete(ctx, owner, repo, pattern); err != nil {
		return nil, err
	}
	if err := b.create(ctx, owner, repo, rs); err != nil {
		return nil, err
	}
	return b.Get(ctx, owner, repo, rule.Pattern)
}

func (b branchProtections) Delete(ctx context.Context, owner, repo, pattern string) error {
	rs, err := b.forPattern(ctx, owner, repo, pattern)
	if err != nil {
		return err
	}
	for _, r := range rs {
		if _, err := b.c.api.Do(ctx, http.MethodDelete, restrictionsPath(owner, repo, strconv.FormatInt(r.ID, 10)), nil, nil, nil); err != nil {
			return err
		}
	}
	return nil
}
