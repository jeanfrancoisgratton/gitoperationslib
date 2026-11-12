package contracttest

import (
	"errors"
	"slices"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// Collaborators checks reading and refusing on every backend, and with
// env.Collaborator, adding, changing and removing a collaborator.
func Collaborators(t *testing.T, env Env) {
	c := env.Client.Collaborators()
	if !env.Client.Capabilities().Has(gitoperationslib.CapCollaborators) {
		t.Fatalf("%s does not implement Collaborators", env.Client.Backend())
	}
	ctx := testContext(t)
	repo, _ := create(t, ctx, env, gitoperationslib.CreateRepoOptions{Owner: env.Org, AutoInit: true})
	owner, name := repo.Owner, repo.Name

	logins := func() []string {
		t.Helper()
		page, err := c.List(ctx, owner, name, gitoperationslib.ListOptions{})
		if err != nil {
			t.Fatal(err)
		}
		out := make([]string, len(page.Items))
		for i, item := range page.Items {
			out[i] = item.Login
		}
		return out
	}
	logins()

	if _, err := c.Add(ctx, owner, name, env.Login, "owner"); err == nil || gitoperationslib.StatusCode(err) != 0 {
		t.Errorf("Add(owner) = %v, want a refusal without a request", err)
	}
	const stranger = "gitopslib-no-such-user-zz9"
	if _, err := c.Get(ctx, owner, name, stranger); !gitoperationslib.IsNotFound(err) {
		t.Errorf("GetCollaborator(unknown user) = %v, want 404", err)
	}
	if _, err := c.Add(ctx, owner, name, stranger, gitoperationslib.CollaboratorRead); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Add(unknown user) = %v, want 404", err)
	}
	if env.Client.Backend() != gitoperationslib.BackendGitHub {
		if _, err := c.Add(ctx, owner, name, env.Login, gitoperationslib.CollaboratorTriage); !errors.Is(err, gitoperationslib.ErrNotSupported) {
			t.Errorf("Add(triage) = %v, want ErrNotSupported", err)
		}
	}

	who := env.Collaborator
	if who == "" {
		t.Skip("no second user: adding and removing not checked")
	}
	set := func(want gitoperationslib.CollaboratorPermission) {
		t.Helper()
		invited, err := c.Add(ctx, owner, name, who, want)
		if err != nil {
			t.Fatalf("Add(%s): %v", want, err)
		}
		if invited {
			t.Skip("the user was invited: changing and removing cannot be checked before they accept")
		}
		got, err := c.Get(ctx, owner, name, who)
		if err != nil {
			t.Fatal(err)
		}
		if got.Permission != want {
			t.Errorf("Permission = %q after adding with %q", got.Permission, want)
		}
	}
	set(gitoperationslib.CollaboratorRead)
	set(gitoperationslib.CollaboratorAdmin)
	set(gitoperationslib.CollaboratorWrite)

	got, err := c.Get(ctx, owner, name, who)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(logins(), got.Login) {
		t.Errorf("%s not listed in %v", got.Login, logins())
	}
	for range 2 { // removing is idempotent
		if err := c.Remove(ctx, owner, name, who); err != nil {
			t.Fatalf("Remove: %v", err)
		}
	}
	if _, err := c.Get(ctx, owner, name, who); !gitoperationslib.IsNotFound(err) {
		t.Errorf("GetCollaborator after removing = %v, want 404", err)
	}
	if slices.Contains(logins(), got.Login) {
		t.Errorf("%s still listed after removing", got.Login)
	}
}
