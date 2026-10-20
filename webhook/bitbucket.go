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
		Actor *bbUser `json:"actor"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("webhook: decode %s payload: %w", key, err)
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

// isBitbucketPR reports whether a Bitbucket key is a pull request event.
func isBitbucketPR(key string) bool { return strings.HasPrefix(key, "pullrequest:") }
