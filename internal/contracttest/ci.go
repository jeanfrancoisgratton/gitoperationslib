package contracttest

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// CI checks CI secrets and variables in a throwaway repository, in the test
// organisation (when it is one: a GitHub personal namespace is not) and at
// user level. Everything it sets is named GITOPSLIB_IT_… and deleted.
func CI(t *testing.T, env Env) {
	ci, ok := env.Client.(gitoperationslib.CI)
	if !ok || !env.Client.Capabilities().Has(gitoperationslib.CapSecrets) {
		t.Fatalf("%s does not implement CI", env.Client.Backend())
	}
	t.Run("repository", func(t *testing.T) {
		ctx := testContext(t)
		repo, _ := create(t, ctx, env, gitoperationslib.CreateRepoOptions{Owner: env.Org, AutoInit: true})
		ciScope(t, env, ci, gitoperationslib.RepoCI(repo.Owner, repo.Name))
	})
	t.Run("organisation", func(t *testing.T) {
		if env.Org == "" || env.Org == env.Login {
			t.Skip("the test namespace is not an organisation")
		}
		ciScope(t, env, ci, gitoperationslib.OrgCI(env.Org))
	})
	t.Run("user", func(t *testing.T) {
		ctx := testContext(t)
		if env.Client.Backend() != gitoperationslib.BackendForgejo {
			if err := ci.Secrets().Set(ctx, gitoperationslib.UserCI(), "X", "x"); !errors.Is(err, gitoperationslib.ErrNotSupported) {
				t.Errorf("user scope: err = %v, want ErrNotSupported", err)
			}
			return
		}
		if _, err := ci.Secrets().List(ctx, gitoperationslib.UserCI(), gitoperationslib.ListOptions{}); !errors.Is(err, gitoperationslib.ErrNotSupported) {
			t.Errorf("Forgejo user secrets List: err = %v, want ErrNotSupported", err)
		}
		ciScope(t, env, ci, gitoperationslib.UserCI())
	})
}

// ciName returns a fresh name; Forgejo and GitHub upper-case names anyway.
func ciName(t *testing.T) string { return "GITOPSLIB_IT_" + strings.ToUpper(randomHex(t)) }

func ciScope(t *testing.T, env Env, ci gitoperationslib.CI, scope gitoperationslib.CIScope) {
	ctx := testContext(t)
	secrets, variables := ci.Secrets(), ci.Variables()
	listable := scope != gitoperationslib.UserCI() // Forgejo cannot list user secrets

	secretNames := func() []string {
		var names []string
		for s, err := range gitoperationslib.All(ctx, gitoperationslib.ListOptions{}, func(ctx2 context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Secret], error) {
			return secrets.List(ctx2, scope, o)
		}) {
			if err != nil {
				t.Fatal(err)
			}
			names = append(names, s.Name)
		}
		return names
	}

	secret := ciName(t)
	if err := secrets.Set(ctx, scope, secret, "first value"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = secrets.Delete(context.Background(), scope, secret) })
	if listable && !eventually(ctx, func() bool { return slices.Contains(secretNames(), secret) }) {
		t.Errorf("secret %s not listed: %v", secret, secretNames())
	}
	if err := secrets.Set(ctx, scope, secret, "second value"); err != nil {
		t.Errorf("overwrite: %v", err)
	}
	if err := secrets.Set(ctx, scope, ciName(t)+"-bad", "x"); gitoperationslib.StatusCode(err) < 400 {
		t.Errorf("invalid name: err = %v, want a 4xx", err)
	}

	variable := ciName(t)
	if err := variables.Set(ctx, scope, variable, "v1"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = variables.Delete(context.Background(), scope, variable) })
	get := func(want string) {
		t.Helper()
		ok := eventually(ctx, func() bool {
			v, err := variables.Get(ctx, scope, variable)
			return err == nil && v.Name == variable && v.Value == want
		})
		if !ok {
			v, err := variables.Get(ctx, scope, variable)
			t.Errorf("Get = %+v, %v; want value %q", v, err, want)
		}
	}
	get("v1")
	if err := variables.Set(ctx, scope, variable, "v2"); err != nil {
		t.Fatal(err)
	}
	get("v2")
	listed := false
	for v, err := range gitoperationslib.All(ctx, gitoperationslib.ListOptions{}, func(ctx2 context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Variable], error) {
		return variables.List(ctx2, scope, o)
	}) {
		if err != nil {
			t.Fatal(err)
		}
		listed = listed || (v.Name == variable && v.Value == "v2")
	}
	if !listed {
		t.Errorf("variable %s=v2 not listed", variable)
	}

	if isBitbucket(env) {
		// One list for both kinds: a secret's name cannot be reused for a
		// variable.
		if err := variables.Set(ctx, scope, secret, "x"); err == nil || !strings.Contains(err.Error(), "is a secret") {
			t.Errorf("variable named like a secret: err = %v", err)
		}
	}

	if err := variables.Delete(ctx, scope, variable); err != nil {
		t.Fatal(err)
	}
	if _, err := variables.Get(ctx, scope, variable); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Get after Delete: err = %v, want 404", err)
	}
	if err := secrets.Delete(ctx, scope, secret); err != nil {
		t.Fatal(err)
	}
	if listable && !eventually(ctx, func() bool { return !slices.Contains(secretNames(), secret) }) {
		t.Errorf("secret %s still listed after Delete", secret)
	}
}
