package forgejo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/forgejoapi"
)

type labelJSON struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
}

func normalizeColor(c string) string { return strings.ToLower(strings.TrimPrefix(c, "#")) }

func (l *labelJSON) toLabel() gitoperationslib.Label {
	return gitoperationslib.Label{ID: l.ID, Name: l.Name, Color: normalizeColor(l.Color), Description: l.Description}
}

type milestoneJSON struct {
	ID           int64      `json:"id"`
	Title        string     `json:"title"`
	Description  string     `json:"description"`
	State        string     `json:"state"`
	DueOn        *time.Time `json:"due_on"`
	OpenIssues   int        `json:"open_issues"`
	ClosedIssues int        `json:"closed_issues"`
	ClosedAt     *time.Time `json:"closed_at"`
}

func (m *milestoneJSON) toMilestone() gitoperationslib.Milestone {
	return gitoperationslib.Milestone{
		ID: m.ID, Title: m.Title, Description: m.Description, State: gitoperationslib.MilestoneState(m.State),
		DueOn: timeOrZero(m.DueOn), OpenIssues: m.OpenIssues, ClosedIssues: m.ClosedIssues, ClosedAt: timeOrZero(m.ClosedAt),
	}
}

type issueJSON struct {
	Number      int            `json:"number"`
	Title       string         `json:"title"`
	Body        string         `json:"body"`
	State       string         `json:"state"`
	PullRequest *struct{}      `json:"pull_request"`
	User        *loginJSON     `json:"user"`
	Labels      []labelJSON    `json:"labels"`
	Assignees   []loginJSON    `json:"assignees"`
	Milestone   *milestoneJSON `json:"milestone"`
	Comments    int            `json:"comments"`
	Locked      bool           `json:"is_locked"`
	HTMLURL     string         `json:"html_url"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	ClosedAt    *time.Time     `json:"closed_at"`
}

func (i *issueJSON) toIssue() gitoperationslib.Issue {
	issue := gitoperationslib.Issue{
		Number: i.Number, IsPullRequest: i.PullRequest != nil, Title: i.Title, Body: i.Body, State: gitoperationslib.IssueState(i.State),
		Author: i.User.login(), Labels: []string{}, Assignees: []string{}, Comments: i.Comments, Locked: i.Locked,
		HTMLURL: i.HTMLURL, CreatedAt: i.CreatedAt, UpdatedAt: i.UpdatedAt, ClosedAt: timeOrZero(i.ClosedAt),
	}
	for _, l := range i.Labels {
		issue.Labels = append(issue.Labels, l.Name)
	}
	for _, a := range i.Assignees {
		issue.Assignees = append(issue.Assignees, a.Login)
	}
	if i.Milestone != nil {
		m := i.Milestone.toMilestone()
		issue.Milestone = &m
	}
	return issue
}

type commentJSON struct {
	ID        int64      `json:"id"`
	User      *loginJSON `json:"user"`
	Body      string     `json:"body"`
	HTMLURL   string     `json:"html_url"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

func (c *commentJSON) toComment() gitoperationslib.Comment {
	return gitoperationslib.Comment{ID: c.ID, Author: c.User.login(), Body: c.Body, HTMLURL: c.HTMLURL, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}
}

func repoPath(owner, repo string, rest ...string) string {
	return forgejoapi.Path(append([]string{"repos", owner, repo}, rest...)...)
}

// allLabels lists every repository label.
func (c *Client) allLabels(ctx context.Context, owner, repo string) ([]labelJSON, error) {
	return c.labelsAt(ctx, repoPath(owner, repo, "labels"))
}

// orgLabels lists every label of an organisation; a user has none.
func (c *Client) orgLabels(ctx context.Context, owner string) ([]labelJSON, error) {
	all, err := c.labelsAt(ctx, forgejoapi.Path("orgs", owner, "labels"))
	if gitoperationslib.IsNotFound(err) {
		return nil, nil
	}
	return all, err
}

func (c *Client) labelsAt(ctx context.Context, path string) ([]labelJSON, error) {
	var all []labelJSON
	opts := gitoperationslib.ListOptions{Page: 1, PerPage: 50}
	for {
		var page []labelJSON
		resp, err := c.api.Do(ctx, http.MethodGet, path, forgejoapi.ListQuery(opts), nil, &page)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		next := resp.Next(len(page))
		if next == 0 {
			return all, nil
		}
		opts.Page = next
	}
}

// labelsNamed groups labels by name: Forgejo lets several share one.
func labelsNamed(all []labelJSON) map[string][]int64 {
	byName := map[string][]int64{}
	for _, l := range all {
		byName[l.Name] = append(byName[l.Name], l.ID)
	}
	return byName
}

func ambiguousLabel(name string, ids []int64) error {
	return fmt.Errorf("forgejo: label %q, IDs %v: %w", name, ids, gitoperationslib.ErrAmbiguousName)
}

func labelNotFound(owner, repo, name string) error {
	return &gitoperationslib.APIError{
		Backend: gitoperationslib.BackendForgejo, StatusCode: http.StatusNotFound,
		Message: fmt.Sprintf("label %q not found in %s/%s", name, owner, repo),
	}
}

// usableLabels maps names to the labels an issue of this repository can use. Names
// not found in the repository are looked up in its organisation, so that the usual
// case costs a single listing; a repository label wins over an organisation's.
func (c *Client) usableLabels(ctx context.Context, owner, repo string, names []string) (map[string][]int64, error) {
	all, err := c.allLabels(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	byName := labelsNamed(all)
	if slices.ContainsFunc(names, func(n string) bool { _, ok := byName[n]; return !ok }) {
		org, err := c.orgLabels(ctx, owner)
		if err != nil {
			return nil, err
		}
		for name, ids := range labelsNamed(org) {
			if _, ok := byName[name]; !ok {
				byName[name] = ids
			}
		}
	}
	return byName, nil
}

// labelIDs resolves label names; Forgejo's issue endpoints take IDs.
func (c *Client) labelIDs(ctx context.Context, owner, repo string, names []string) ([]int64, error) {
	out := make([]int64, 0, len(names))
	if len(names) == 0 {
		return out, nil
	}
	byName, err := c.usableLabels(ctx, owner, repo, names)
	if err != nil {
		return nil, err
	}
	for _, n := range names {
		switch ids := byName[n]; len(ids) {
		case 0:
			return nil, labelNotFound(owner, repo, n)
		case 1:
			out = append(out, ids[0])
		default:
			return nil, ambiguousLabel(n, ids)
		}
	}
	return out, nil
}

// refusePull stops what would act on a pull request found under an issue's number.
func (c *Client) refusePull(ctx context.Context, owner, repo string, number int, op string) error {
	issue, err := issues{c}.Get(ctx, owner, repo, number)
	if err != nil {
		return err
	}
	if issue.IsPullRequest {
		return fmt.Errorf("forgejo: %s: %d: %w", op, number, gitoperationslib.ErrIsPullRequest)
	}
	return nil
}

// Issues implements gitoperationslib.Client.
func (c *Client) Issues() gitoperationslib.Issues { return issues{c} }

type issues struct{ c *Client }

func (s issues) List(ctx context.Context, owner, repo string, opts gitoperationslib.ListIssuesOptions) (gitoperationslib.Page[gitoperationslib.Issue], error) {
	q := forgejoapi.ListQuery(opts.ListOptions)
	q.Set("type", "issues")
	if opts.State != "" {
		q.Set("state", string(opts.State))
	}
	if len(opts.Labels) > 0 {
		// The server ignores a name it does not know, which would list issues
		// without that label.
		byName, err := s.c.usableLabels(ctx, owner, repo, opts.Labels)
		if err != nil {
			return gitoperationslib.Page[gitoperationslib.Issue]{}, err
		}
		if slices.ContainsFunc(opts.Labels, func(n string) bool { return len(byName[n]) == 0 }) {
			return gitoperationslib.Page[gitoperationslib.Issue]{Items: []gitoperationslib.Issue{}}, nil
		}
		q.Set("labels", strings.Join(opts.Labels, ","))
	}
	var out []issueJSON
	resp, err := s.c.api.Do(ctx, http.MethodGet, repoPath(owner, repo, "issues"), q, nil, &out)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Issue]{}, err
	}
	return toPage(resp, out, (*issueJSON).toIssue), nil
}

