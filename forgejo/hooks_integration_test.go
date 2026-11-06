//go:build integration

package forgejo

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/contracttest"
)

// TestIntegrationHookEditKeeps guards against what a PATCH does on its own: without
// events and a branch filter, it resets them to "push" and none.
func TestIntegrationHookEditKeeps(t *testing.T) {
	ctx, c := adminEnv(t)
	hooks := c.UserWebhooks()
	create := func(events ...gitoperationslib.HookEvent) *gitoperationslib.Webhook {
		t.Helper()
		hook, err := hooks.Create(ctx, gitoperationslib.CreateWebhookOptions{
			URL: "https://example.invalid/keep", Events: events, BranchFilter: "main", AuthorizationHeader: "Bearer x",
		})
		if err != nil {
			t.Fatalf("Create(%v): %v", events, err)
		}
		t.Cleanup(func() { _ = hooks.Delete(context.Background(), hook.ID) })
		return hook
	}
	for _, events := range [][]gitoperationslib.HookEvent{
		{gitoperationslib.HookEventPush, gitoperationslib.HookEventRelease},
		{gitoperationslib.HookEventPullRequestReview},
		{gitoperationslib.HookEventIssues},
		{gitoperationslib.HookEventPullRequest},
		{"issues_only"},
		{"pull_request_only", "issue_label"},
		{"pull_request_comment"},
	} {
		hook := create(events...)
		edited, err := hooks.Edit(ctx, hook.ID, gitoperationslib.EditWebhookOptions{Active: gitoperationslib.Ptr(false)})
		if err != nil {
			t.Fatalf("Edit(%v): %v", events, err)
		}
		if edited.Active || edited.BranchFilter != "main" || !edited.HasAuthorizationHeader || !slices.Equal(sortedEvents(edited), sortedEvents(hook)) {
			t.Errorf("Edit(%v) = %+v; created as %+v", events, edited, hook)
		}
	}

	// Naming a field still changes it.
	hook := create(gitoperationslib.HookEventPush)
	edited, err := hooks.Edit(ctx, hook.ID, gitoperationslib.EditWebhookOptions{Events: []gitoperationslib.HookEvent{gitoperationslib.HookEventRelease}, BranchFilter: gitoperationslib.Ptr("")})
	if err != nil {
		t.Fatal(err)
	}
	if edited.BranchFilter != "" || !slices.Equal(edited.Events, []gitoperationslib.HookEvent{gitoperationslib.HookEventRelease}) {
		t.Errorf("edited = %+v", edited)
	}

	// What the API ignores is refused, on every version.
	if _, err := hooks.Edit(ctx, hook.ID, gitoperationslib.EditWebhookOptions{Secret: gitoperationslib.Ptr("new")}); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("changing the secret: %v", err)
	}
	if _, err := hooks.Edit(ctx, hook.ID, gitoperationslib.EditWebhookOptions{AuthorizationHeader: gitoperationslib.Ptr("")}); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("removing the header: %v", err)
	}
}

func sortedEvents(h *gitoperationslib.Webhook) []gitoperationslib.HookEvent {
	out := slices.Clone(h.Events)
	slices.Sort(out)
	return out
}

func TestIntegrationHookTypes(t *testing.T) {
	ctx, c := adminEnv(t)
	hooks := c.UserWebhooks()
	push := []gitoperationslib.HookEvent{gitoperationslib.HookEventPush}
	hook, err := hooks.Create(ctx, gitoperationslib.CreateWebhookOptions{
		Type: gitoperationslib.HookTypeSlack, URL: "https://example.invalid/slack", Events: push,
		Slack: &gitoperationslib.SlackHookSettings{Channel: "#builds", Username: "bot", Color: "#fff"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = hooks.Delete(context.Background(), hook.ID) })
	if hook.Type != gitoperationslib.HookTypeSlack || hook.Slack == nil || hook.Slack.Channel != "#builds" || hook.Slack.Username != "bot" || hook.Slack.Color != "#fff" {
		t.Errorf("created = %+v", hook)
	}
	moved := "https://example.invalid/moved"
	edited, err := hooks.Edit(ctx, hook.ID, gitoperationslib.EditWebhookOptions{URL: &moved})
	if err != nil {
		t.Fatal(err)
	}
	if edited.URL != moved || edited.Slack == nil || edited.Slack.Channel != "#builds" || edited.Slack.Username != "bot" {
		t.Errorf("after a URL change = %+v", edited)
	}
	if edited, err = hooks.Edit(ctx, hook.ID, gitoperationslib.EditWebhookOptions{Slack: &gitoperationslib.SlackHookSettings{Channel: "#other"}}); err != nil {
		t.Fatal(err)
	}
	if edited.Slack == nil || *edited.Slack != (gitoperationslib.SlackHookSettings{Channel: "#other"}) {
		t.Errorf("after a settings change = %+v", edited.Slack)
	}

	plain, err := hooks.Create(ctx, gitoperationslib.CreateWebhookOptions{Type: gitoperationslib.HookTypeDiscord, URL: "https://example.invalid/discord", Events: push})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = hooks.Delete(context.Background(), plain.ID) })
	if plain.Type != gitoperationslib.HookTypeDiscord || plain.Slack != nil {
		t.Errorf("Discord = %+v", plain)
	}
	if _, err := hooks.Edit(ctx, plain.ID, gitoperationslib.EditWebhookOptions{Slack: &gitoperationslib.SlackHookSettings{Channel: "#c"}}); err == nil {
		t.Error("Slack settings on a Discord hook accepted")
	}
}

