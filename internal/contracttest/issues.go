package contracttest

import (
	"context"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// Issues runs the issue, label, milestone and comment checks in a throwaway
// repository.
func Issues(t *testing.T, env Env) {
	ctx := testContext(t)
	created, repo := create(t, ctx, env, gitoperationslib.CreateRepoOptions{Owner: env.Org, AutoInit: true})
	owner := env.Org
	issues, labels, milestones := env.Client.Issues(), env.Client.Labels(), env.Client.Milestones()
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	// Labels: colours come back without "#", in lower case.
	bug, err := labels.Create(ctx, owner, *repo, gitoperationslib.Label{Name: "bug", Color: "D73A4A", Description: "Something is broken"})
	check(err)
	if bug.Name != "bug" || bug.Color != "d73a4a" || bug.Description != "Something is broken" {
		t.Errorf("label = %+v", bug)
	}
	_, err = labels.Create(ctx, owner, *repo, gitoperationslib.Label{Name: "triage", Color: "#ededed"})
	check(err)
	renamed, err := labels.Edit(ctx, owner, *repo, "triage", gitoperationslib.EditLabelOptions{
		Name: gitoperationslib.Ptr("needs-triage"), Color: gitoperationslib.Ptr("#FBCA04"),
	})
	check(err)
	if renamed.Name != "needs-triage" || renamed.Color != "fbca04" {
		t.Errorf("renamed label = %+v", renamed)
	}
	if got, err := labels.Get(ctx, owner, *repo, "bug"); err != nil || got.ID != bug.ID {
		t.Errorf("Get label = %+v, %v", got, err)
	}
	var labelNames []string
	for l, err := range gitoperationslib.All(ctx, gitoperationslib.ListOptions{PerPage: 1}, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Label], error) {
		return labels.List(ctx, owner, *repo, o)
	}) {
		check(err)
		labelNames = append(labelNames, l.Name)
	}
	if !slices.Contains(labelNames, "bug") || !slices.Contains(labelNames, "needs-triage") {
		t.Errorf("labels = %v", labelNames)
	}

	// Milestones.
	due := time.Now().Add(30 * 24 * time.Hour).UTC().Truncate(24 * time.Hour)
	ms, err := milestones.Create(ctx, owner, *repo, gitoperationslib.CreateMilestoneOptions{Title: "v1.0", Description: "First release", DueOn: due})
	check(err)
	if ms.ID == 0 || ms.Title != "v1.0" || ms.State != gitoperationslib.MilestoneStateOpen || ms.DueOn.IsZero() {
		t.Errorf("milestone = %+v", ms)
	}
	if got, err := milestones.Get(ctx, owner, *repo, ms.ID); err != nil || got.Title != "v1.0" {
		t.Errorf("Get milestone = %+v, %v", got, err)
	}

	// An issue with labels, assignee and milestone.
	issue, err := issues.Create(ctx, owner, *repo, gitoperationslib.CreateIssueOptions{
		Title: "Something is broken", Body: "Steps to reproduce", Labels: []string{"bug"},
		Assignees: []string{env.Login}, Milestone: ms.ID,
	})
	check(err)
	if issue.Number == 0 || issue.State != gitoperationslib.IssueStateOpen || !slices.Equal(issue.Labels, []string{"bug"}) ||
		len(issue.Assignees) != 1 || issue.Milestone == nil || issue.Milestone.ID != ms.ID {
		t.Errorf("issue = %+v", issue)
	}

	// A pull request shares the number sequence but is not listed as an
	// issue; labels work on it through the issues API.
	check(retryConflict(ctx, t, func() error {
		return env.CreateBranch(ctx, owner, *repo, "feature", "feature.txt", "x\n")
	}))
	pr, err := env.Client.PullRequests().Create(ctx, owner, *repo, gitoperationslib.CreatePROptions{Title: "A pull request", Head: "feature", Base: created.DefaultBranch})
	check(err)
	check(issues.AddLabels(ctx, owner, *repo, pr.Number, []string{"needs-triage"}))
	if !eventually(ctx, func() bool {
		page, err := issues.List(ctx, owner, *repo, gitoperationslib.ListIssuesOptions{State: gitoperationslib.IssueStateAll})
		check(err)
		var numbers []int
		for _, i := range page.Items {
			numbers = append(numbers, i.Number)
		}
		return slices.Equal(numbers, []int{issue.Number})
	}) {
		t.Error("List did not return exactly the issue (pull requests must be excluded)")
	}
	byLabel, err := issues.List(ctx, owner, *repo, gitoperationslib.ListIssuesOptions{Labels: []string{"bug"}})
	check(err)
	if len(byLabel.Items) != 1 {
		t.Errorf("List by label = %+v", byLabel.Items)
	}

	// Edit replaces labels and assignees, and removes the milestone.
	edited, err := issues.Edit(ctx, owner, *repo, issue.Number, gitoperationslib.EditIssueOptions{
		Title: gitoperationslib.Ptr("Something is still broken"), Labels: &[]string{"needs-triage"},
		Assignees: &[]string{}, Milestone: gitoperationslib.Ptr(int64(0)),
	})
	check(err)
	if edited.Title != "Something is still broken" || !slices.Equal(edited.Labels, []string{"needs-triage"}) ||
		len(edited.Assignees) != 0 || edited.Milestone != nil {
		t.Errorf("edited = %+v", edited)
	}
	check(issues.AddLabels(ctx, owner, *repo, issue.Number, []string{"bug"}))
	check(issues.RemoveLabel(ctx, owner, *repo, issue.Number, "needs-triage"))
	got, err := issues.Get(ctx, owner, *repo, issue.Number)
	check(err)
	if !slices.Equal(got.Labels, []string{"bug"}) {
		t.Errorf("labels after add/remove = %v", got.Labels)
	}

	// Comments.
	comment, err := issues.CreateComment(ctx, owner, *repo, issue.Number, "I can reproduce this.")
	check(err)
	if comment.ID == 0 || comment.Body != "I can reproduce this." {
		t.Errorf("comment = %+v", comment)
	}
	editedComment, err := issues.EditComment(ctx, owner, *repo, comment.ID, "I can reproduce this on main.")
	check(err)
	if editedComment.Body != "I can reproduce this on main." {
		t.Errorf("edited comment = %+v", editedComment)
	}
	comments, err := issues.ListComments(ctx, owner, *repo, issue.Number, gitoperationslib.ListOptions{})
	check(err)
	if len(comments.Items) != 1 || comments.Items[0].ID != comment.ID {
		t.Errorf("comments = %+v", comments.Items)
	}
	check(issues.DeleteComment(ctx, owner, *repo, comment.ID))

	// Close the issue and the milestone.
	closed, err := issues.Edit(ctx, owner, *repo, issue.Number, gitoperationslib.EditIssueOptions{State: gitoperationslib.Ptr(gitoperationslib.IssueStateClosed)})
	check(err)
	if closed.State != gitoperationslib.IssueStateClosed || closed.ClosedAt.IsZero() {
		t.Errorf("closed = %+v", closed)
	}
	closedMS, err := milestones.Edit(ctx, owner, *repo, ms.ID, gitoperationslib.EditMilestoneOptions{State: gitoperationslib.Ptr(gitoperationslib.MilestoneStateClosed)})
	check(err)
	if closedMS.State != gitoperationslib.MilestoneStateClosed {
		t.Errorf("closed milestone = %+v", closedMS)
	}
	if page, err := milestones.List(ctx, owner, *repo, gitoperationslib.MilestoneStateClosed, gitoperationslib.ListOptions{}); err != nil || len(page.Items) != 1 {
		t.Errorf("closed milestones = %+v, %v", page, err)
	}

	// Delete everything (GitHub answers 410 Gone for a deleted issue).
	check(issues.Delete(ctx, owner, *repo, issue.Number))
	if _, err := issues.Get(ctx, owner, *repo, issue.Number); !gitoperationslib.IsNotFound(err) && gitoperationslib.StatusCode(err) != http.StatusGone {
		t.Errorf("Get after delete: err = %v, want 404 or 410", err)
	}
	check(milestones.Delete(ctx, owner, *repo, ms.ID))
	check(labels.Delete(ctx, owner, *repo, "bug"))
	if _, err := labels.Get(ctx, owner, *repo, "bug"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Get label after delete: err = %v, want 404", err)
	}
}