func (s issues) Get(ctx context.Context, owner, repo string, number int) (*gitoperationslib.Issue, error) {
	var out issueJSON
	if _, err := s.c.api.Do(ctx, http.MethodGet, repoPath(owner, repo, "issues", strconv.Itoa(number)), nil, nil, &out); err != nil {
		return nil, err
	}
	issue := out.toIssue()
	return &issue, nil
}

func (s issues) Create(ctx context.Context, owner, repo string, opts gitoperationslib.CreateIssueOptions) (*gitoperationslib.Issue, error) {
	if opts.Title == "" {
		return nil, errors.New("forgejo: issues.create: title is required")
	}
	ids, err := s.c.labelIDs(ctx, owner, repo, opts.Labels)
	if err != nil {
		return nil, err
	}
	body := struct {
		Title     string   `json:"title"`
		Body      string   `json:"body,omitempty"`
		Labels    []int64  `json:"labels,omitempty"`
		Assignees []string `json:"assignees,omitempty"`
		Milestone int64    `json:"milestone,omitempty"`
	}{opts.Title, opts.Body, ids, opts.Assignees, opts.Milestone}
	var out issueJSON
	if _, err := s.c.api.Do(ctx, http.MethodPost, repoPath(owner, repo, "issues"), nil, body, &out); err != nil {
		return nil, err
	}
	issue := out.toIssue()
	return &issue, nil
}

