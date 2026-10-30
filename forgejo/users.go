package forgejo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/forgejoapi"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/refusal"
)

type userJSON struct {
	ID            int64     `json:"id"`
	Login         string    `json:"login"`
	FullName      string    `json:"full_name"`
	Email         string    `json:"email"`
	Description   string    `json:"description"`
	Website       string    `json:"website"`
	Location      string    `json:"location"`
	Visibility    string    `json:"visibility"`
	IsAdmin       bool      `json:"is_admin"`
	Active        bool      `json:"active"`
	Restricted    bool      `json:"restricted"`
	ProhibitLogin bool      `json:"prohibit_login"`
	HTMLURL       string    `json:"html_url"`
	AvatarURL     string    `json:"avatar_url"`
	Pronouns      string    `json:"pronouns"`
	Language      string    `json:"language"`
	Followers     int       `json:"followers_count"`
	Following     int       `json:"following_count"`
	StarredRepos  int       `json:"starred_repos_count"`
	Created       time.Time `json:"created"`
	LastLogin     time.Time `json:"last_login"`
}

func (u *userJSON) toUser() gitoperationslib.User {
	lastLogin := u.LastLogin
	if lastLogin.Unix() <= 0 { // Forgejo reports "never" as the Unix epoch
		lastLogin = time.Time{}
	}
	return gitoperationslib.User{
		ID:            u.ID,
		Login:         u.Login,
		FullName:      u.FullName,
		Email:         u.Email,
		Description:   u.Description,
		Website:       u.Website,
		Location:      u.Location,
		Visibility:    gitoperationslib.OwnerVisibility(u.Visibility),
		IsAdmin:       u.IsAdmin,
		Active:        u.Active,
		Restricted:    u.Restricted,
		ProhibitLogin: u.ProhibitLogin,
		HTMLURL:       u.HTMLURL,
		AvatarURL:     u.AvatarURL,
		Pronouns:      u.Pronouns,
		Language:      u.Language,
		Followers:     u.Followers,
		Following:     u.Following,
		StarredRepos:  u.StarredRepos,
		CreatedAt:     u.Created,
		LastLogin:     lastLogin,
	}
}

type createUserBody struct {
	Username           string `json:"username"`
	Email              string `json:"email"`
	Password           string `json:"password,omitempty"`
	FullName           string `json:"full_name,omitempty"`
	MustChangePassword *bool  `json:"must_change_password,omitempty"`
	Visibility         string `json:"visibility,omitempty"`
	Restricted         bool   `json:"restricted,omitempty"`
	SendNotify         bool   `json:"send_notify,omitempty"`
}

type editUserBody struct {
	Email                   *string `json:"email,omitempty"`
	FullName                *string `json:"full_name,omitempty"`
	Description             *string `json:"description,omitempty"`
	Website                 *string `json:"website,omitempty"`
	Location                *string `json:"location,omitempty"`
	Password                *string `json:"password,omitempty"`
	MustChangePassword      *bool   `json:"must_change_password,omitempty"`
	Visibility              *string `json:"visibility,omitempty"`
	Admin                   *bool   `json:"admin,omitempty"`
	Active                  *bool   `json:"active,omitempty"`
	ProhibitLogin           *bool   `json:"prohibit_login,omitempty"`
	Restricted              *bool   `json:"restricted,omitempty"`
	AllowCreateOrganization *bool   `json:"allow_create_organization,omitempty"`
	MaxRepoCreation         *int    `json:"max_repo_creation,omitempty"`
}

// ownerVisibility validates v; empty is allowed only where allowEmpty.
func ownerVisibility(v gitoperationslib.OwnerVisibility, op string, allowEmpty bool) (string, error) {
	if (v == "" && allowEmpty) || v.Valid() {
		return string(v), nil
	}
	return "", fmt.Errorf("forgejo: %s: invalid visibility %q", op, v)
}

// Users implements gitoperationslib.Admin.
func (c *Client) Users() gitoperationslib.Users { return users{c} }

type users struct{ c *Client }

