package bitbucket

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/bitbucketapi"
)

type commitStatusJSON struct {
	Key         string    `json:"key"`
	Name        string    `json:"name"`
	State       string    `json:"state"`
	Description string    `json:"description"`
	URL         string    `json:"url"`
	CreatedOn   time.Time `json:"created_on"`
	UpdatedOn   time.Time `json:"updated_on"`
}

func statusState(s string) gitoperationslib.StatusState {
	switch s {
	case "SUCCESSFUL":
		return gitoperationslib.StatusSuccess
	case "FAILED":
		return gitoperationslib.StatusFailure
	case "INPROGRESS":
		return gitoperationslib.StatusPending
	}
	return gitoperationslib.StatusError // STOPPED
}

// Status implements gitoperationslib.Commits. Bitbucket Pipelines
// and other CI systems report build statuses; the combined state is the
// library's, as Bitbucket has none. A branch or tag costs one more request.
func (m commits) Status(ctx context.Context, owner, repo, ref string) (*gitoperationslib.CombinedStatus, error) {
	c := m.c
	sha, err := c.resolveCommit(ctx, owner, repo, ref)
	if err != nil {
		return nil, err
	}
	list, err := bitbucketapi.All[commitStatusJSON](ctx, c.api, repoPath(owner, repo, "commit", sha, "statuses"), url.Values{"pagelen": {"100"}})
	if err != nil {
		return nil, err
	}
	out := &gitoperationslib.CombinedStatus{SHA: sha}
	var states []gitoperationslib.StatusState
	for _, s := range list {
		st := statusState(s.State)
		states = append(states, st)
		ctxName := s.Key
		if ctxName == "" {
			ctxName = s.Name
		}
		out.Statuses = append(out.Statuses, gitoperationslib.CommitStatus{
			Context: ctxName, State: st, Description: s.Description, TargetURL: s.URL,
			CreatedAt: s.CreatedOn, UpdatedAt: s.UpdatedOn,
		})
	}
	out.State = gitoperationslib.CombineStates(states...)
	return out, nil
}

// SetStatus implements gitoperationslib.Commits: a build status,
// keyed by Context.
func (m commits) SetStatus(ctx context.Context, owner, repo, sha string, s gitoperationslib.CommitStatus) error {
	c := m.c
	if s.Context == "" || s.State == "" || s.TargetURL == "" {
		return errors.New("bitbucket: a commit status needs a context, a state and a target URL")
	}
	states := map[gitoperationslib.StatusState]string{
		gitoperationslib.StatusSuccess: "SUCCESSFUL", gitoperationslib.StatusFailure: "FAILED",
		gitoperationslib.StatusPending: "INPROGRESS", gitoperationslib.StatusError: "STOPPED",
	}
	state, ok := states[s.State]
	if !ok {
		return gitoperationslib.NotSupported(gitoperationslib.BackendBitbucket, "statuses.set", string(s.State)+" statuses")
	}
	body := map[string]string{"key": s.Context, "state": state, "url": s.TargetURL, "description": s.Description}
	_, err := c.api.Do(ctx, http.MethodPost, repoPath(owner, repo, "commit", sha, "statuses", "build"), nil, body, nil)
	return err
}
