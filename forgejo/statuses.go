package forgejo

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/forgejoapi"
)

var _ gitoperationslib.CommitStatuses = (*Client)(nil)

type commitStatusJSON struct {
	Status      string    `json:"status"`
	TargetURL   string    `json:"target_url"`
	Description string    `json:"description"`
	Context     string    `json:"context"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type combinedStatusJSON struct {
	State      string             `json:"state"`
	SHA        string             `json:"sha"`
	TotalCount int                `json:"total_count"`
	Statuses   []commitStatusJSON `json:"statuses"`
}

// CommitStatus implements gitoperationslib.CommitStatuses. Forgejo Actions
// reports each job as a status. The combined state is Forgejo's own. A ref
// without statuses costs one more request.
func (c *Client) CommitStatus(ctx context.Context, owner, repo, ref string) (*gitoperationslib.CombinedStatus, error) {
	path := forgejoapi.Path("repos", owner, repo, "commits", ref, "status")
	out := &gitoperationslib.CombinedStatus{}
	for page := 1; ; page++ {
		var j combinedStatusJSON
		q := url.Values{"page": {strconv.Itoa(page)}, "limit": {"50"}}
		if _, err := c.api.Do(ctx, http.MethodGet, path, q, nil, &j); err != nil {
			return nil, err
		}
		out.SHA = j.SHA
		if j.TotalCount > 0 {
			out.State = gitoperationslib.StatusState(j.State)
		}
		for _, s := range j.Statuses {
			out.Statuses = append(out.Statuses, gitoperationslib.CommitStatus{
				Context: s.Context, State: gitoperationslib.StatusState(s.Status), Description: s.Description,
				TargetURL: s.TargetURL, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
			})
		}
		if len(j.Statuses) == 0 || len(out.Statuses) >= j.TotalCount {
			break
		}
	}
	if out.SHA == "" {
		// Without statuses, Forgejo answers an empty success, for a missing ref
		// too (checked on 15.0.11 and 17.0.1): the commit list tells, and gives
		// the SHA.
		var list []struct {
			SHA string `json:"sha"`
		}
		q := url.Values{"sha": {ref}, "limit": {"1"}, "stat": {"false"}, "files": {"false"}, "verification": {"false"}}
		if _, err := c.api.Do(ctx, http.MethodGet, forgejoapi.Path("repos", owner, repo, "commits"), q, nil, &list); err != nil {
			return nil, err
		}
		if len(list) == 0 {
			return nil, &gitoperationslib.APIError{Backend: gitoperationslib.BackendForgejo, StatusCode: http.StatusNotFound,
				Message: ref + " is not a branch, tag or commit of " + owner + "/" + repo}
		}
		out.SHA = list[0].SHA
	}
	return out, nil
}

// SetCommitStatus implements gitoperationslib.CommitStatuses.
func (c *Client) SetCommitStatus(ctx context.Context, owner, repo, sha string, s gitoperationslib.CommitStatus) error {
	if s.Context == "" || s.State == "" {
		return errors.New("forgejo: a commit status needs a context and a state")
	}
	body := map[string]string{"context": s.Context, "state": string(s.State), "description": s.Description, "target_url": s.TargetURL}
	_, err := c.api.Do(ctx, http.MethodPost, forgejoapi.Path("repos", owner, repo, "statuses", sha), nil, body, nil)
	return err
}
