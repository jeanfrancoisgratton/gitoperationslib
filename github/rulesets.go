package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	gh "github.com/google/go-github/v93/github"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// adminRoleID is the actor ID of the repository "admin" role in ruleset
// bypass lists.
const adminRoleID int64 = 5

// rulesets implements branch and tag protection with repository rulesets
// targeting refs/heads/<pattern> or refs/tags/<pattern>.
type rulesets struct {
	c      *Client
	target gh.RulesetTarget
	prefix string // "refs/heads/" or "refs/tags/"
}

func (r rulesets) kind() string { return string(r.target) } // "branch" or "tag"

// list returns this repository's own rulesets for r.target, with rules and
// conditions loaded.
func (r rulesets) list(ctx context.Context, owner, repo string) ([]*gh.RepositoryRuleset, error) {
	summaries, _, err := r.c.gh.Repositories.GetAllRulesets(ctx, owner, repo, &gh.RepositoryListRulesetsOptions{
		IncludesParents: gh.Ptr(false), ListOptions: gh.ListOptions{PerPage: 100},
	})
	if err != nil {
		return nil, convertError(err)
	}
	var out []*gh.RepositoryRuleset
	for _, s := range summaries {
		if s.Target == nil || *s.Target != r.target {
			continue
		}
		full, _, err := r.c.gh.Repositories.GetRuleset(ctx, owner, repo, s.GetID(), false)
		if err != nil {
			return nil, convertError(err)
		}
		out = append(out, full)
	}
	return out, nil
}

// pattern returns the ref pattern a ruleset targets, or "" if it targets
// more than one (such rulesets are listed but cannot be addressed).
func (r rulesets) pattern(rs *gh.RepositoryRuleset) string {
	if rs.Conditions == nil || rs.Conditions.RefName == nil || len(rs.Conditions.RefName.Include) != 1 {
		return ""
	}
	return strings.TrimPrefix(rs.Conditions.RefName.Include[0], r.prefix)
}

