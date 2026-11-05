package contracttest

import (
	"errors"
	"slices"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// HookURL is where test webhooks point. The host does not exist, so
// deliveries simply fail.
const HookURL = "https://gitopslib-hook.example.com/hook"

// Webhooks runs the repository webhook checks in a throwaway repository.
func Webhooks(t *testing.T, env Env) {
	ctx := testContext(t)
	_, repo := create(t, ctx, env, gitoperationslib.CreateRepoOptions{Owner: env.Org, AutoInit: true})
	scope := gitoperationslib.RepoHooks(env.Org, *repo)
	HookURL := HookURL
	if env.HookURL != "" {
		HookURL = env.HookURL
	}
	bb := isBitbucket(env)
	hooks := env.Client.Webhooks()
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	secret := "integration-secret"

	events := []gitoperationslib.HookEvent{
		gitoperationslib.HookEventPush, gitoperationslib.HookEventPullRequest, gitoperationslib.HookEventPullRequestReview,
	}
	if !bb { // Bitbucket has no release or wiki events
		events = append(events, gitoperationslib.HookEventRelease, gitoperationslib.HookEventWiki)
	}
	hook, err := hooks.Create(ctx, scope, gitoperationslib.CreateWebhookOptions{URL: HookURL, Secret: secret, Events: events})
	check(err)
	if hook.ID == 0 || hook.URL != HookURL || hook.ContentType != gitoperationslib.HookContentJSON || !hook.Active {
		t.Errorf("created = %+v", hook)
	}
	for _, e := range events {
		if !slices.Contains(hook.Events, e) {
			t.Errorf("created hook lacks %s: %v", e, hook.Events)
		}
	}

	got, err := hooks.Get(ctx, scope, hook.ID)
	check(err)
	if got.ID != hook.ID || got.URL != HookURL {
		t.Errorf("Get = %+v", got)
	}
	// The server may add default hooks to new repositories (Forgejo's
	// instance-wide default webhooks), so only look for ours. Never print
	// hook URLs: they can embed credentials.
	page, err := hooks.List(ctx, scope, gitoperationslib.ListOptions{})
	check(err)
	var ids []int64
	for _, h := range page.Items {
		ids = append(ids, h.ID)
	}
	if !slices.Contains(ids, hook.ID) {
		t.Errorf("List returned hook IDs %v, missing %d", ids, hook.ID)
	}

	if err := hooks.Ping(ctx, scope, hook.ID); bb && errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Logf("ping: %v", err)
	} else {
		check(err)
	}
	if env.CheckPing != nil {
		env.CheckPing(t, ctx, env.Org, *repo, hook.ID, []byte(secret))
	}

	newURL := HookURL + "/v2"
	form := gitoperationslib.HookContentForm
	newEvent := gitoperationslib.HookEventCreate
	editOpts := gitoperationslib.EditWebhookOptions{
		URL: &newURL, ContentType: &form, Events: []gitoperationslib.HookEvent{newEvent}, Active: gitoperationslib.Ptr(false),
	}
	if bb { // JSON only; no branch creation event (pushes cover it)
		form, newEvent = gitoperationslib.HookContentJSON, gitoperationslib.HookEventFork
		editOpts.ContentType, editOpts.Events = nil, []gitoperationslib.HookEvent{newEvent}
	}
	edited, err := hooks.Edit(ctx, scope, hook.ID, editOpts)
	check(err)
	if edited.URL != newURL || edited.ContentType != form || edited.Active || !slices.Contains(edited.Events, newEvent) ||
		slices.Contains(edited.Events, gitoperationslib.HookEventPush) {
		t.Errorf("edited = %+v", edited)
	}
	// Changing only the secret keeps the rest of the configuration.
	rekeyed, err := hooks.Edit(ctx, scope, hook.ID, gitoperationslib.EditWebhookOptions{Secret: gitoperationslib.Ptr("new-secret")})
	check(err)
	if rekeyed.URL != newURL || rekeyed.ContentType != form {
		t.Errorf("after secret change = %+v", rekeyed)
	}

	// Unknown events are rejected (GitHub) or reported after the fact
	// (Forgejo drops them silently).
	bogus, err := hooks.Create(ctx, scope, gitoperationslib.CreateWebhookOptions{
		URL: HookURL, Events: []gitoperationslib.HookEvent{"no_such_event"},
	})
	if err == nil {
		t.Error("unknown event accepted without error")
	} else {
		t.Logf("unknown event: %v", err)
	}
	if bogus != nil {
		check(hooks.Delete(ctx, scope, bogus.ID))
	}

	// Skipping the certificate check survives an edit that does not name it,
	// where the forge has it at all.
	insecure, err := hooks.Create(ctx, scope, gitoperationslib.CreateWebhookOptions{
		URL: HookURL, Events: []gitoperationslib.HookEvent{gitoperationslib.HookEventPush}, InsecureSSL: true,
	})
	if env.Client.Backend() == gitoperationslib.BackendForgejo {
		if !errors.Is(err, gitoperationslib.ErrNotSupported) {
			t.Errorf("InsecureSSL on Forgejo: err = %v, want ErrNotSupported", err)
		}
	} else {
		check(err)
		if !insecure.InsecureSSL {
			t.Errorf("created = %+v", insecure)
		}
		moved := HookURL + "/moved"
		kept, err := hooks.Edit(ctx, scope, insecure.ID, gitoperationslib.EditWebhookOptions{URL: &moved})
		check(err)
		if !kept.InsecureSSL || kept.URL != moved {
			t.Errorf("after changing the URL = %+v", kept)
		}
		reset, err := hooks.Edit(ctx, scope, insecure.ID, gitoperationslib.EditWebhookOptions{InsecureSSL: gitoperationslib.Ptr(false)})
		check(err)
		if reset.InsecureSSL || reset.URL != moved {
			t.Errorf("after resetting = %+v", reset)
		}
		check(hooks.Delete(ctx, scope, insecure.ID))
	}

	// Everything: a server that dropped names would give an error here.
	everything, err := hooks.Create(ctx, scope, gitoperationslib.CreateWebhookOptions{
		URL: HookURL, Events: []gitoperationslib.HookEvent{gitoperationslib.HookEventAll}, Active: gitoperationslib.Ptr(false),
	})
	check(err)
	if len(everything.Events) == 0 || !slices.Contains(everything.Events, gitoperationslib.HookEventAll) && !slices.Contains(everything.Events, gitoperationslib.HookEventPush) {
		t.Errorf("subscribed to everything = %v", everything.Events)
	}
	check(hooks.Delete(ctx, scope, everything.ID))

	check(hooks.Delete(ctx, scope, hook.ID))
	if _, err := hooks.Get(ctx, scope, hook.ID); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Get after delete: err = %v, want 404", err)
	}
}