func (s issues) Edit(ctx context.Context, owner, repo string, number int, opts gitoperationslib.EditIssueOptions) (*gitoperationslib.Issue, error) {
	n := strconv.Itoa(number)
	body := struct {
		Title     *string   `json:"title,omitempty"`
		Body      *string   `json:"body,omitempty"`
		State     *string   `json:"state,omitempty"`
		Assignees *[]string `json:"assignees,omitempty"`
		Milestone *int64    `json:"milestone,omitempty"`
	}{Title: opts.Title, Body: opts.Body, Milestone: opts.Milestone}
	if opts.State != nil {
		if *opts.State != gitoperationslib.IssueStateOpen && *opts.State != gitoperationslib.IssueStateClosed {
			return nil, fmt.Errorf("forgejo: issues.edit: invalid state %q", *opts.State)
		}
		if err := s.c.refusePull(ctx, owner, repo, number, "issues.edit"); err != nil {
			return nil, err
		}
		body.State = (*string)(opts.State)
	}
	if opts.Assignees != nil {
		a := nonNil(*opts.Assignees)
		body.Assignees = &a
	}
	if opts.Labels != nil {
		// Labels are replaced through their own endpoint.
		ids, err := s.c.labelIDs(ctx, owner, repo, *opts.Labels)
		if err != nil {
			return nil, err
		}
		if _, err := s.c.api.Do(ctx, http.MethodPut, repoPath(owner, repo, "issues", n, "labels"), nil,
			map[string][]int64{"labels": ids}, nil); err != nil {
			return nil, err
		}
	}
	var out issueJSON
	if _, err := s.c.api.Do(ctx, http.MethodPatch, repoPath(owner, repo, "issues", n), nil, body, &out); err != nil {
		return nil, err
	}
	issue := out.toIssue()
	return &issue, nil
}

func (s issues) Delete(ctx context.Context, owner, repo string, number int) error {
	if err := s.c.refusePull(ctx, owner, repo, number, "issues.delete"); err != nil {
		return err
	}
	_, err := s.c.api.Do(ctx, http.MethodDelete, repoPath(owner, repo, "issues", strconv.Itoa(number)), nil, nil, nil)
	return err
}

func (s issues) AddLabels(ctx context.Context, owner, repo string, number int, labels []string) error {
	ids, err := s.c.labelIDs(ctx, owner, repo, labels)
	if err != nil {
		return err
	}
	_, err = s.c.api.Do(ctx, http.MethodPost, repoPath(owner, repo, "issues", strconv.Itoa(number), "labels"), nil,
		map[string][]int64{"labels": ids}, nil)
	return err
}

