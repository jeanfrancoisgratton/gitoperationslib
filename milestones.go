package gitoperationslib

import (
	"context"
	"time"
)

// Milestones manages repository milestones, addressed by Milestone.ID.
type Milestones interface {
	List(ctx context.Context, owner, repo string, state MilestoneState, opts ListOptions) (Page[Milestone], error)
	Get(ctx context.Context, owner, repo string, id int64) (*Milestone, error)
	Create(ctx context.Context, owner, repo string, opts CreateMilestoneOptions) (*Milestone, error)
	Edit(ctx context.Context, owner, repo string, id int64, opts EditMilestoneOptions) (*Milestone, error)
	Delete(ctx context.Context, owner, repo string, id int64) error
}

// MilestoneState is the state of a milestone; MilestoneStateAll is only
// meaningful as a List filter.
type MilestoneState string

const (
	MilestoneStateOpen   MilestoneState = "open"
	MilestoneStateClosed MilestoneState = "closed"
	MilestoneStateAll    MilestoneState = "all"
)

// Milestone is a repository milestone.
type Milestone struct {
	// ID addresses the milestone: Forgejo's ID, GitHub's milestone number.
	ID          int64          `json:"id"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	State       MilestoneState `json:"state"`
	// DueOn is zero if none. GitHub stores a date, not a time: whatever was sent, it
	// answers midnight UTC of the day as written in the request, which local time
	// west of Greenwich reads as the day before.
	DueOn        time.Time `json:"dueOn,omitzero"`
	OpenIssues   int       `json:"openIssues"`
	ClosedIssues int       `json:"closedIssues"`
	ClosedAt     time.Time `json:"closedAt,omitzero"`
}

// CreateMilestoneOptions describes a milestone to create.
type CreateMilestoneOptions struct {
	Title       string    `json:"title"` // required; ErrNameTaken if in use
	Description string    `json:"description,omitzero"`
	DueOn       time.Time `json:"dueOn,omitzero"` // zero: none
}

// EditMilestoneOptions changes a milestone. Nil fields are left unchanged.
type EditMilestoneOptions struct {
	Title       *string         `json:"title,omitzero"` // ErrNameTaken if in use
	Description *string         `json:"description,omitzero"`
	State       *MilestoneState `json:"state,omitzero"` // close or reopen
	DueOn       *time.Time      `json:"dueOn,omitzero"`
	// ClearDueOn removes the due date, and cannot go with DueOn. It returns
	// ErrNotSupported on Forgejo, whose API cannot remove one.
	ClearDueOn bool `json:"clearDueOn,omitzero"`
}