// TestIntegrationWorkflowHookEvents checks the subscription on each version.
// Deliveries need a runner: see the fixtures in webhook/testdata.
func TestIntegrationWorkflowHookEvents(t *testing.T) {
	ctx, c := adminEnv(t)
	hooks := c.UserWebhooks()
	create := func(events ...gitoperationslib.HookEvent) (*gitoperationslib.Webhook, error) {
		hook, err := hooks.Create(ctx, gitoperationslib.CreateWebhookOptions{URL: "https://example.invalid/wf", Events: events})
		if hook != nil {
			t.Cleanup(func() { _ = hooks.Delete(context.Background(), hook.ID) })
		}
		return hook, err
	}
	all, err := create(gitoperationslib.HookEventAll)
	if err != nil {
		t.Fatal(err)
	}
	if c.Version().Compare(workflowEventsVersion) < 0 {
		if _, err := create(gitoperationslib.HookEventWorkflowRun); !errors.Is(err, gitoperationslib.ErrNotSupported) {
			t.Errorf("before 17: %v", err)
		}
		if !slices.Contains(all.Events, "action_run_success") || slices.Contains(all.Events, gitoperationslib.HookEventWorkflowRun) {
			t.Errorf("everything = %v", all.Events)
		}
		return
	}
	if slices.Contains(all.Events, "action_run_success") || !slices.Contains(all.Events, gitoperationslib.HookEventWorkflowRun) || !slices.Contains(all.Events, gitoperationslib.HookEventWorkflowJob) {
		t.Errorf("everything = %v", all.Events)
	}
	hook, err := create(gitoperationslib.HookEventWorkflowRun, "workflow_job_failure")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(sortedEvents(hook), []gitoperationslib.HookEvent{"workflow_job_failure", gitoperationslib.HookEventWorkflowRun}) {
		t.Errorf("events = %v", hook.Events)
	}
	edited, err := hooks.Edit(ctx, hook.ID, gitoperationslib.EditWebhookOptions{Active: gitoperationslib.Ptr(false)})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(sortedEvents(edited), sortedEvents(hook)) {
		t.Errorf("after an edit = %v", edited.Events)
	}
}

// TestIntegrationSystemWebhookCreate checks the undocumented flag really makes a
// system hook: listed as one, firing for a repository made afterwards, and not
// copied into it as a default hook would be.
func TestIntegrationSystemWebhookCreate(t *testing.T) {
	ctx, c := adminEnv(t)
	secret := "created-system-secret"
	recv := newHookReceiver(t, map[string][]byte{"/created": []byte(secret)})
	system := c.SystemWebhooks()
	hook, err := system.Create(ctx, gitoperationslib.CreateWebhookOptions{
		URL: recv.srv.URL + "/created", Secret: secret, Events: []gitoperationslib.HookEvent{gitoperationslib.HookEventRepository},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = system.Delete(context.Background(), hook.ID) })
	if _, err := system.Get(ctx, hook.ID); err != nil {
		t.Errorf("Get: %v", err)
	}
	if _, err := c.DefaultWebhooks().Get(ctx, hook.ID); err == nil {
		t.Error("listed as a default hook too")
	}

	admin, err := c.currentLogin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	repo := contracttest.RepoPrefix + "syscreate"
	t.Cleanup(func() { c.Repos().Delete(context.Background(), admin, repo) })
	if _, err := c.Repos().Create(ctx, gitoperationslib.CreateRepoOptions{Name: repo}); err != nil {
		t.Fatal(err)
	}
	if ev := recv.wait(t, "/created", "repository"); ev.Repo != admin+"/"+repo {
		t.Errorf("delivery = %+v", ev)
	}
	copies, err := c.Webhooks().List(ctx, gitoperationslib.RepoHooks(admin, repo), gitoperationslib.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(copies.Items, func(h gitoperationslib.Webhook) bool { return h.URL == hook.URL }) {
		t.Errorf("copied into a new repository: %+v", copies.Items)
	}

	// An edit keeps it a system hook.
	edited, err := system.Edit(ctx, hook.ID, gitoperationslib.EditWebhookOptions{Active: gitoperationslib.Ptr(false)})
	if err != nil {
		t.Fatal(err)
	}
	if edited.Active || !slices.Contains(edited.Events, gitoperationslib.HookEventRepository) {
		t.Errorf("edited = %+v", edited)
	}
	if _, err := system.Get(ctx, hook.ID); err != nil {
		t.Errorf("Get after the edit: %v", err)
	}
}
