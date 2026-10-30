package bitbucket

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/bitbucketapi"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/refusal"
)

type refJSON struct {
	Name    string `json:"name"`
	Message string `json:"message"` // tags only
	Target  struct {
		Hash    string    `json:"hash"`
		Message string    `json:"message"`
		Date    time.Time `json:"date"` // the author date
		Author  struct {
			Raw  string `json:"raw"` // "Name <email>"
			User struct {
				DisplayName string `json:"display_name"`
			} `json:"user"`
		} `json:"author"`
	} `json:"target"`
	Links struct {
		HTML linkJSON `json:"html"`
	} `json:"links"`
}

func (c *Client) defaultBranch(ctx context.Context, owner, repo string) (string, error) {
	r, err := c.Repos().Get(ctx, owner, repo)
	if err != nil {
		return "", err
	}
	return r.DefaultBranch, nil
}

// resolveCommit turns a branch, tag or commit into a full commit hash; empty
// means the default branch.
func (c *Client) resolveCommit(ctx context.Context, owner, repo, ref string) (string, error) {
	if ref == "" {
		var err error
		if ref, err = c.defaultBranch(ctx, owner, repo); err != nil {
			return "", err
		}
	}
	for _, kind := range []string{"branches", "tags"} {
		var out refJSON
		_, err := c.api.Do(ctx, http.MethodGet, repoPath(owner, repo, "refs", kind, ref), nil, nil, &out)
		if err == nil {
			return out.Target.Hash, nil
		}
		if !gitoperationslib.IsNotFound(err) {
			return "", err
		}
	}
	var commit struct {
		Hash string `json:"hash"`
	}
	_, err := c.api.Do(ctx, http.MethodGet, repoPath(owner, repo, "commit", ref), nil, nil, &commit)
	if gitoperationslib.IsNotFound(err) {
		return "", &gitoperationslib.APIError{
			Backend: gitoperationslib.BackendBitbucket, StatusCode: http.StatusNotFound,
			Message: fmt.Sprintf("%q is not a branch, tag or commit of %s/%s", ref, owner, repo),
		}
	}
	return commit.Hash, err
}

// Branches implements gitoperationslib.Client.
func (c *Client) Branches() gitoperationslib.Branches { return branches{c} }

type branches struct{ c *Client }

func toBranch(r *refJSON, def string) gitoperationslib.Branch {
	br := gitoperationslib.Branch{Name: r.Name, SHA: r.Target.Hash, IsDefault: r.Name == def}
	if !r.Target.Date.IsZero() {
		// The git author ("Name <email>") is always there; the Bitbucket account only when the e-mail maps to one
		author, _, _ := strings.Cut(r.Target.Author.Raw, " <")
		if author == "" {
			author = r.Target.Author.User.DisplayName
		}
		br.LastCommit = &gitoperationslib.CommitSummary{Message: r.Target.Message, Author: author, Date: r.Target.Date}
	}
	return br
}

// List returns the branches. Branch.Protected is not reported by Bitbucket.
func (b branches) List(ctx context.Context, owner, repo string, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Branch], error) {
	def, err := b.c.defaultBranch(ctx, owner, repo)
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Branch]{}, err
	}
	page, err := bitbucketapi.List[refJSON](ctx, b.c.api, repoPath(owner, repo, "refs", "branches"), bitbucketapi.ListQuery(opts))
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Branch]{}, err
	}
	out := gitoperationslib.Page[gitoperationslib.Branch]{Items: make([]gitoperationslib.Branch, len(page.Values)), NextPage: page.NextPage()}
	for i := range page.Values {
		out.Items[i] = toBranch(&page.Values[i], def)
	}
	return out, nil
}

func (b branches) Get(ctx context.Context, owner, repo, name string) (*gitoperationslib.Branch, error) {
	var out refJSON
	if _, err := b.c.api.Do(ctx, http.MethodGet, repoPath(owner, repo, "refs", "branches", name), nil, nil, &out); err != nil {
		return nil, err
	}
	def, err := b.c.defaultBranch(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	br := toBranch(&out, def)
	return &br, nil
}

func (b branches) Create(ctx context.Context, owner, repo string, opts gitoperationslib.CreateBranchOptions) (*gitoperationslib.Branch, error) {
	if opts.Name == "" {
		return nil, errNoName
	}
	sha, err := b.c.resolveCommit(ctx, owner, repo, opts.From)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"name": opts.Name, "target": map[string]string{"hash": sha}}
	var out refJSON
	if _, err := b.c.api.Do(ctx, http.MethodPost, repoPath(owner, repo, "refs", "branches"), nil, body, &out); err != nil {
		return nil, err
	}
	br := toBranch(&out, "")
	return &br, nil
}

func (b branches) Rename(context.Context, string, string, string, string) (*gitoperationslib.Branch, error) {
	return nil, notSupported("branches.rename", "Bitbucket has no branch rename API")
}

func (b branches) Delete(ctx context.Context, owner, repo, name string) error {
	_, err := b.c.api.Do(ctx, http.MethodDelete, repoPath(owner, repo, "refs", "branches", name), nil, nil, nil)
	if gitoperationslib.StatusCode(err) == http.StatusInternalServerError && b.deleteRestricted(ctx, owner, repo, name) {
		// Bitbucket refuses with HTTP 500 "Something went wrong".
		return refusal.Mark(err, refusal.Rule{Sentinel: gitoperationslib.ErrBranchProtected})
	}
	return err
}