func (u users) Get(ctx context.Context, login string) (*gitoperationslib.User, error) {
	var out userJSON
	if _, err := u.c.api.Do(ctx, http.MethodGet, forgejoapi.Path("users", login), nil, nil, &out); err != nil {
		return nil, err
	}
	user := out.toUser()
	return &user, nil
}

func (u users) List(ctx context.Context, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.User], error) {
	var out []userJSON
	resp, err := u.c.api.Do(ctx, http.MethodGet, "admin/users", forgejoapi.ListQuery(opts), nil, &out)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.User]{}, err
	}
	return toPage(resp, out, (*userJSON).toUser), nil
}

func (u users) Create(ctx context.Context, opts gitoperationslib.CreateUserOptions) (*gitoperationslib.User, error) {
	const op = "users.create"
	if opts.Login == "" || opts.Email == "" {
		return nil, errors.New("forgejo: users.create: login and email are required")
	}
	vis, err := ownerVisibility(opts.Visibility, op, true)
	if err != nil {
		return nil, err
	}
	body := createUserBody{
		Username:           opts.Login,
		Email:              opts.Email,
		Password:           opts.Password,
		FullName:           opts.FullName,
		MustChangePassword: opts.MustChangePassword,
		Visibility:         vis,
		Restricted:         opts.Restricted,
		SendNotify:         opts.SendNotify,
	}
	var out userJSON
	if _, err := u.c.api.Do(ctx, http.MethodPost, "admin/users", nil, body, &out); err != nil {
		return nil, err
	}
	user := out.toUser()
	return &user, nil
}

func (u users) Edit(ctx context.Context, login string, opts gitoperationslib.EditUserOptions) (*gitoperationslib.User, error) {
	body := editUserBody{
		Email:                   opts.Email,
		FullName:                opts.FullName,
		Description:             opts.Description,
		Website:                 opts.Website,
		Location:                opts.Location,
		Password:                opts.Password,
		MustChangePassword:      opts.MustChangePassword,
		Admin:                   opts.Admin,
		Active:                  opts.Active,
		ProhibitLogin:           opts.ProhibitLogin,
		Restricted:              opts.Restricted,
		AllowCreateOrganization: opts.AllowCreateOrganization,
		MaxRepoCreation:         opts.MaxRepoCreation,
	}
	if opts.Visibility != nil {
		vis, err := ownerVisibility(*opts.Visibility, "users.edit", false)
		if err != nil {
			return nil, err
		}
		body.Visibility = &vis
	}
	var out userJSON
	if _, err := u.c.api.Do(ctx, http.MethodPatch, forgejoapi.Path("admin", "users", login), nil, body, &out); err != nil {
		return nil, err
	}
	user := out.toUser()
	// Forgejo (at least 15.0.x) silently ignores a change to "public", the
	// zero value of its internal visibility enum. Report it instead of
	// pretending it worked.
	if body.Visibility != nil && out.Visibility != *body.Visibility {
		return &user, gitoperationslib.NotSupported(gitoperationslib.BackendForgejo, "users.edit",
			fmt.Sprintf("server kept visibility %q instead of %q; other changes were applied", out.Visibility, *body.Visibility))
	}
	return &user, nil
}

func (u users) Rename(ctx context.Context, login, newLogin string) error {
	if newLogin == "" {
		return errors.New("forgejo: users.rename: new login is required")
	}
	body := struct {
		NewUsername string `json:"new_username"`
	}{newLogin}
	_, err := u.c.api.Do(ctx, http.MethodPost, forgejoapi.Path("admin", "users", login, "rename"), nil, body, nil)
	return err
}

func (u users) Delete(ctx context.Context, login string, opts gitoperationslib.DeleteUserOptions) error {
	var q url.Values
	if opts.Purge {
		q = url.Values{"purge": {"true"}}
	}
	_, err := u.c.api.Do(ctx, http.MethodDelete, forgejoapi.Path("admin", "users", login), q, nil, nil)
	return refusal.Mark(err,
		refusal.Rule{Status: http.StatusUnprocessableEntity, Fragment: "still has ownership of repositories", Sentinel: gitoperationslib.ErrOwnsRepos},
		refusal.Rule{Status: http.StatusUnprocessableEntity, Fragment: "still has membership of organizations", Sentinel: gitoperationslib.ErrOrgMember})
}