func (s issues) RemoveLabel(ctx context.Context, owner, repo string, number int, label string) error {
	ids, err := s.c.labelIDs(ctx, owner, repo, []string{label})
	if err != nil {
		return err
	}
	_, err = s.c.api.Do(ctx, http.MethodDelete, repoPath(owner, repo, "issues", strconv.Itoa(number), "labels", strconv.FormatInt(ids[0], 10)), nil, nil, nil)
	if gitoperationslib.StatusCode(err) != http.StatusUnprocessableEntity {
		return err
	}
	// Forgejo refuses an organisation label's ID here but takes its name
	// (checked on 15.0.11). A numeric name could be taken for another label's ID.
	if _, numeric := strconv.Atoi(label); numeric == nil {
		return err
	}
	_, err = s.c.api.Do(ctx, http.MethodDelete, repoPath(owner, repo, "issues", strconv.Itoa(number), "labels", label), nil, nil, nil)
	return err
}

// ListComments returns all comments at once: Forgejo does not paginate them.
func (s issues) ListComments(ctx context.Context, owner, repo string, number int, _ gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Comment], error) {
	var out []commentJSON
	resp, err := s.c.api.Do(ctx, http.MethodGet, repoPath(owner, repo, "issues", strconv.Itoa(number), "comments"), nil, nil, &out)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Comment]{}, err
	}
	return toPage(resp, out, (*commentJSON).toComment), nil
}

func (s issues) CreateComment(ctx context.Context, owner, repo string, number int, body string) (*gitoperationslib.Comment, error) {
	var out commentJSON
	if _, err := s.c.api.Do(ctx, http.MethodPost, repoPath(owner, repo, "issues", strconv.Itoa(number), "comments"), nil,
		map[string]string{"body": body}, &out); err != nil {
		return nil, err
	}
	c := out.toComment()
	return &c, nil
}

func (s issues) EditComment(ctx context.Context, owner, repo string, id int64, body string) (*gitoperationslib.Comment, error) {
	var out commentJSON
	if _, err := s.c.api.Do(ctx, http.MethodPatch, repoPath(owner, repo, "issues", "comments", strconv.FormatInt(id, 10)), nil,
		map[string]string{"body": body}, &out); err != nil {
		return nil, err
	}
	c := out.toComment()
	return &c, nil
}

func (s issues) DeleteComment(ctx context.Context, owner, repo string, id int64) error {
	_, err := s.c.api.Do(ctx, http.MethodDelete, repoPath(owner, repo, "issues", "comments", strconv.FormatInt(id, 10)), nil, nil, nil)
	return err
}

// Labels implements gitoperationslib.Client.
func (c *Client) Labels() gitoperationslib.Labels { return labels{c} }

type labels struct{ c *Client }

func (l labels) List(ctx context.Context, owner, repo string, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Label], error) {
	var out []labelJSON
	resp, err := l.c.api.Do(ctx, http.MethodGet, repoPath(owner, repo, "labels"), forgejoapi.ListQuery(opts), nil, &out)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Label]{}, err
	}
	return toPage(resp, out, (*labelJSON).toLabel), nil
}