// deleteRestricted reports whether a branch restriction forbids deleting
// name. Errors count as "no": the caller keeps the original error.
func (b branches) deleteRestricted(ctx context.Context, owner, repo, name string) bool {
	rs, err := branchProtections(b).all(ctx, owner, repo)
	if err != nil {
		return false
	}
	for _, r := range rs {
		if r.Kind == kindDelete && globMatch(r.Pattern, name) {
			return true
		}
	}
	return false
}

// globMatch matches name against a Bitbucket branch pattern, where "*"
// matches any run of characters, "/" included.
func globMatch(pattern, name string) bool {
	re := "^" + strings.NewReplacer(`\*`, ".*", `\?`, ".").Replace(regexp.QuoteMeta(pattern)) + "$"
	ok, _ := regexp.MatchString(re, name)
	return ok
}

// Compare counts commits, as Bitbucket has no comparison endpoint: the commits of head excluding base's, and the reverse.
func (b branches) Compare(ctx context.Context, owner, repo, base, head string) (*gitoperationslib.Comparison, error) {
	if base == "" || head == "" {
		return nil, errors.New("bitbucket: branches.compare: base and head are required")
	}
	ahead, err := b.commitsMissing(ctx, owner, repo, base, head)
	if err != nil {
		return nil, err
	}
	behind, err := b.commitsMissing(ctx, owner, repo, head, base)
	if err != nil {
		return nil, err
	}
	return &gitoperationslib.Comparison{Base: base, Head: head, Ahead: ahead, Behind: behind}, nil
}

// commitsMissing counts the commits of head that base lacks, page by page.
// The commit list's pages are not numbered (page is an opaque token): every next link is followed as given.
func (b branches) commitsMissing(ctx context.Context, owner, repo, base, head string) (int, error) {
	query := url.Values{"include": {head}, "exclude": {base}, "pagelen": {"100"}}
	count := 0
	for {
		page, err := bitbucketapi.List[struct {
			Hash string `json:"hash"`
		}](ctx, b.c.api, repoPath(owner, repo, "commits"), query)
		if err != nil {
			return 0, err
		}
		count += len(page.Values)
		if page.Next == "" {
			return count, nil
		}
		next, err := url.Parse(page.Next)
		if err != nil {
			return 0, fmt.Errorf("bitbucket: branches.compare: next page link %q: %w", page.Next, err)
		}
		query = next.Query()
	}
}

// Tags implements gitoperationslib.Client.
func (c *Client) Tags() gitoperationslib.Tags { return tags{c} }

type tags struct{ c *Client }

// toTag reports Bitbucket's generated annotation ("Added tag … for
// changeset …", used when no message was given) as an empty message, like a
// lightweight tag elsewhere.
func toTag(owner, repo string, r *refJSON) gitoperationslib.Tag {
	msg := r.Message
	if strings.HasPrefix(msg, "Added tag "+r.Name+" for changeset ") {
		msg = ""
	}
	archive := "https://bitbucket.org/" + bitbucketapi.Path(owner, repo, "get", r.Name)
	return gitoperationslib.Tag{Name: r.Name, SHA: r.Target.Hash, Message: msg, TarballURL: archive + ".tar.gz", ZipballURL: archive + ".zip"}
}

func (t tags) List(ctx context.Context, owner, repo string, opts gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Tag], error) {
	page, err := bitbucketapi.List[refJSON](ctx, t.c.api, repoPath(owner, repo, "refs", "tags"), bitbucketapi.ListQuery(opts))
	if err != nil {
		return gitoperationslib.Page[gitoperationslib.Tag]{}, err
	}
	out := gitoperationslib.Page[gitoperationslib.Tag]{Items: make([]gitoperationslib.Tag, len(page.Values)), NextPage: page.NextPage()}
	for i := range page.Values {
		out.Items[i] = toTag(owner, repo, &page.Values[i])
	}
	return out, nil
}

func (t tags) Get(ctx context.Context, owner, repo, name string) (*gitoperationslib.Tag, error) {
	var out refJSON
	if _, err := t.c.api.Do(ctx, http.MethodGet, repoPath(owner, repo, "refs", "tags", name), nil, nil, &out); err != nil {
		return nil, err
	}
	tag := toTag(owner, repo, &out)
	return &tag, nil
}

// Create creates a tag. Bitbucket always creates annotated tags; without a
// Message it writes its own annotation.
func (t tags) Create(ctx context.Context, owner, repo string, opts gitoperationslib.CreateTagOptions) (*gitoperationslib.Tag, error) {
	if opts.Name == "" {
		return nil, errNoName
	}
	sha, err := t.c.resolveCommit(ctx, owner, repo, opts.Target)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"name": opts.Name, "target": map[string]string{"hash": sha}}
	if opts.Message != "" {
		body["message"] = opts.Message
	}
	var out refJSON
	if _, err := t.c.api.Do(ctx, http.MethodPost, repoPath(owner, repo, "refs", "tags"), nil, body, &out); err != nil {
		return nil, err
	}
	tag := toTag(owner, repo, &out)
	return &tag, nil
}

func (t tags) Delete(ctx context.Context, owner, repo, name string) error {
	_, err := t.c.api.Do(ctx, http.MethodDelete, repoPath(owner, repo, "refs", "tags", name), nil, nil, nil)
	return err
}