func (r rulesets) find(ctx context.Context, owner, repo, pattern string) (*gh.RepositoryRuleset, error) {
	all, err := r.list(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	for _, rs := range all {
		if r.pattern(rs) == pattern {
			return rs, nil
		}
	}
	return nil, &gitoperationslib.APIError{
		Backend: gitoperationslib.BackendGitHub, StatusCode: http.StatusNotFound,
		Message: fmt.Sprintf("no %s ruleset for %q in %s/%s", r.kind(), pattern, owner, repo),
	}
}

// bypassTeams turns team slugs into bypass actors. Users cannot be bypass
// actors in repository rulesets.
func (r rulesets) bypassTeams(ctx context.Context, owner string, a gitoperationslib.Allowlist, op string) ([]*gh.BypassActor, error) {
	if len(a.Users) > 0 {
		return nil, gitoperationslib.NotSupported(gitoperationslib.BackendGitHub, op, "exempting individual users (only teams)")
	}
	var actors []*gh.BypassActor
	for _, slug := range a.Teams {
		team, _, err := r.c.gh.Teams.GetTeamBySlug(ctx, owner, slug)
		if err != nil {
			return nil, fmt.Errorf("%s: team %q: %w", op, slug, convertError(err))
		}
		actors = append(actors, &gh.BypassActor{
			ActorID: team.ID, ActorType: gh.Ptr(gh.BypassActorTypeTeam), BypassMode: gh.Ptr(gh.BypassModeAlways),
		})
	}
	return actors, nil
}

// teamSlugs maps bypass team IDs back to slugs, using the repository's teams;
// unknown IDs are reported as numbers.
func (r rulesets) teamSlugs(ctx context.Context, owner, repo string, actors []*gh.BypassActor) []string {
	slugs := []string{}
	var byID map[int64]string
	for _, a := range actors {
		if actorType(a) != gh.BypassActorTypeTeam {
			continue
		}
		if byID == nil {
			byID = map[int64]string{}
			if teams, _, err := r.c.gh.Repositories.ListTeams(ctx, owner, repo, &gh.ListOptions{PerPage: 100}); err == nil {
				for _, t := range teams {
					byID[t.GetID()] = t.GetSlug()
				}
			}
		}
		if slug, ok := byID[a.GetActorID()]; ok {
			slugs = append(slugs, slug)
		} else {
			slugs = append(slugs, strconv.FormatInt(a.GetActorID(), 10))
		}
	}
	return slugs
}

func actorType(a *gh.BypassActor) gh.BypassActorType {
	if a == nil || a.ActorType == nil {
		return ""
	}
	return *a.ActorType
}

func hasAdminBypass(actors []*gh.BypassActor) bool {
	for _, a := range actors {
		if actorType(a) == gh.BypassActorTypeRepositoryRole && a.GetActorID() == adminRoleID {
			return true
		}
	}
	return false
}

func (r rulesets) create(ctx context.Context, owner, repo string, rs gh.RepositoryRuleset) (*gh.RepositoryRuleset, error) {
	out, _, err := r.c.gh.Repositories.CreateRuleset(ctx, owner, repo, rs)
	return out, convertError(err)
}

func (r rulesets) update(ctx context.Context, owner, repo, pattern string, rs gh.RepositoryRuleset) (*gh.RepositoryRuleset, error) {
	cur, err := r.find(ctx, owner, repo, pattern)
	if err != nil {
		return nil, err
	}
	out, _, err := r.c.gh.Repositories.UpdateRuleset(ctx, owner, repo, cur.GetID(), rs)
	return out, convertError(err)
}

func (r rulesets) delete(ctx context.Context, owner, repo, pattern string) error {
	cur, err := r.find(ctx, owner, repo, pattern)
	if err != nil {
		return err
	}
	_, err = r.c.gh.Repositories.DeleteRuleset(ctx, owner, repo, cur.GetID())
	return convertError(err)
}

func (r rulesets) base(pattern string) gh.RepositoryRuleset {
	return gh.RepositoryRuleset{
		Name:        r.kind() + ":" + pattern,
		Target:      gh.Ptr(r.target),
		Enforcement: gh.RulesetEnforcementActive,
		Conditions: &gh.RepositoryRulesetConditions{
			RefName: &gh.RepositoryRulesetRefConditionParameters{Include: []string{r.prefix + pattern}, Exclude: []string{}},
		},
		Rules:        &gh.RepositoryRulesetRules{},
		BypassActors: []*gh.BypassActor{},
	}
}

// BranchProtections implements gitoperationslib.Client with branch rulesets.
func (c *Client) BranchProtections() gitoperationslib.BranchProtections {
	return branchRulesets{rulesets{c, gh.RulesetTargetBranch, "refs/heads/"}}
}

type branchRulesets struct{ r rulesets }

func (b branchRulesets) toRuleset(ctx context.Context, owner string, p gitoperationslib.BranchProtection, op string) (gh.RepositoryRuleset, error) {
	unsupported := map[string]bool{
		"merge allowlists":                                    p.MergeAllowlist != nil,
		"approval allowlists":                                 p.ApprovalsAllowlist != nil,
		"blocking on rejected reviews":                        p.BlockOnRejectedReviews,
		"blocking on official review requests":                p.BlockOnOfficialReviewRequests,
		"protected file patterns":                             p.ProtectedFilePatterns != "" || p.UnprotectedFilePatterns != "",
		"requiring up-to-date branches without status checks": p.RequireUpToDate && len(p.RequiredStatusChecks) == 0,
	}
	for what, set := range unsupported {
		if set {
			return gh.RepositoryRuleset{}, gitoperationslib.NotSupported(gitoperationslib.BackendGitHub, op, what)
		}
	}
	rs := b.r.base(p.Pattern)
	rules := rs.Rules
	if !p.AllowDeletion {
		rules.Deletion = &gh.EmptyRuleParameters{}
	}
	if !p.AllowForcePush {
		rules.NonFastForward = &gh.EmptyRuleParameters{}
	}
	// A pull request rule forbids direct pushes; the push allowlist's teams
	// bypass the ruleset.
	if p.RequiredApprovals > 0 || p.DismissStaleApprovals || p.RequireConversationResolution || p.PushAllowlist != nil {
		rules.PullRequest = &gh.PullRequestRuleParameters{
			RequiredApprovingReviewCount:   p.RequiredApprovals,
			DismissStaleReviewsOnPush:      p.DismissStaleApprovals,
			RequiredReviewThreadResolution: p.RequireConversationResolution,
		}
	}
	if len(p.RequiredStatusChecks) > 0 {
		checks := make([]*gh.RuleStatusCheck, len(p.RequiredStatusChecks))
		for i, ctxName := range p.RequiredStatusChecks {
			checks[i] = &gh.RuleStatusCheck{Context: ctxName}
		}
		rules.RequiredStatusChecks = &gh.RequiredStatusChecksRuleParameters{
			RequiredStatusChecks: checks, StrictRequiredStatusChecksPolicy: p.RequireUpToDate,
		}
	}
	if p.RequireSignedCommits {
		rules.RequiredSignatures = &gh.EmptyRuleParameters{}
	}
	if p.RequireLinearHistory {
		rules.RequiredLinearHistory = &gh.EmptyRuleParameters{}
	}
	if !p.ApplyToAdmins {
		rs.BypassActors = append(rs.BypassActors, &gh.BypassActor{
			ActorID: gh.Ptr(adminRoleID), ActorType: gh.Ptr(gh.BypassActorTypeRepositoryRole), BypassMode: gh.Ptr(gh.BypassModeAlways),
		})
	}
	if p.PushAllowlist != nil {
		teams, err := b.r.bypassTeams(ctx, owner, *p.PushAllowlist, op)
		if err != nil {
			return gh.RepositoryRuleset{}, err
		}
		rs.BypassActors = append(rs.BypassActors, teams...)
	}
	return rs, nil
}

func (b branchRulesets) fromRuleset(ctx context.Context, owner, repo string, rs *gh.RepositoryRuleset) gitoperationslib.BranchProtection {
	p := gitoperationslib.BranchProtection{
		ID:            rs.GetID(),
		Pattern:       b.r.pattern(rs),
		ApplyToAdmins: !hasAdminBypass(rs.BypassActors),
	}
	rules := rs.Rules
	if rules == nil {
		rules = &gh.RepositoryRulesetRules{}
	}
	p.AllowDeletion = rules.Deletion == nil
	p.AllowForcePush = rules.NonFastForward == nil
	p.RequireSignedCommits = rules.RequiredSignatures != nil
	p.RequireLinearHistory = rules.RequiredLinearHistory != nil
	if pr := rules.PullRequest; pr != nil {
		p.RequiredApprovals = pr.RequiredApprovingReviewCount
		p.DismissStaleApprovals = pr.DismissStaleReviewsOnPush
		p.RequireConversationResolution = pr.RequiredReviewThreadResolution
		p.PushAllowlist = &gitoperationslib.Allowlist{Users: []string{}, Teams: b.r.teamSlugs(ctx, owner, repo, rs.BypassActors)}
	}
	if sc := rules.RequiredStatusChecks; sc != nil {
		for _, check := range sc.RequiredStatusChecks {
			p.RequiredStatusChecks = append(p.RequiredStatusChecks, check.Context)
		}
		p.RequireUpToDate = sc.StrictRequiredStatusChecksPolicy
	}
	return p
}

func (b branchRulesets) List(ctx context.Context, owner, repo string) ([]gitoperationslib.BranchProtection, error) {
	all, err := b.r.list(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	out := make([]gitoperationslib.BranchProtection, len(all))
	for i, rs := range all {
		out[i] = b.fromRuleset(ctx, owner, repo, rs)
	}
	return out, nil
}

func (b branchRulesets) Get(ctx context.Context, owner, repo, pattern string) (*gitoperationslib.BranchProtection, error) {
	rs, err := b.r.find(ctx, owner, repo, pattern)
	if err != nil {
		return nil, err
	}
	p := b.fromRuleset(ctx, owner, repo, rs)
	return &p, nil
}

func (b branchRulesets) Create(ctx context.Context, owner, repo string, rule gitoperationslib.BranchProtection) (*gitoperationslib.BranchProtection, error) {
	if rule.Pattern == "" {
		return nil, errors.New("github: branch_protections.create: pattern is required")
	}
	rs, err := b.toRuleset(ctx, owner, rule, "branch_protections.create")
	if err != nil {
		return nil, err
	}
	out, err := b.r.create(ctx, owner, repo, rs)
	if err != nil {
		return nil, err
	}
	p := b.fromRuleset(ctx, owner, repo, out)
	return &p, nil
}

func (b branchRulesets) Update(ctx context.Context, owner, repo, pattern string, rule gitoperationslib.BranchProtection) (*gitoperationslib.BranchProtection, error) {
	if rule.Pattern == "" {
		rule.Pattern = pattern
	}
	rs, err := b.toRuleset(ctx, owner, rule, "branch_protections.update")
	if err != nil {
		return nil, err
	}
	out, err := b.r.update(ctx, owner, repo, pattern, rs)
	if err != nil {
		return nil, err
	}
	p := b.fromRuleset(ctx, owner, repo, out)
	return &p, nil
}

func (b branchRulesets) Delete(ctx context.Context, owner, repo, pattern string) error {
	return b.r.delete(ctx, owner, repo, pattern)
}

// TagProtections implements gitoperationslib.Client with tag rulesets
// blocking creation, updates and deletion of matching tags.
func (c *Client) TagProtections() gitoperationslib.TagProtections {
	return tagRulesets{rulesets{c, gh.RulesetTargetTag, "refs/tags/"}}
}

type tagRulesets struct{ r rulesets }

func (t tagRulesets) toRuleset(ctx context.Context, owner string, p gitoperationslib.TagProtection, op string) (gh.RepositoryRuleset, error) {
	rs := t.r.base(p.Pattern)
	rs.Rules.Creation = &gh.EmptyRuleParameters{}
	rs.Rules.Update = &gh.UpdateRuleParameters{}
	rs.Rules.Deletion = &gh.EmptyRuleParameters{}
	teams, err := t.r.bypassTeams(ctx, owner, p.Allowlist, op)
	if err != nil {
		return gh.RepositoryRuleset{}, err
	}
	rs.BypassActors = append(rs.BypassActors, teams...)
	return rs, nil
}

func (t tagRulesets) fromRuleset(ctx context.Context, owner, repo string, rs *gh.RepositoryRuleset) gitoperationslib.TagProtection {
	return gitoperationslib.TagProtection{
		ID:        rs.GetID(),
		Pattern:   t.r.pattern(rs),
		Allowlist: gitoperationslib.Allowlist{Users: []string{}, Teams: t.r.teamSlugs(ctx, owner, repo, rs.BypassActors)},
	}
}

func (t tagRulesets) List(ctx context.Context, owner, repo string) ([]gitoperationslib.TagProtection, error) {
	all, err := t.r.list(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	out := make([]gitoperationslib.TagProtection, len(all))
	for i, rs := range all {
		out[i] = t.fromRuleset(ctx, owner, repo, rs)
	}
	return out, nil
}

func (t tagRulesets) Get(ctx context.Context, owner, repo, pattern string) (*gitoperationslib.TagProtection, error) {
	rs, err := t.r.find(ctx, owner, repo, pattern)
	if err != nil {
		return nil, err
	}
	p := t.fromRuleset(ctx, owner, repo, rs)
	return &p, nil
}

func (t tagRulesets) Create(ctx context.Context, owner, repo string, rule gitoperationslib.TagProtection) (*gitoperationslib.TagProtection, error) {
	if rule.Pattern == "" {
		return nil, errors.New("github: tag_protections.create: pattern is required")
	}
	rs, err := t.toRuleset(ctx, owner, rule, "tag_protections.create")
	if err != nil {
		return nil, err
	}
	out, err := t.r.create(ctx, owner, repo, rs)
	if err != nil {
		return nil, err
	}
	p := t.fromRuleset(ctx, owner, repo, out)
	return &p, nil
}

func (t tagRulesets) Update(ctx context.Context, owner, repo, pattern string, rule gitoperationslib.TagProtection) (*gitoperationslib.TagProtection, error) {
	if rule.Pattern == "" {
		rule.Pattern = pattern
	}
	rs, err := t.toRuleset(ctx, owner, rule, "tag_protections.update")
	if err != nil {
		return nil, err
	}
	out, err := t.r.update(ctx, owner, repo, pattern, rs)
	if err != nil {
		return nil, err
	}
	p := t.fromRuleset(ctx, owner, repo, out)
	return &p, nil
}

func (t tagRulesets) Delete(ctx context.Context, owner, repo, pattern string) error {
	return t.r.delete(ctx, owner, repo, pattern)
}
