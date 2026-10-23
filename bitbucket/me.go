package bitbucket

import (
	"context"
	"net/http"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/bitbucketapi"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/partial"
)

// meJSON is GET /user.
type meJSON struct {
	userJSON
	Location      string `json:"location"`
	AccountStatus string `json:"account_status"`
	Has2FAEnabled *bool  `json:"has_2fa_enabled"`
	Links         struct {
		HTML   linkJSON `json:"html"`
		Avatar linkJSON `json:"avatar"`
	} `json:"links"`
	CreatedOn time.Time `json:"created_on"`
}

type emailJSON struct {
	Email       string `json:"email"`
	IsPrimary   bool   `json:"is_primary"`
	IsConfirmed bool   `json:"is_confirmed"`
}

type sshKeyJSON struct {
	Key         string    `json:"key"`
	Comment     string    `json:"comment"`
	Label       string    `json:"label"`
	Fingerprint string    `json:"fingerprint"`
	CreatedOn   time.Time `json:"created_on"`
	LastUsed    time.Time `json:"last_used"`
}

func (k *sshKeyJSON) toSSHKey() gitoperationslib.SSHKey {
	key := gitoperationslib.SSHKey{
		Title: k.Label, Key: k.Key, Fingerprint: k.Fingerprint, CreatedAt: k.CreatedOn, LastUsed: k.LastUsed,
	}
	if key.Title == "" {
		key.Title = k.Comment
	}
	return key
}

// workspaceAccessJSON is an entry of GET /user/workspaces.
type workspaceAccessJSON struct {
	Workspace struct {
		Slug  string `json:"slug"`
		Name  string `json:"name"`
		Links struct {
			HTML   linkJSON `json:"html"`
			Avatar linkJSON `json:"avatar"`
		} `json:"links"`
	} `json:"workspace"`
}

const noMeTeams = "Bitbucket Cloud groups are not exposed by the REST API 2.0"
const noMeGPG = "GPG keys are not implemented for Bitbucket Cloud"

// Me reports workspaces as organisations. Teams and GPG keys are recorded in
// Incomplete as not supported.
func (d directory) Me(ctx context.Context) (*gitoperationslib.CurrentUser, error) {
	var u meJSON
	if _, err := d.c.api.Do(ctx, http.MethodGet, "user", nil, nil, &u); err != nil {
		return nil, err
	}
	me := &gitoperationslib.CurrentUser{
		User: gitoperationslib.User{
			Login:     u.login(),
			FullName:  u.DisplayName,
			Location:  u.Location,
			Active:    u.AccountStatus == "" || u.AccountStatus == "active",
			HTMLURL:   u.Links.HTML.Href,
			AvatarURL: u.Links.Avatar.Href,
			CreatedAt: u.CreatedOn,
		},
		UUID:          u.UUID,
		AccountID:     u.AccountID,
		TwoFactorAuth: u.Has2FAEnabled,
	}
	var res partial.Result

	emails, err := bitbucketapi.All[emailJSON](ctx, d.c.api, "user/emails", nil)
	if err == nil {
		me.Emails = make([]gitoperationslib.Email, len(emails))
		for i, e := range emails {
			me.Emails[i] = gitoperationslib.Email{Address: e.Email, Primary: e.IsPrimary, Verified: e.IsConfirmed}
			if e.IsPrimary {
				me.Email = e.Email
			}
		}
	}
	if err := res.Add("e-mail addresses", err); err != nil {
		return nil, err
	}

	workspaces, err := bitbucketapi.All[workspaceAccessJSON](ctx, d.c.api, "user/workspaces", nil)
	if err == nil {
		me.Orgs = make([]gitoperationslib.Org, len(workspaces))
		for i, w := range workspaces {
			me.Orgs[i] = gitoperationslib.Org{
				Name: w.Workspace.Slug, FullName: w.Workspace.Name,
				HTMLURL: w.Workspace.Links.HTML.Href, AvatarURL: w.Workspace.Links.Avatar.Href,
			}
		}
	}
	if err := res.Add("organisations", err); err != nil {
		return nil, err
	}

	if err := res.Add("teams", notSupported("directory.me", noMeTeams)); err != nil {
		return nil, err
	}
	me.Repos, err = partial.Collect(ctx, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Repo], error) {
		return repos{d.c}.List(ctx, "", o)
	})
	if err := res.Add("repositories", err); err != nil {
		return nil, err
	}

	keys, err := bitbucketapi.All[sshKeyJSON](ctx, d.c.api, bitbucketapi.Path("users", u.UUID, "ssh-keys"), nil)
	if err == nil {
		me.SSHKeys = make([]gitoperationslib.SSHKey, len(keys))
		for i := range keys {
			me.SSHKeys[i] = keys[i].toSSHKey()
		}
	}
	if err := res.Add("SSH keys", err); err != nil {
		return nil, err
	}
	if err := res.Add("GPG keys", notSupported("directory.me", noMeGPG)); err != nil {
		return nil, err
	}
	me.Incomplete = res.Err()
	return me, nil
}
