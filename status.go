package gitoperationslib

import (
	"context"
	"time"
)

// CommitStatuses reads the CI state of a commit: the statuses CI systems
// report on it, and on GitHub its check runs. Optional (CapCommitStatuses):
//
//	s, ok := client.(gitoperationslib.CommitStatuses)
type CommitStatuses interface {
	// CommitStatus returns the combined state of ref (a branch, tag or commit
	// SHA) and each status it combines.
	CommitStatus(ctx context.Context, owner, repo, ref string) (*CombinedStatus, error)
	// SetCommitStatus reports a status on a commit, as a CI system does; a
	// status with the same Context replaces the previous one. Context and State
	// are required; Bitbucket also requires TargetURL. Warnings are Forgejo's
	// only (ErrNotSupported elsewhere), and Check is ignored.
	SetCommitStatus(ctx context.Context, owner, repo, sha string, status CommitStatus) error
}

// StatusState is the state of a commit status.
type StatusState string

const (
	StatusPending StatusState = "pending" // queued or running
	StatusSuccess StatusState = "success"
	StatusFailure StatusState = "failure" // the checks failed
	StatusError   StatusState = "error"   // the CI system failed, or was stopped
	StatusWarning StatusState = "warning" // Forgejo only
)

// CombinedStatus is the state of a commit, combined from its statuses.
type CombinedStatus struct {
	SHA string `json:"sha"`
	// State is empty when nothing reported on the commit. Otherwise it is a
	// failure or an error if any status is one, pending if any is still
	// running, else a success (Forgejo combines it itself, and has warnings).
	State    StatusState    `json:"state,omitzero"`
	Statuses []CommitStatus `json:"statuses,omitzero"`
}

// CommitStatus is what one CI job, workflow or check reported on a commit.
type CommitStatus struct {
	// Context names it: "ci/woodpecker", "build / test (push)", a check run's
	// name. Bitbucket's key.
	Context     string      `json:"context"`
	State       StatusState `json:"state"`
	Description string      `json:"description,omitzero"`
	TargetURL   string      `json:"targetUrl,omitzero"` // the run's page
	// Check is set for a GitHub check run, which is how GitHub Actions reports.
	Check     bool      `json:"check,omitzero"`
	CreatedAt time.Time `json:"createdAt,omitzero"`
	UpdatedAt time.Time `json:"updatedAt,omitzero"`
}

// CombineStates combines states as CombinedStatus.State describes; "" for none.
func CombineStates(states ...StatusState) StatusState {
	var out StatusState
	rank := map[StatusState]int{StatusSuccess: 1, StatusWarning: 2, StatusPending: 3, StatusError: 4, StatusFailure: 5}
	for _, s := range states {
		if rank[s] > rank[out] {
			out = s
		}
	}
	return out
}
