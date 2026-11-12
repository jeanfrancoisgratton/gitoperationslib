package contracttest

import (
	"errors"
	"slices"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// Runners registers a runner's record in a new repository, finds it, and deletes
// it. No runner program connects: that is outside the library.
func Runners(t *testing.T, env Env) {
	if !env.Client.Capabilities().Has(gitoperationslib.CapRunners) {
		t.Skip("runners are not supported")
	}
	r := env.Client.CI().Runners()
	ctx := testContext(t)
	_, repo := create(t, ctx, env, gitoperationslib.CreateRepoOptions{Owner: env.Org, AutoInit: true})
	scope := gitoperationslib.RepoRunners(env.Org, *repo)
	github := env.Client.Backend() == gitoperationslib.BackendGitHub
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	token, err := r.RegistrationToken(ctx, scope)
	check(err)
	if token.Token == "" || github == token.ExpiresAt.IsZero() {
		t.Errorf("registration token = %+v", token)
	}

	opts := gitoperationslib.RegisterRunnerOptions{Name: "gitopslib-runner", Ephemeral: true}
	if github {
		if _, err := r.Register(ctx, scope, gitoperationslib.RegisterRunnerOptions{Name: "lasting"}); !errors.Is(err, gitoperationslib.ErrNotSupported) {
			t.Errorf("a lasting runner: err = %v, want ErrNotSupported", err)
		}
	} else {
		opts.Description = "a test"
	}
	reg, err := r.Register(ctx, scope, opts)
	check(err)
	if reg.ID == 0 || github == (reg.JITConfig == "") || !github && (reg.Token == "" || reg.UUID == "") {
		t.Errorf("registered = %+v", reg)
	}
	t.Cleanup(func() { _ = r.Delete(testContext(t), scope, reg.ID) })

	page, err := r.List(ctx, scope, gitoperationslib.ListRunnersOptions{})
	check(err)
	i := slices.IndexFunc(page.Items, func(x gitoperationslib.Runner) bool { return x.ID == reg.ID })
	if i < 0 {
		t.Fatalf("listed = %+v", page.Items)
	}
	if got := page.Items[i]; got.Name != opts.Name || got.Online || got.Busy || got.Status != "offline" {
		t.Errorf("listed runner = %+v", got)
	}
	got, err := r.Get(ctx, scope, reg.ID)
	check(err)
	if got.Name != opts.Name {
		t.Errorf("Get = %+v", got)
	}

	check(r.Delete(ctx, scope, reg.ID))
	if _, err := r.Get(ctx, scope, reg.ID); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Get after delete: err = %v, want 404", err)
	}
	if err := r.Delete(ctx, scope, reg.ID); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Delete again: err = %v, want 404", err)
	}
}
