package github

import (
	"context"

	gh "github.com/google/go-github/v93/github"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/partial"
)

func toEmail(e *gh.UserEmail) gitoperationslib.Email {
	return gitoperationslib.Email{Address: e.GetEmail(), Primary: e.GetPrimary(), Verified: e.GetVerified(), Visibility: e.GetVisibility()}
}

func toSSHKey(k *gh.Key) gitoperationslib.SSHKey {
	return gitoperationslib.SSHKey{
		ID: k.GetID(), Title: k.GetTitle(), Key: k.GetKey(), ReadOnly: k.GetReadOnly(),
		CreatedAt: k.GetCreatedAt().Time, LastUsed: k.GetLastUsed().Time,
	}
}

func toGPGKey(k *gh.GPGKey) gitoperationslib.GPGKey {
	key := gitoperationslib.GPGKey{
		ID:                k.GetID(),
		KeyID:             k.GetKeyID(),
		PublicKey:         k.GetPublicKey(),
		CanSign:           k.GetCanSign(),
		CanEncryptComms:   k.GetCanEncryptComms(),
		CanEncryptStorage: k.GetCanEncryptStorage(),
		CanCertify:        k.GetCanCertify(),
		CreatedAt:         k.GetCreatedAt().Time,
		ExpiresAt:         k.GetExpiresAt().Time,
	}
	for _, e := range k.Emails {
		key.Emails = append(key.Emails, gitoperationslib.GPGKeyEmail{Address: e.GetEmail(), Verified: e.GetVerified()})
	}
	for _, sub := range k.Subkeys {
		key.Subkeys = append(key.Subkeys, toGPGKey(sub))
	}
	return key
}

// toCurrentUser converts GET /user, which adds private fields to a user.
func toCurrentUser(u *gh.User) *gitoperationslib.CurrentUser {
	return &gitoperationslib.CurrentUser{
		User:              toUser(u),
		TwoFactorAuth:     u.TwoFactorAuthentication,
		Type:              u.GetType(),
		Plan:              u.GetPlan().GetName(),
		Hireable:          u.GetHireable(),
		TwitterUsername:   u.GetTwitterUsername(),
		PublicRepos:       u.GetPublicRepos(),
		TotalPrivateRepos: u.GetTotalPrivateRepos(),
		OwnedPrivateRepos: u.GetOwnedPrivateRepos(),
		PublicGists:       u.GetPublicGists(),
		PrivateGists:      u.GetPrivateGists(),
		DiskUsage:         u.GetDiskUsage(),
		Collaborators:     u.GetCollaborators(),
		UpdatedAt:         u.GetUpdatedAt().Time,
	}
}

func (d directory) Me(ctx context.Context) (*gitoperationslib.CurrentUser, error) {
	u, _, err := d.c.gh.Users.Get(ctx, "")
	if err != nil {
		return nil, convertError(err)
	}
	me := toCurrentUser(u)
	var res partial.Result

	me.Emails, err = partial.Collect(ctx, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Email], error) {
		lo := ghList(o)
		list, resp, err := d.c.gh.Users.ListEmails(ctx, &lo)
		if err != nil {
			return gitoperationslib.Page[gitoperationslib.Email]{}, convertError(err)
		}
		return toPage(list, resp, toEmail), nil
	})
	if err := res.Add("e-mail addresses", err); err != nil {
		return nil, err
	}
	me.Orgs, err = partial.Collect(ctx, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Org], error) {
		return d.ListOrgs(ctx, "", o)
	})
	if err := res.Add("organisations", err); err != nil {
		return nil, err
	}
	me.Teams, err = partial.Collect(ctx, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Team], error) {
		lo := ghList(o)
		list, resp, err := d.c.gh.Teams.ListUserTeams(ctx, &lo)
		if err != nil {
			return gitoperationslib.Page[gitoperationslib.Team]{}, convertError(err)
		}
		return toPage(list, resp, func(t *gh.Team) gitoperationslib.Team { return toTeam(t, "") }), nil
	})
	if err := res.Add("teams", err); err != nil {
		return nil, err
	}
	me.Repos, err = partial.Collect(ctx, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Repo], error) {
		return repos{d.c}.List(ctx, "", o)
	})
	if err := res.Add("repositories", err); err != nil {
		return nil, err
	}
	me.SSHKeys, err = partial.Collect(ctx, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.SSHKey], error) {
		lo := ghList(o)
		list, resp, err := d.c.gh.Users.ListKeys(ctx, "", &lo)
		if err != nil {
			return gitoperationslib.Page[gitoperationslib.SSHKey]{}, convertError(err)
		}
		return toPage(list, resp, toSSHKey), nil
	})
	if err := res.Add("SSH keys", err); err != nil {
		return nil, err
	}
	me.GPGKeys, err = partial.Collect(ctx, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.GPGKey], error) {
		lo := ghList(o)
		list, resp, err := d.c.gh.Users.ListGPGKeys(ctx, "", &lo)
		if err != nil {
			return gitoperationslib.Page[gitoperationslib.GPGKey]{}, convertError(err)
		}
		return toPage(list, resp, toGPGKey), nil
	})
	if err := res.Add("GPG keys", err); err != nil {
		return nil, err
	}
	me.Incomplete, me.Missing = res.Err(), res.Parts()
	return me, nil
}
