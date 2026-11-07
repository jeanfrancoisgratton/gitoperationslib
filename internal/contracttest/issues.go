package contracttest

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// Issues runs the issue, label, milestone and comment checks in a throwaway
// repository.
func Issues(t *testing.T, env Env) {
	if unsupported(t, env, gitoperationslib.CapIssues, func() error {
		bg := context.Background()
		_, err := env.Client.Issues().Create(bg, env.Org, "any", gitoperationslib.CreateIssueOptions{Title: "x"})
		if errors.Is(err, gitoperationslib.ErrNotSupported) {
			_, err = env.Client.Labels().Create(bg, env.Org, "any", gitoperationslib.Label{Name: "x"})
		}
		if errors.Is(err, gitoperationslib.ErrNotSupported) {
			_, err = env.Client.Milestones().Create(bg, env.Org, "any", gitoperationslib.CreateMilestoneOptions{Title: "x"})
		}
		return err
	}) {
		return
	}
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

	// A pull request found under an issue's number: told apart, and protected.
	t.Run("pull request number", func(t *testing.T) {
		got, err := issues.Get(ctx, owner, *repo, pr.Number)
		check(err)
		if !got.IsPullRequest {
			t.Errorf("Get(pull request) = %+v", got)
		}
		plain, err := issues.Get(ctx, owner, *repo, issue.Number)
		check(err)
		if plain.IsPullRequest {
			t.Errorf("Get(issue) = %+v", plain)
		}
		if err := issues.Delete(ctx, owner, *repo, pr.Number); !errors.Is(err, gitoperationslib.ErrIsPullRequest) {
			t.Errorf("Delete: err = %v, want ErrIsPullRequest", err)
		}
		if _, err := issues.Edit(ctx, owner, *repo, pr.Number, gitoperationslib.EditIssueOptions{State: gitoperationslib.Ptr(gitoperationslib.IssueStateClosed)}); !errors.Is(err, gitoperationslib.ErrIsPullRequest) {
			t.Errorf("Edit(State): err = %v, want ErrIsPullRequest", err)
		}
		still, err := env.Client.PullRequests().Get(ctx, owner, *repo, pr.Number)
		check(err)
		if still.State != gitoperationslib.PRStateOpen {
			t.Errorf("pull request after the refusals = %+v", still)
		}
		// What is legitimate on a pull request keeps working.
		if _, err := issues.Edit(ctx, owner, *repo, pr.Number, gitoperationslib.EditIssueOptions{Labels: &[]string{"bug"}}); err != nil {
			t.Errorf("Edit(Labels): %v", err)
		}
	})

	t.Run("names", func(t *testing.T) {
		if _, err := labels.Create(ctx, owner, *repo, gitoperationslib.Label{Name: "bug", Color: "ededed"}); !errors.Is(err, gitoperationslib.ErrNameTaken) {
			t.Errorf("Create(label in use): err = %v, want ErrNameTaken", err)
		}
		if _, err := labels.Edit(ctx, owner, *repo, "needs-triage", gitoperationslib.EditLabelOptions{Name: gitoperationslib.Ptr("bug")}); !errors.Is(err, gitoperationslib.ErrNameTaken) {
			t.Errorf("rename a label onto one in use: err = %v, want ErrNameTaken", err)
		}
		if _, err := milestones.Create(ctx, owner, *repo, gitoperationslib.CreateMilestoneOptions{Title: "v1.0"}); !errors.Is(err, gitoperationslib.ErrNameTaken) {
			t.Errorf("Create(milestone in use): err = %v, want ErrNameTaken", err)
		}
		other, err := milestones.Create(ctx, owner, *repo, gitoperationslib.CreateMilestoneOptions{Title: "v2.0"})
		check(err)
		if _, err := milestones.Edit(ctx, owner, *repo, other.ID, gitoperationslib.EditMilestoneOptions{Title: gitoperationslib.Ptr("v1.0")}); !errors.Is(err, gitoperationslib.ErrNameTaken) {
			t.Errorf("rename a milestone onto one in use: err = %v, want ErrNameTaken", err)
		}
		check(milestones.Delete(ctx, owner, *repo, other.ID))
	})

	t.Run("unknown labels", func(t *testing.T) {
		for _, names := range [][]string{{"no-such-label"}, {"bug", "no-such-label"}} {
			page, err := issues.List(ctx, owner, *repo, gitoperationslib.ListIssuesOptions{State: gitoperationslib.IssueStateAll, Labels: names})
			check(err)
			if len(page.Items) != 0 {
				t.Errorf("List(%v) = %+v, want nothing", names, page.Items)
			}
		}
		if _, err := issues.Create(ctx, owner, *repo, gitoperationslib.CreateIssueOptions{Title: "x", Labels: []string{"no-such-label"}}); !gitoperationslib.IsNotFound(err) {
			t.Errorf("Create: err = %v, want 404", err)
		}
		if err := issues.AddLabels(ctx, owner, *repo, issue.Number, []string{"no-such-label"}); !gitoperationslib.IsNotFound(err) {
			t.Errorf("AddLabels: err = %v, want 404", err)
		}
		if _, err := issues.Edit(ctx, owner, *repo, issue.Number, gitoperationslib.EditIssueOptions{Labels: &[]string{"no-such-label"}}); !gitoperationslib.IsNotFound(err) {
			t.Errorf("Edit: err = %v, want 404", err)
		}
		if _, err := labels.Get(ctx, owner, *repo, "no-such-label"); !gitoperationslib.IsNotFound(err) {
			t.Errorf("the refusals left a label behind: err = %v", err)
		}
	})

	t.Run("clearing a due date", func(t *testing.T) {
		due, err := milestones.Create(ctx, owner, *repo, gitoperationslib.CreateMilestoneOptions{Title: "due", DueOn: time.Now().Add(240 * time.Hour)})
		check(err)
		now := time.Now()
		if _, err := milestones.Edit(ctx, owner, *repo, due.ID, gitoperationslib.EditMilestoneOptions{ClearDueOn: true, DueOn: &now}); err == nil {
			t.Error("ClearDueOn and DueOn together accepted")
		}
		cleared, err := milestones.Edit(ctx, owner, *repo, due.ID, gitoperationslib.EditMilestoneOptions{ClearDueOn: true})
		if env.Client.Backend() == gitoperationslib.BackendForgejo {
			if !errors.Is(err, gitoperationslib.ErrNotSupported) {
				t.Errorf("Forgejo: err = %v, want ErrNotSupported", err)
			}
		} else {
			check(err)
			if !cleared.DueOn.IsZero() {
				t.Errorf("cleared = %+v", cleared)
			}
		}
		check(milestones.Delete(ctx, owner, *repo, due.ID))
	})

	// Delete everything. GitHub answers 410 for a deleted issue, which counts.
	check(issues.Delete(ctx, owner, *repo, issue.Number))
	if _, err := issues.Get(ctx, owner, *repo, issue.Number); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Get after delete: err = %v, want not found", err)
	}
	check(milestones.Delete(ctx, owner, *repo, ms.ID))
	check(labels.Delete(ctx, owner, *repo, "bug"))
	if _, err := labels.Get(ctx, owner, *repo, "bug"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Get label after delete: err = %v, want 404", err)
	}

	if l, ok := env.Client.(gitoperationslib.IssueLocks); ok {
		t.Run("locks", func(t *testing.T) {
			issue, err := issues.Create(ctx, owner, *repo, gitoperationslib.CreateIssueOptions{Title: "To lock"})
			check(err)
			locked := func() bool {
				t.Helper()
				got, err := issues.Get(ctx, owner, *repo, issue.Number)
				check(err)
				return got.Locked
			}
			err = l.LockIssue(ctx, owner, *repo, issue.Number, gitoperationslib.LockReasonResolved)
			if errors.Is(err, gitoperationslib.ErrNotSupported) {
				t.Skip(err)
			}
			check(err)
			if !locked() {
				t.Error("not locked after LockIssue")
			}
			check(l.LockIssue(ctx, owner, *repo, issue.Number, gitoperationslib.LockReasonResolved))
			check(l.UnlockIssue(ctx, owner, *repo, issue.Number))
			if locked() {
				t.Error("locked after UnlockIssue")
			}
			check(l.UnlockIssue(ctx, owner, *repo, issue.Number))
		})
	}
}
