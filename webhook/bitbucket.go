package webhook

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// Bitbucket Cloud deliveries carry X-Event-Key ("repo:push",
// "pullrequest:created", …), X-Request-UUID and, when the webhook has a
// secret, X-Hub-Signature ("sha256=<hex>"). Payload shapes differ from
// GitHub's and Forgejo's; the typed accessors handle both.
//
// Implemented from Bitbucket's documentation; not yet checked against live
// deliveries.

// bitbucketActions maps pull request event keys to Name and Action.
var bitbucketActions = map[string][2]string{
	"pullrequest:created":                 {"pull_request", "opened"},
	"pullrequest:updated":                 {"pull_request", "edited"},
	"pullrequest:fulfilled":               {"pull_request", "closed"}, // merged
	"pullrequest:rejected":                {"pull_request", "closed"}, // declined
	"pullrequest:approved":                {"pull_request_review", "approved"},
	"pullrequest:unapproved":              {"pull_request_review", "dismissed"},
	"pullrequest:changes_request_created": {"pull_request_review", "changes_requested"},
	"pullrequest:changes_request_removed": {"pull_request_review", "dismissed"},
}

// bitbucketCommentActions maps comment event keys to Action. Their Name depends
// on whether the comment is on a line.
var bitbucketCommentActions = map[string]string{
	"pullrequest:comment_created": "created",
	"pullrequest:comment_updated": "edited",
	"pullrequest:comment_deleted": "deleted",
}

type bbUser struct {
	Nickname    string `json:"nickname"`
	DisplayName string `json:"display_name"`
}

func (u *bbUser) login() string {
	switch {
	case u == nil:
		return ""
	case u.Nickname != "":
		return u.Nickname
	}
	return u.DisplayName
}