func (l labels) find(ctx context.Context, owner, repo, name string) (*labelJSON, error) {
	all, err := l.c.allLabels(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	ids := labelsNamed(all)[name]
	if len(ids) > 1 {
		return nil, ambiguousLabel(name, ids)
	}
	for i := range all {
		if all[i].Name == name {
			return &all[i], nil
		}
	}
	return nil, labelNotFound(owner, repo, name)
}

func (l labels) Get(ctx context.Context, owner, repo, name string) (*gitoperationslib.Label, error) {
	j, err := l.find(ctx, owner, repo, name)
	if err != nil {
		return nil, err
	}
	label := j.toLabel()
	return &label, nil
}

func (l labels) Create(ctx context.Context, owner, repo string, label gitoperationslib.Label) (*gitoperationslib.Label, error) {
	return l.c.createLabelAt(ctx, repoPath(owner, repo, "labels"), label)
}

// createLabelAt creates a label below base, a repository's or an organisation's
// labels. Forgejo would accept a name in use and keep both.
func (c *Client) createLabelAt(ctx context.Context, base string, label gitoperationslib.Label) (*gitoperationslib.Label, error) {
	if label.Name == "" {
		return nil, errors.New("forgejo: labels.create: name is required")
	}
	all, err := c.labelsAt(ctx, base)
	if err != nil {
		return nil, err
	}
	if len(labelsNamed(all)[label.Name]) > 0 {
		return nil, fmt.Errorf("forgejo: labels.create: %q: %w", label.Name, gitoperationslib.ErrNameTaken)
	}
	color := normalizeColor(label.Color)
	if color == "" {
		color = "ededed" // Forgejo requires a colour
	}
	body := map[string]string{"name": label.Name, "color": "#" + color, "description": label.Description}
	var out labelJSON
	if _, err := c.api.Do(ctx, http.MethodPost, base, nil, body, &out); err != nil {
		return nil, err
	}
	created := out.toLabel()
	return &created, nil
}

func (l labels) Edit(ctx context.Context, owner, repo, name string, opts gitoperationslib.EditLabelOptions) (*gitoperationslib.Label, error) {
	cur, err := l.find(ctx, owner, repo, name)
	if err != nil {
		return nil, err
	}
	return l.c.editLabelAt(ctx, repoPath(owner, repo, "labels"), cur.ID, opts)
}

// editLabelAt edits a label below base. A rename is checked against the others, as
// Forgejo would accept a name in use and keep both.
func (c *Client) editLabelAt(ctx context.Context, base string, id int64, opts gitoperationslib.EditLabelOptions) (*gitoperationslib.Label, error) {
	body := map[string]string{}
	if opts.Name != nil {
		all, err := c.labelsAt(ctx, base)
		if err != nil {
			return nil, err
		}
		if slices.ContainsFunc(all, func(l labelJSON) bool { return l.Name == *opts.Name && l.ID != id }) {
			return nil, fmt.Errorf("forgejo: labels.edit: %q: %w", *opts.Name, gitoperationslib.ErrNameTaken)
		}
		body["name"] = *opts.Name
	}
	if opts.Color != nil {
		body["color"] = "#" + normalizeColor(*opts.Color)
	}
	if opts.Description != nil {
		body["description"] = *opts.Description
	}
	var out labelJSON
	if _, err := c.api.Do(ctx, http.MethodPatch, base+"/"+strconv.FormatInt(id, 10), nil, body, &out); err != nil {
		return nil, err
	}
	edited := out.toLabel()
	return &edited, nil
}

func (l labels) Delete(ctx context.Context, owner, repo, name string) error {
	cur, err := l.find(ctx, owner, repo, name)
	if err != nil {
		return err
	}
	_, err = l.c.api.Do(ctx, http.MethodDelete, repoPath(owner, repo, "labels", strconv.FormatInt(cur.ID, 10)), nil, nil, nil)
	return err
}

// Milestones implements gitoperationslib.Client.
func (c *Client) Milestones() gitoperationslib.Milestones { return milestones{c} }

type milestones struct{ c *Client }

func (m milestones) List(ctx context.Context, owner, repo string, state gitoperationslib.MilestoneState, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Milestone], error) {
	q := forgejoapi.ListQuery(opts)
	if state != "" {
		q.Set("state", string(state))
	}
	var out []milestoneJSON
	resp, err := m.c.api.Do(ctx, http.MethodGet, repoPath(owner, repo, "milestones"), q, nil, &out)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Milestone]{}, err
	}
	return toPage(resp, out, (*milestoneJSON).toMilestone), nil
}

func (m milestones) Get(ctx context.Context, owner, repo string, id int64) (*gitoperationslib.Milestone, error) {
	var out milestoneJSON
	if _, err := m.c.api.Do(ctx, http.MethodGet, repoPath(owner, repo, "milestones", strconv.FormatInt(id, 10)), nil, nil, &out); err != nil {
		return nil, err
	}
	ms := out.toMilestone()
	return &ms, nil
}

func (m milestones) Create(ctx context.Context, owner, repo string, opts gitoperationslib.CreateMilestoneOptions) (*gitoperationslib.Milestone, error) {
	if opts.Title == "" {
		return nil, errors.New("forgejo: milestones.create: title is required")
	}
	body := struct {
		Title       string     `json:"title"`
		Description string     `json:"description,omitempty"`
		DueOn       *time.Time `json:"due_on,omitempty"`
	}{Title: opts.Title, Description: opts.Description}
	if !opts.DueOn.IsZero() {
		body.DueOn = &opts.DueOn
	}
	var out milestoneJSON
	if err := m.checkTitle(ctx, owner, repo, opts.Title, 0); err != nil {
		return nil, err
	}
	if _, err := m.c.api.Do(ctx, http.MethodPost, repoPath(owner, repo, "milestones"), nil, body, &out); err != nil {
		return nil, err
	}
	ms := out.toMilestone()
	return &ms, nil
}

