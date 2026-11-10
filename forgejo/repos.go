package forgejo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/features"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/forgejoapi"
)

type repoJSON struct {
	ID    int64 `json:"id"`
	Owner struct {
		Login string `json:"login"`
	} `json:"owner"`
	Name            string `json:"name"`
	FullName        string `json:"full_name"`
	Description     string `json:"description"`
	Website         string `json:"website"`
	Private         bool   `json:"private"`
	Internal        bool   `json:"internal"`
	DefaultBranch   string `json:"default_branch"`
	Fork            bool   `json:"fork"`
	Mirror          bool   `json:"mirror"`
	Archived        bool   `json:"archived"`
	Template        bool   `json:"template"`
	Empty           bool   `json:"empty"`
	HasActions      *bool  `json:"has_actions"`
	HasIssues       *bool  `json:"has_issues"`
	HasWiki         *bool  `json:"has_wiki"`
	HasPullRequests *bool  `json:"has_pull_requests"`
	HasProjects     *bool  `json:"has_projects"`
	HasReleases     *bool  `json:"has_releases"`
	HasPackages     *bool  `json:"has_packages"`

	AllowMergeCommits   *bool                `json:"allow_merge_commits"`
	AllowRebase         *bool                `json:"allow_rebase"`
	AllowRebaseExplicit *bool                `json:"allow_rebase_explicit"`
	AllowSquashMerge    *bool                `json:"allow_squash_merge"`
	AllowFastForward    *bool                `json:"allow_fast_forward_only_merge"`
	AllowRebaseUpdate   *bool                `json:"allow_rebase_update"`
	DefaultMergeStyle   *string              `json:"default_merge_style"`
	DeleteAfterMerge    *bool                `json:"default_delete_branch_after_merge"`
	InternalTracker     *internalTrackerJSON `json:"internal_tracker"`
	ExternalTracker     *externalTrackerJSON `json:"external_tracker"`
	ExternalWiki        *externalWikiJSON    `json:"external_wiki"`
	GloballyEditable    *bool                `json:"globally_editable_wiki"`

	HTMLURL   string    `json:"html_url"`
	CloneURL  string    `json:"clone_url"`
	SSHURL    string    `json:"ssh_url"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type internalTrackerJSON struct {
	TimeTracker      bool `json:"enable_time_tracker"`
	ContributorsOnly bool `json:"allow_only_contributors_to_track_time"`
	Dependencies     bool `json:"enable_issue_dependencies"`
}

type externalTrackerJSON struct {
	URL     string `json:"external_tracker_url"`
	Format  string `json:"external_tracker_format"`
	Style   string `json:"external_tracker_style"`
	Pattern string `json:"external_tracker_regexp_pattern"`
}

type externalWikiJSON struct {
	URL string `json:"external_wiki_url"`
}

func (r *repoJSON) wikiSettings() gitoperationslib.WikiSettings {
	w := gitoperationslib.WikiSettings{EditableByAll: r.GloballyEditable}
	if r.HasWiki != nil {
		url := ""
		if r.ExternalWiki != nil {
			url = r.ExternalWiki.URL
		}
		w.ExternalURL = &url
	}
	return w
}

func (r *repoJSON) trackerSettings() gitoperationslib.IssueTrackerSettings {
	var t gitoperationslib.IssueTrackerSettings
	if r.InternalTracker != nil {
		t.TimeTracking = gitoperationslib.Ptr(r.InternalTracker.TimeTracker)
		t.TimeTrackingContributorsOnly = gitoperationslib.Ptr(r.InternalTracker.ContributorsOnly)
		t.Dependencies = gitoperationslib.Ptr(r.InternalTracker.Dependencies)
	}
	if r.ExternalTracker != nil && r.ExternalTracker.URL != "" {
		t.External = &gitoperationslib.ExternalTracker{
			URL: r.ExternalTracker.URL, Format: r.ExternalTracker.Format,
			Style: gitoperationslib.ExternalIssueStyle(r.ExternalTracker.Style), Pattern: r.ExternalTracker.Pattern,
		}
	} else if r.HasIssues != nil {
		t.External = &gitoperationslib.ExternalTracker{}
	}
	return t
}

func (r *repoJSON) toRepo() gitoperationslib.Repo {
	vis := gitoperationslib.VisibilityPublic
	switch {
	case r.Private:
		vis = gitoperationslib.VisibilityPrivate
	case r.Internal:
		vis = gitoperationslib.VisibilityInternal
	}
	return gitoperationslib.Repo{
		ID:            r.ID,
		Owner:         r.Owner.Login,
		Name:          r.Name,
		FullName:      r.FullName,
		Description:   r.Description,
		Website:       r.Website,
		Visibility:    vis,
		DefaultBranch: r.DefaultBranch,
		Fork:          r.Fork,
		Mirror:        r.Mirror,
		Archived:      r.Archived,
		Template:      r.Template,
		Empty:         r.Empty,
		Features: gitoperationslib.RepoFeatures{
			Actions: r.HasActions, Issues: r.HasIssues, Wiki: r.HasWiki, PullRequests: r.HasPullRequests,
			Projects: r.HasProjects, Releases: r.HasReleases, Packages: r.HasPackages,
		},
		Merge: gitoperationslib.MergeSettings{
			MergeCommit: r.AllowMergeCommits, Rebase: r.AllowRebase, RebaseMerge: r.AllowRebaseExplicit, Squash: r.AllowSquashMerge,
			FastForwardOnly: r.AllowFastForward, DefaultMethod: (*gitoperationslib.MergeMethod)(r.DefaultMergeStyle),
			DeleteBranch: r.DeleteAfterMerge, RebaseUpdate: r.AllowRebaseUpdate,
		},
		IssueTracker: r.trackerSettings(),
		Wiki:         r.wikiSettings(),
		HTMLURL:      r.HTMLURL,
		CloneURL:     r.CloneURL,
		SSHURL:       r.SSHURL,
		CreatedAt:    r.CreatedAt,
		UpdatedAt:    r.UpdatedAt,
	}
}

type createRepoBody struct {
	Name          string `json:"name"`
	Description   string `json:"description,omitempty"`
	Private       *bool  `json:"private,omitempty"`
	AutoInit      bool   `json:"auto_init,omitempty"`
	DefaultBranch string `json:"default_branch,omitempty"`
	Template      bool   `json:"template,omitempty"`
	Gitignores    string `json:"gitignores,omitempty"`
	License       string `json:"license,omitempty"`
}

type editRepoBody struct {
	Name            *string `json:"name,omitempty"`
	Description     *string `json:"description,omitempty"`
	Website         *string `json:"website,omitempty"`
	Private         *bool   `json:"private,omitempty"`
	DefaultBranch   *string `json:"default_branch,omitempty"`
	Archived        *bool   `json:"archived,omitempty"`
	Template        *bool   `json:"template,omitempty"`
	HasActions      *bool   `json:"has_actions,omitempty"`
	HasIssues       *bool   `json:"has_issues,omitempty"`
	HasWiki         *bool   `json:"has_wiki,omitempty"`
	HasPullRequests *bool   `json:"has_pull_requests,omitempty"`
	HasProjects     *bool   `json:"has_projects,omitempty"`
	HasReleases     *bool   `json:"has_releases,omitempty"`
	HasPackages     *bool   `json:"has_packages,omitempty"`

	AllowMergeCommits   *bool                `json:"allow_merge_commits,omitempty"`
	AllowRebase         *bool                `json:"allow_rebase,omitempty"`
	AllowRebaseExplicit *bool                `json:"allow_rebase_explicit,omitempty"`
	AllowSquashMerge    *bool                `json:"allow_squash_merge,omitempty"`
	AllowFastForward    *bool                `json:"allow_fast_forward_only_merge,omitempty"`
	AllowRebaseUpdate   *bool                `json:"allow_rebase_update,omitempty"`
	DefaultMergeStyle   *string              `json:"default_merge_style,omitempty"`
	DeleteAfterMerge    *bool                `json:"default_delete_branch_after_merge,omitempty"`
	InternalTracker     *internalTrackerJSON `json:"internal_tracker,omitempty"`
	ExternalTracker     *externalTrackerJSON `json:"external_tracker,omitempty"`
	ExternalWiki        *externalWikiJSON    `json:"external_wiki,omitempty"`
	GloballyEditable    *bool                `json:"globally_editable_wiki,omitempty"`
}

// featuresWhy is what may keep a Forgejo server from applying a setting.
const featuresWhy = "the instance may have it turned off (Actions with [actions] ENABLED=false, for one), or another setting may rule it out"

// privateFlag maps a Visibility to Forgejo's private flag; nil means
// "server default". Forgejo cannot set internal visibility on a repository.
func privateFlag(v gitoperationslib.Visibility, op string) (*bool, error) {
	switch v {
	case "":
		return nil, nil
	case gitoperationslib.VisibilityPublic:
		return gitoperationslib.Ptr(false), nil
	case gitoperationslib.VisibilityPrivate:
		return gitoperationslib.Ptr(true), nil
	case gitoperationslib.VisibilityInternal:
		return nil, gitoperationslib.NotSupported(gitoperationslib.BackendForgejo, op, "internal visibility")
	}
	return nil, fmt.Errorf("forgejo: %s: invalid visibility %q", op, v)
}

type repos struct{ c *Client }

func (r repos) Get(ctx context.Context, owner, name string) (*gitoperationslib.Repo, error) {
	var out repoJSON
	if _, err := r.c.api.Do(ctx, http.MethodGet, forgejoapi.Path("repos", owner, name), nil, nil, &out); err != nil {
		return nil, err
	}
	repo := out.toRepo()
	return &repo, nil
}

func (r repos) List(ctx context.Context, owner string, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Repo], error) {
	path := "user/repos"
	if owner != "" {
		org, err := r.c.isOrg(ctx, owner)
		if err != nil {
			return gitoperationslib.Page[gitoperationslib.Repo]{}, err
		}
		if org {
			path = forgejoapi.Path("orgs", owner, "repos")
		} else {
			path = forgejoapi.Path("users", owner, "repos")
		}
	}

	var out []repoJSON
	resp, err := r.c.api.Do(ctx, http.MethodGet, path, forgejoapi.ListQuery(opts), nil, &out)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Repo]{}, err
	}
	return toPage(resp, out, (*repoJSON).toRepo), nil
}

func (r repos) Create(ctx context.Context, opts gitoperationslib.CreateRepoOptions) (*gitoperationslib.Repo, error) {
	if opts.Name == "" {
		return nil, errors.New("forgejo: repos.create: name is required")
	}
	if err := features.Refuse(gitoperationslib.BackendForgejo, "repos.create", opts.Features, "discussions"); err != nil {
		return nil, err
	}
	private, err := privateFlag(opts.Visibility, "repos.create")
	if err != nil {
		return nil, err
	}
	path, err := r.createPath(ctx, opts.Owner)
	if err != nil {
		return nil, err
	}
	body := createRepoBody{
		Name:          opts.Name,
		Description:   opts.Description,
		Private:       private,
		AutoInit:      opts.AutoInit,
		DefaultBranch: opts.DefaultBranch,
		Template:      opts.Template,
		Gitignores:    strings.Join(opts.Gitignores, ","),
		License:       opts.License,
	}
	var out repoJSON
	if _, err := r.c.api.Do(ctx, http.MethodPost, path, nil, body, &out); err != nil {
		return nil, err
	}
	repo := out.toRepo()
	// The create endpoint takes no features: set those that differ afterwards.
	if len(features.Unapplied(opts.Features, repo.Features)) == 0 {
		return &repo, nil
	}
	edited, err := r.Edit(ctx, repo.Owner, repo.Name, gitoperationslib.EditRepoOptions{Features: opts.Features})
	if edited == nil {
		return &repo, fmt.Errorf("forgejo: repos.create: %s created, but setting its features failed: %w", repo.FullName, err)
	}
	return edited, err
}

// createPath picks the create endpoint for owner: the authenticated user, an
// organisation, or (admin only) another user.
func (r repos) createPath(ctx context.Context, owner string) (string, error) {
	if owner == "" {
		return "user/repos", nil
	}
	me, err := r.c.currentLogin(ctx)
	if err != nil {
		return "", err
	}
	if strings.EqualFold(owner, me) {
		return "user/repos", nil
	}
	org, err := r.c.isOrg(ctx, owner)
	if err != nil {
		return "", err
	}
	if org {
		return forgejoapi.Path("orgs", owner, "repos"), nil
	}
	return forgejoapi.Path("admin", "users", owner, "repos"), nil
}

func (r repos) Edit(ctx context.Context, owner, name string, opts gitoperationslib.EditRepoOptions) (*gitoperationslib.Repo, error) {
	const op = "repos.edit"
	if err := features.Refuse(gitoperationslib.BackendForgejo, op, opts.Features, "discussions"); err != nil {
		return nil, err
	}
	if err := features.Refuse(gitoperationslib.BackendForgejo, op, opts.Merge, "autoMerge", "updateBranch"); err != nil {
		return nil, err
	}
	if features.Requested(opts.Actions) {
		return nil, gitoperationslib.NotSupported(gitoperationslib.BackendForgejo, op, "Actions settings")
	}
	f, m := opts.Features, opts.Merge
	body := editRepoBody{
		Name:          opts.Name,
		Description:   opts.Description,
		Website:       opts.Website,
		DefaultBranch: opts.DefaultBranch,
		Archived:      opts.Archived,
		Template:      opts.Template,
		HasActions:    f.Actions, HasIssues: f.Issues, HasWiki: f.Wiki, HasPullRequests: f.PullRequests,
		HasProjects: f.Projects, HasReleases: f.Releases, HasPackages: f.Packages,
		AllowMergeCommits: m.MergeCommit, AllowRebase: m.Rebase, AllowRebaseExplicit: m.RebaseMerge, AllowSquashMerge: m.Squash,
		AllowFastForward: m.FastForwardOnly, DefaultMergeStyle: (*string)(m.DefaultMethod), DeleteAfterMerge: m.DeleteBranch,
		AllowRebaseUpdate: m.RebaseUpdate, GloballyEditable: opts.Wiki.EditableByAll,
	}
	if err := r.trackerAndWiki(ctx, owner, name, opts, &body); err != nil {
		return nil, err
	}
	if opts.Visibility != nil {
		private, err := privateFlag(*opts.Visibility, "repos.edit")
		if err != nil {
			return nil, err
		}
		body.Private = private
	}
	var out repoJSON
	if _, err := r.c.api.Do(ctx, http.MethodPatch, forgejoapi.Path("repos", owner, name), nil, body, &out); err != nil {
		return nil, err
	}
	repo := out.toRepo()
	// The style the library filled in is what the server reports.
	tracker := opts.IssueTracker
	if tracker.External != nil && tracker.External.URL != "" && tracker.External.Style == "" {
		external := *tracker.External
		external.Style = gitoperationslib.ExternalIssueNumeric
		tracker.External = &external
	}
	return features.Check(gitoperationslib.BackendForgejo, op, &repo, featuresWhy,
		opts.Features, repo.Features, opts.Merge, repo.Merge, tracker, repo.IssueTracker, opts.Wiki, repo.Wiki)
}

// trackerAndWiki fills what a PATCH needs to keep the issue tracker and the wiki as
// they are. Forgejo resets the tracker settings left out of internal_tracker, and
// has_issues or has_wiki set to true goes back to the built-in tracker or wiki,
// with default settings (checked on 15.0.11 and 17.0.1). So whenever the PATCH
// names any of them, it sends them all, from the current repository.
func (r repos) trackerAndWiki(ctx context.Context, owner, name string, opts gitoperationslib.EditRepoOptions, body *editRepoBody) error {
	t, w := opts.IssueTracker, opts.Wiki
	settings := t.TimeTracking != nil || t.TimeTrackingContributorsOnly != nil || t.Dependencies != nil || t.External != nil
	tracker := opts.Features.Issues != nil || settings
	wiki := opts.Features.Wiki != nil || w.ExternalURL != nil
	merge := features.Requested(opts.Merge)
	if !tracker && !wiki && !merge {
		return nil
	}
	var cur repoJSON
	if _, err := r.c.api.Do(ctx, http.MethodGet, forgejoapi.Path("repos", owner, name), nil, nil, &cur); err != nil {
		return err
	}
	// The server ignores merge settings while pull requests are off.
	pulls := cur.HasPullRequests != nil && *cur.HasPullRequests
	if opts.Features.PullRequests != nil {
		pulls = *opts.Features.PullRequests
	}
	if merge && !pulls {
		return errors.New("forgejo: repos.edit: merge settings need pull requests on")
	}
	if tracker {
		on := cur.HasIssues != nil && *cur.HasIssues
		if opts.Features.Issues != nil {
			on = *opts.Features.Issues
		}
		if !on && settings {
			return errors.New("forgejo: repos.edit: the issue tracker settings need issues on")
		}
		if on {
			body.HasIssues = gitoperationslib.Ptr(true)
			external := cur.ExternalTracker
			if t.External != nil {
				external = &externalTrackerJSON{URL: t.External.URL, Format: t.External.Format, Style: string(t.External.Style), Pattern: t.External.Pattern}
				if external.Style == "" {
					external.Style = string(gitoperationslib.ExternalIssueNumeric)
				}
			}
			if external != nil && external.URL != "" {
				body.ExternalTracker = external
			} else {
				internal := internalTrackerJSON{TimeTracker: true, ContributorsOnly: true, Dependencies: true}
				if cur.InternalTracker != nil {
					internal = *cur.InternalTracker
				}
				if t.TimeTracking != nil {
					internal.TimeTracker = *t.TimeTracking
				}
				if t.TimeTrackingContributorsOnly != nil {
					internal.ContributorsOnly = *t.TimeTrackingContributorsOnly
				}
				if t.Dependencies != nil {
					internal.Dependencies = *t.Dependencies
				}
				body.InternalTracker = &internal
			}
		}
	}
	if wiki {
		on := cur.HasWiki != nil && *cur.HasWiki
		if opts.Features.Wiki != nil {
			on = *opts.Features.Wiki
		}
		if !on && w.ExternalURL != nil {
			return errors.New("forgejo: repos.edit: an external wiki needs the wiki on")
		}
		if on {
			body.HasWiki = gitoperationslib.Ptr(true)
			url := ""
			if cur.ExternalWiki != nil {
				url = cur.ExternalWiki.URL
			}
			if w.ExternalURL != nil {
				url = *w.ExternalURL
			}
			if url != "" {
				body.ExternalWiki = &externalWikiJSON{URL: url}
			}
		}
	}
	return nil
}

func (r repos) Delete(ctx context.Context, owner, name string) error {
	_, err := r.c.api.Do(ctx, http.MethodDelete, forgejoapi.Path("repos", owner, name), nil, nil, nil)
	return err
}