func decodeBitbucket(h http.Header, body []byte) (*Event, error) {
	key := h.Get("X-Event-Key")
	ev := &Event{Forge: gitoperationslib.BackendBitbucket, Name: key, Key: key, Delivery: h.Get("X-Request-UUID"), Payload: body}
	switch {
	case key == "repo:push":
		ev.Name = "push"
	case bitbucketActions[key] != [2]string{}:
		ev.Name, ev.Action = bitbucketActions[key][0], bitbucketActions[key][1]
	}
	var env struct {
		Repository *struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		Actor   *bbUser `json:"actor"`
		Comment *struct {
			Inline *json.RawMessage `json:"inline"`
		} `json:"comment"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("webhook: decode %s payload: %w", key, err)
	}
	if action, ok := bitbucketCommentActions[key]; ok && env.Comment != nil {
		ev.Name, ev.Action = "issue_comment", action
		if env.Comment.Inline != nil {
			ev.Name = "pull_request_review_comment"
		}
	}
	if env.Repository != nil {
		ev.Repo = env.Repository.FullName
	}
	ev.Sender = env.Actor.login()
	return ev, nil
}

type bbCommit struct {
	Hash    string    `json:"hash"`
	Message string    `json:"message"`
	Date    time.Time `json:"date"`
	Author  struct {
		Raw  string  `json:"raw"` // "Name <email>"
		User *bbUser `json:"user"`
	} `json:"author"`
	Links struct {
		HTML struct {
			Href string `json:"href"`
		} `json:"html"`
	} `json:"links"`
}

func (c *bbCommit) toCommit() Commit {
	commit := Commit{ID: c.Hash, Message: c.Message, URL: c.Links.HTML.Href, Timestamp: c.Date}
	if addr, err := mail.ParseAddress(c.Author.Raw); err == nil {
		commit.Author.Name, commit.Author.Email = addr.Name, addr.Address
	} else {
		commit.Author.Name = c.Author.Raw
	}
	if c.Author.User != nil {
		commit.Author.Username = c.Author.User.login()
	}
	return commit
}

type bbRef struct {
	Type   string   `json:"type"` // "branch" or "tag"
	Name   string   `json:"name"`
	Target bbCommit `json:"target"`
}

func (r *bbRef) ref() string {
	if r.Type == "tag" {
		return "refs/tags/" + r.Name
	}
	return "refs/heads/" + r.Name
}

// bitbucketPush decodes the first change of a "repo:push" delivery (a push
// may update several refs; the others are in Payload).
func (e *Event) bitbucketPush() (*PushEvent, error) {
	var p struct {
		Actor *bbUser `json:"actor"`
		Push  struct {
			Changes []struct {
				New     *bbRef     `json:"new"`
				Old     *bbRef     `json:"old"`
				Commits []bbCommit `json:"commits"` // newest first
				Links   struct {
					HTML struct {
						Href string `json:"href"`
					} `json:"html"`
				} `json:"links"`
			} `json:"changes"`
		} `json:"push"`
	}
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		return nil, fmt.Errorf("webhook: decode push payload: %w", err)
	}
	if len(p.Push.Changes) == 0 {
		return nil, fmt.Errorf("webhook: push payload has no changes")
	}
	ch := p.Push.Changes[0]
	push := &PushEvent{Pusher: p.Actor.login(), CompareURL: ch.Links.HTML.Href, Commits: make([]Commit, 0, len(ch.Commits))}
	const zero = "0000000000000000000000000000000000000000"
	push.Before, push.After = zero, zero
	if ch.Old != nil {
		push.Ref, push.Before = ch.Old.ref(), ch.Old.Target.Hash
	}
	if ch.New != nil {
		push.Ref, push.After = ch.New.ref(), ch.New.Target.Hash
		head := ch.New.Target.toCommit()
		push.HeadCommit = &head
	}
	// Report commits oldest first, like the other forges.
	for i := len(ch.Commits) - 1; i >= 0; i-- {
		push.Commits = append(push.Commits, ch.Commits[i].toCommit())
	}
	return push, nil
}

// bitbucketPullRequest decodes a "pullrequest:…" delivery.
func (e *Event) bitbucketPullRequest() (*PullRequestEvent, error) {
	type endpoint struct {
		Branch struct {
			Name string `json:"name"`
		} `json:"branch"`
		Commit *struct {
			Hash string `json:"hash"`
		} `json:"commit"`
		Repository *struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	branch := func(e endpoint) gitoperationslib.PRBranch {
		b := gitoperationslib.PRBranch{Ref: e.Branch.Name}
		if e.Commit != nil {
			b.SHA = e.Commit.Hash
		}
		if e.Repository != nil {
			b.Repo = e.Repository.FullName
		}
		return b
	}
	var p struct {
		PullRequest struct {
			ID          int      `json:"id"`
			Title       string   `json:"title"`
			Description string   `json:"description"`
			State       string   `json:"state"`
			Draft       bool     `json:"draft"`
			Author      *bbUser  `json:"author"`
			Source      endpoint `json:"source"`
			Destination endpoint `json:"destination"`
			MergeCommit *struct {
				Hash string `json:"hash"`
			} `json:"merge_commit"`
			ClosedBy  *bbUser   `json:"closed_by"`
			CreatedOn time.Time `json:"created_on"`
			UpdatedOn time.Time `json:"updated_on"`
			Links     struct {
				HTML struct {
					Href string `json:"href"`
				} `json:"html"`
			} `json:"links"`
		} `json:"pullrequest"`
	}
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		return nil, fmt.Errorf("webhook: decode pull_request payload: %w", err)
	}
	src := p.PullRequest
	pr := gitoperationslib.PullRequest{
		Number: src.ID, Title: src.Title, Body: src.Description, State: gitoperationslib.PRStateOpen, Draft: src.Draft,
		Author: src.Author.login(), Head: branch(src.Source), Base: branch(src.Destination), Merged: src.State == "MERGED",
		HTMLURL: src.Links.HTML.Href, CreatedAt: src.CreatedOn, UpdatedAt: src.UpdatedOn,
	}
	if src.State != "OPEN" {
		pr.State, pr.ClosedAt = gitoperationslib.PRStateClosed, src.UpdatedOn
	}
	if pr.Merged {
		pr.MergedBy, pr.MergedAt = src.ClosedBy.login(), src.UpdatedOn
		if src.MergeCommit != nil {
			pr.MergeCommitSHA = src.MergeCommit.Hash
		}
	}
	return &PullRequestEvent{Action: e.Action, Number: src.ID, PullRequest: pr}, nil
}

// bitbucketReviewStates gives the verdict of the review events; removing one
// dismisses it.
var bitbucketReviewStates = map[string]gitoperationslib.ReviewState{
	"pullrequest:approved":                gitoperationslib.ReviewApproved,
	"pullrequest:unapproved":              gitoperationslib.ReviewDismissed,
	"pullrequest:changes_request_created": gitoperationslib.ReviewChangesRequested,
	"pullrequest:changes_request_removed": gitoperationslib.ReviewDismissed,
}

// bitbucketReview decodes an approval or change request. Their payloads hold the
// pull request, and the verdict under "approval" or "changes_request".
func (e *Event) bitbucketReview() (*ReviewEvent, error) {
	state, ok := bitbucketReviewStates[e.Key]
	if !ok {
		return nil, fmt.Errorf("webhook: %s delivery is not a pull_request_review event", e.Key)
	}
	pr, err := e.bitbucketPullRequest()
	if err != nil {
		return nil, err
	}
	type verdict struct {
		Date time.Time `json:"date"`
		User *bbUser   `json:"user"`
	}
	var p struct {
		Actor          *bbUser  `json:"actor"`
		Approval       *verdict `json:"approval"`
		ChangesRequest *verdict `json:"changes_request"`
	}
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		return nil, fmt.Errorf("webhook: decode pull_request_review payload: %w", err)
	}
	out := &ReviewEvent{Action: "submitted", Number: pr.Number, PullRequest: pr.PullRequest, Review: gitoperationslib.Review{Reviewer: p.Actor.login(), State: state}}
	if state == gitoperationslib.ReviewDismissed {
		out.Action = "dismissed"
	}
	v := p.Approval
	if v == nil {
		v = p.ChangesRequest
	}
	if v != nil {
		out.Review.SubmittedAt = v.Date
		if v.User != nil {
			out.Review.Reviewer = v.User.login()
		}
	}
	return out, nil
}

type bbComment struct {
	ID      int64 `json:"id"`
	Content struct {
		Raw string `json:"raw"`
	} `json:"content"`
	User   *bbUser `json:"user"`
	Inline *struct {
		Path string `json:"path"`
		From *int   `json:"from"`
		To   *int   `json:"to"`
	} `json:"inline"`
	CreatedOn time.Time `json:"created_on"`
	UpdatedOn time.Time `json:"updated_on"`
	Links     struct {
		HTML struct {
			Href string `json:"href"`
		} `json:"html"`
	} `json:"links"`
}

// bitbucketComment decodes a comment delivery of the given Name.
func (e *Event) bitbucketComment(want string) (*bbComment, *PullRequestEvent, error) {
	if e.Name != want {
		return nil, nil, fmt.Errorf("webhook: %s delivery is not a %s event", e.Key, want)
	}
	pr, err := e.bitbucketPullRequest()
	if err != nil {
		return nil, nil, err
	}
	var p struct {
		Comment bbComment `json:"comment"`
	}
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		return nil, nil, fmt.Errorf("webhook: decode %s payload: %w", want, err)
	}
	return &p.Comment, pr, nil
}

func (e *Event) bitbucketIssueComment() (*IssueCommentEvent, error) {
	c, pr, err := e.bitbucketComment("issue_comment")
	if err != nil {
		return nil, err
	}
	src := pr.PullRequest
	return &IssueCommentEvent{
		Action: e.Action, IsPullRequest: true,
		Issue: gitoperationslib.Issue{
			Number: src.Number, Title: src.Title, Body: src.Body, State: gitoperationslib.IssueState(src.State), Author: src.Author,
			HTMLURL: src.HTMLURL, CreatedAt: src.CreatedAt, UpdatedAt: src.UpdatedAt, ClosedAt: src.ClosedAt,
		},
		Comment: gitoperationslib.Comment{
			ID: c.ID, Author: c.User.login(), Body: c.Content.Raw, HTMLURL: c.Links.HTML.Href, CreatedAt: c.CreatedOn, UpdatedAt: c.UpdatedOn,
		},
	}, nil
}

func (e *Event) bitbucketReviewComment() (*ReviewCommentEvent, error) {
	c, pr, err := e.bitbucketComment("pull_request_review_comment")
	if err != nil {
		return nil, err
	}
	comment := gitoperationslib.ReviewComment{
		ID: c.ID, Author: c.User.login(), Body: c.Content.Raw, Path: c.Inline.Path, HTMLURL: c.Links.HTML.Href,
		CreatedAt: c.CreatedOn, UpdatedAt: c.UpdatedOn,
	}
	if c.Inline.To != nil {
		comment.Line = *c.Inline.To
	}
	if c.Inline.From != nil {
		comment.OldLine = *c.Inline.From
	}
	return &ReviewCommentEvent{Action: e.Action, Number: pr.Number, PullRequest: pr.PullRequest, Comment: comment}, nil
}

// isBitbucketPR reports whether a Bitbucket key is a pull request event.
func isBitbucketPR(key string) bool { return strings.HasPrefix(key, "pullrequest:") }
