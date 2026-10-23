package forgejo

import (
	"context"
	"net/http"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/forgejoapi"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/partial"
)

// meJSON is GET /user: a user with the fields only its owner sees.
type meJSON struct {
	userJSON
	LoginName string `json:"login_name"`
	SourceID  int64  `json:"source_id"`
}

type settingsJSON struct {
	Theme               string `json:"theme"`
	DiffViewStyle       string `json:"diff_view_style"`
	HideEmail           bool   `json:"hide_email"`
	HideActivity        bool   `json:"hide_activity"`
	HidePronouns        bool   `json:"hide_pronouns"`
	EnableRepoUnitHints bool   `json:"enable_repo_unit_hints"`
}

type emailJSON struct {
	Email    string `json:"email"`
	Primary  bool   `json:"primary"`
	Verified bool   `json:"verified"`
}

type sshKeyJSON struct {
	ID          int64     `json:"id"`
	Title       string    `json:"title"`
	Key         string    `json:"key"`
	Fingerprint string    `json:"fingerprint"`
	ReadOnly    bool      `json:"read_only"`
	CreatedAt   time.Time `json:"created_at"`
}

func (k *sshKeyJSON) toSSHKey() gitoperationslib.SSHKey {
	return gitoperationslib.SSHKey{
		ID: k.ID, Title: k.Title, Key: k.Key, Fingerprint: k.Fingerprint, ReadOnly: k.ReadOnly, CreatedAt: k.CreatedAt,
	}
}

type gpgKeyJSON struct {
	ID        int64  `json:"id"`
	KeyID     string `json:"key_id"`
	PublicKey string `json:"public_key"`
	Emails    []struct {
		Email    string `json:"email"`
		Verified bool   `json:"verified"`
	} `json:"emails"`
	Subkeys           []gpgKeyJSON `json:"subkeys"`
	CanSign           bool         `json:"can_sign"`
	CanEncryptComms   bool         `json:"can_encrypt_comms"`
	CanEncryptStorage bool         `json:"can_encrypt_storage"`
	CanCertify        bool         `json:"can_certify"`
	Verified          bool         `json:"verified"`
	CreatedAt         time.Time    `json:"created_at"`
	ExpiresAt         time.Time    `json:"expires_at"`
}

func (k *gpgKeyJSON) toGPGKey() gitoperationslib.GPGKey {
	key := gitoperationslib.GPGKey{
		ID:                k.ID,
		KeyID:             k.KeyID,
		PublicKey:         k.PublicKey,
		CanSign:           k.CanSign,
		CanEncryptComms:   k.CanEncryptComms,
		CanEncryptStorage: k.CanEncryptStorage,
		CanCertify:        k.CanCertify,
		Verified:          k.Verified,
		CreatedAt:         k.CreatedAt,
		ExpiresAt:         k.ExpiresAt,
	}
	if key.ExpiresAt.Unix() <= 0 { // "never" may come as the Unix epoch
		key.ExpiresAt = time.Time{}
	}
	for _, e := range k.Emails {
		key.Emails = append(key.Emails, gitoperationslib.GPGKeyEmail{Address: e.Email, Verified: e.Verified})
	}
	for i := range k.Subkeys {
		key.Subkeys = append(key.Subkeys, k.Subkeys[i].toGPGKey())
	}
	return key
}

func (d directory) Me(ctx context.Context) (*gitoperationslib.CurrentUser, error) {
	var u meJSON
	if _, err := d.c.api.Do(ctx, http.MethodGet, "user", nil, nil, &u); err != nil {
		return nil, err
	}
	me := &gitoperationslib.CurrentUser{User: u.toUser(), LoginName: u.LoginName, AuthSourceID: u.SourceID}
	var res partial.Result

	var settings settingsJSON
	_, err := d.c.api.Do(ctx, http.MethodGet, "user/settings", nil, nil, &settings)
	if err == nil {
		me.Settings = &gitoperationslib.UserSettings{
			Theme:               settings.Theme,
			DiffViewStyle:       settings.DiffViewStyle,
			HideEmail:           settings.HideEmail,
			HideActivity:        settings.HideActivity,
			HidePronouns:        settings.HidePronouns,
			EnableRepoUnitHints: settings.EnableRepoUnitHints,
		}
	}
	if err := res.Add("settings", err); err != nil {
		return nil, err
	}

	// Not paginated.
	var emails []emailJSON
	_, err = d.c.api.Do(ctx, http.MethodGet, "user/emails", nil, nil, &emails)
	if err == nil {
		me.Emails = make([]gitoperationslib.Email, len(emails))
		for i, e := range emails {
			me.Emails[i] = gitoperationslib.Email{Address: e.Email, Primary: e.Primary, Verified: e.Verified}
		}
	}
	if err := res.Add("e-mail addresses", err); err != nil {
		return nil, err
	}

	me.Orgs, err = partial.Collect(ctx, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Org], error) {
		return d.ListOrgs(ctx, "", o)
	})
	if err := res.Add("organisations", err); err != nil {
		return nil, err
	}
	me.Teams, err = partial.Collect(ctx, listAt(d.c, "user/teams", (*teamJSON).toTeam))
	if err := res.Add("teams", err); err != nil {
		return nil, err
	}
	me.Repos, err = partial.Collect(ctx, d.repoLister("user/repos"))
	if err := res.Add("repositories", err); err != nil {
		return nil, err
	}
	me.SSHKeys, err = partial.Collect(ctx, listAt(d.c, "user/keys", (*sshKeyJSON).toSSHKey))
	if err := res.Add("SSH keys", err); err != nil {
		return nil, err
	}
	me.GPGKeys, err = partial.Collect(ctx, listAt(d.c, "user/gpg_keys", (*gpgKeyJSON).toGPGKey))
	if err := res.Add("GPG keys", err); err != nil {
		return nil, err
	}
	me.Incomplete, me.Missing = res.Err(), res.Parts()
	return me, nil
}

// listAt lists the items at path, converting each with convert.
func listAt[J, T any](c *Client, path string, convert func(*J) T) func(context.Context, gitoperationslib.ListOptions) (gitoperationslib.Page[T], error) {
	return func(ctx context.Context, opts gitoperationslib.ListOptions) (gitoperationslib.Page[T], error) {
		var out []J
		resp, err := c.api.Do(ctx, http.MethodGet, path, forgejoapi.ListQuery(opts), nil, &out)
		if err != nil {
			return gitoperationslib.Page[T]{}, err
		}
		return toPage(resp, out, convert), nil
	}
}