// checkTitle refuses a title another milestone has: Forgejo would keep both.
func (m milestones) checkTitle(ctx context.Context, owner, repo, title string, id int64) error {
	list := func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Milestone], error) {
		return m.List(ctx, owner, repo, gitoperationslib.MilestoneStateAll, o)
	}
	for ms, err := range gitoperationslib.All(ctx, gitoperationslib.ListOptions{}, list) {
		if err != nil {
			return err
		}
		if ms.Title == title && ms.ID != id {
			return fmt.Errorf("forgejo: milestones: %q: %w", title, gitoperationslib.ErrNameTaken)
		}
	}
	return nil
}

func (m milestones) Edit(ctx context.Context, owner, repo string, id int64, opts gitoperationslib.EditMilestoneOptions) (*gitoperationslib.Milestone, error) {
	switch {
	case opts.ClearDueOn && opts.DueOn != nil:
		return nil, errors.New("forgejo: milestones.edit: ClearDueOn and DueOn cannot go together")
	case opts.ClearDueOn:
		// A null or zero due_on keeps the date, and an empty one is refused
		// (checked on 15.0.11 and 17.0.1).
		return nil, gitoperationslib.NotSupported(gitoperationslib.BackendForgejo, "milestones.edit", "removing a milestone's due date")
	}
	if opts.Title != nil {
		if err := m.checkTitle(ctx, owner, repo, *opts.Title, id); err != nil {
			return nil, err
		}
	}
	body := struct {
		Title       *string    `json:"title,omitempty"`
		Description *string    `json:"description,omitempty"`
		State       *string    `json:"state,omitempty"`
		DueOn       *time.Time `json:"due_on,omitempty"`
	}{Title: opts.Title, Description: opts.Description, State: (*string)(opts.State), DueOn: opts.DueOn}
	var out milestoneJSON
	if _, err := m.c.api.Do(ctx, http.MethodPatch, repoPath(owner, repo, "milestones", strconv.FormatInt(id, 10)), nil, body, &out); err != nil {
		return nil, err
	}
	ms := out.toMilestone()
	return &ms, nil
}

func (m milestones) Delete(ctx context.Context, owner, repo string, id int64) error {
	_, err := m.c.api.Do(ctx, http.MethodDelete, repoPath(owner, repo, "milestones", strconv.FormatInt(id, 10)), nil, nil, nil)
	return err
}

var _ gitoperationslib.IssueLocks = (*Client)(nil)

// issueLocksVersion is the first release with /issues/{index}/lock.
var issueLocksVersion = Version{17, 0, 0}

func (c *Client) issueLock(ctx context.Context, method, op, owner, repo string, number int, body any) error {
	if c.version.Compare(issueLocksVersion) < 0 {
		return gitoperationslib.NotSupported(gitoperationslib.BackendForgejo, op, "locking issues before Forgejo "+issueLocksVersion.String())
	}
	_, err := c.api.Do(ctx, method, forgejoapi.Path("repos", owner, repo, "issues", strconv.Itoa(number), "lock"), nil, body, nil)
	// The server only answers 409 when the issue is already in that state.
	var apiErr *gitoperationslib.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusConflict {
		return nil
	}
	return err
}

func (c *Client) LockIssue(ctx context.Context, owner, repo string, number int, reason gitoperationslib.LockReason) error {
	if reason == "" {
		return errors.New("forgejo: issues.lock: reason is required")
	}
	return c.issueLock(ctx, http.MethodPut, "issues.lock", owner, repo, number, map[string]string{"reason": string(reason)})
}

func (c *Client) UnlockIssue(ctx context.Context, owner, repo string, number int) error {
	return c.issueLock(ctx, http.MethodDelete, "issues.unlock", owner, repo, number, nil)
}
