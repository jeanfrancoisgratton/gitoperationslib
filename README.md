<p align="center">
  <img src="images/gitoperationslib_banner.png" alt="gitoperationslib banner" width="100%">
</p>
___

# gitoperationslib

A Go library for managing git forges through one set of interfaces. **Forgejo** is the primary backend; **GitHub** is supported wherever the two APIs overlap. Code written against the interfaces works with either backend; anything a backend cannot do is reported with a typed error instead of being silently skipped.

- No third-party types in the public API: all types are this library's own.
- The Forgejo backend is a small hand-written HTTP client (no SDK); the GitHub backend uses [`google/go-github`](https://github.com/google/go-github) (pinned at v93) plus a minimal GraphQL client for the few features REST cannot do.
- Every call takes a `context.Context`.

## Contents

- [Features](#features)
- [Installation](#installation)
- [Creating a client](#creating-a-client)
- [Core concepts](#core-concepts): capabilities, errors, pagination, optional fields
- [Repositories](#repositories)
- [Branches and tags](#branches-and-tags)
- [Branch and tag protection](#branch-and-tag-protection)
- [Releases and assets](#releases-and-assets)
- [Pull requests and reviews](#pull-requests-and-reviews)
- [Issues, labels, milestones and comments](#issues-labels-milestones-and-comments)
- [Webhooks](#webhooks)
- [Receiving webhooks](#receiving-webhooks)
- [Administration (Forgejo)](#administration-forgejo): users, organisations, teams
- [Backend differences](#backend-differences)
- [Testing](#testing)

## Features

| Area | Forgejo | GitHub |
|---|---|---|
| Repositories | ✅ | ✅ |
| Branches and tags | ✅ | ✅ |
| Branch and tag protection | ✅ protection rules | ✅ rulesets |
| Releases and assets | ✅ (plus external assets) | ✅ |
| Pull requests and reviews | ✅ | ✅ (drafts and auto-merge via GraphQL) |
| Issues, labels, milestones, comments | ✅ | ✅ |
| Webhooks (repository and organisation) | ✅ | ✅ |
| Receiving and verifying deliveries | ✅ | ✅ |
| Users, organisations, teams (admin) | ✅ | — |

## Installation

```sh
go get github.com/jeanfrancoisgratton/gitoperationslib
```

Requirements: Go 1.27.2 or later; Forgejo 11.0.10 or later (checked when connecting).

| Package | Import path |
|---|---|
| Interfaces and types | `github.com/jeanfrancoisgratton/gitoperationslib` |
| Forgejo backend | `github.com/jeanfrancoisgratton/gitoperationslib/forgejo` |
| GitHub backend | `github.com/jeanfrancoisgratton/gitoperationslib/github` |
| Receiving webhooks | `github.com/jeanfrancoisgratton/gitoperationslib/webhook` |

The examples below use these imports:

```go
import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	gol "github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/forgejo"
	"github.com/jeanfrancoisgratton/gitoperationslib/github"
	"github.com/jeanfrancoisgratton/gitoperationslib/webhook"
)
```

`gol` is just a short alias for the root package; the examples assume a `ctx context.Context` and a `client gol.Client`.

## Creating a client

### Forgejo

`forgejo.New` connects, reads the server version and refuses servers older than the supported minimum.

```go
// Personal access token (Settings → Applications on Forgejo).
client, err := forgejo.New(ctx, "https://git.example.com", forgejo.Token(os.Getenv("FORGEJO_TOKEN")))
if err != nil {
	var tooOld *gol.UnsupportedVersionError
	if errors.As(err, &tooOld) {
		log.Fatalf("server %s is older than %s", tooOld.Version, tooOld.Minimum)
	}
	log.Fatal(err)
}
fmt.Println("connected to Forgejo", client.Version())

// Username and password instead of a token:
client, err = forgejo.New(ctx, "https://git.example.com", forgejo.BasicAuth("alice", "secret"))

// Anonymous (public data only):
client, err = forgejo.New(ctx, "https://git.example.com", nil)
```

The base URL may be the instance root, include `/api/v1`, or point to a sub-path install (`https://example.com/forgejo`).

Options:

```go
client, err := forgejo.New(ctx, "https://git.example.com", forgejo.Token(token),
	forgejo.WithHTTPClient(&http.Client{Timeout: 30 * time.Second}), // proxies, custom CA, timeouts
	forgejo.WithUserAgent("my-tool/1.0"),
)
```

### GitHub

`github.New` makes no request.

```go
// Personal access token; fine-grained tokens are recommended.
client, err := github.New(os.Getenv("GITHUB_TOKEN"))

// GitHub Enterprise Server: "/api/v3/" is appended when missing.
client, err = github.New(token, github.WithEnterpriseURL("https://github.example.com", ""))

// Same options as Forgejo:
client, err = github.New(token, github.WithHTTPClient(httpClient), github.WithUserAgent("my-tool/1.0"))
```

Both `*forgejo.Client` and `*github.Client` implement `gol.Client`, so the rest of your code does not need to know which forge it talks to:

```go
func open(ctx context.Context, forge string) (gol.Client, error) {
	switch forge {
	case "forgejo":
		return forgejo.New(ctx, os.Getenv("FORGEJO_URL"), forgejo.Token(os.Getenv("FORGEJO_TOKEN")))
	case "github":
		return github.New(os.Getenv("GITHUB_TOKEN"))
	}
	return nil, fmt.Errorf("unknown forge %q", forge)
}
```

## Core concepts

### The client and its areas

`gol.Client` gives access to each area:

```go
client.Repos()             // repositories
client.Branches()          // branches
client.Tags()              // tags
client.BranchProtections() // branch protection rules
client.TagProtections()    // tag protection rules
client.Releases()          // releases and assets
client.PullRequests()      // pull requests and reviews
client.Issues()            // issues and comments
client.Labels()            // repository labels
client.Milestones()        // milestones
client.Webhooks()          // repository and organisation webhooks
```

### Capabilities and optional interfaces

Features only some backends have are exposed through optional interfaces. Check with `Capabilities()` or a type assertion:

```go
fmt.Println(client.Backend(), client.Capabilities()) // e.g. "forgejo repos|admin|releases|…"

if client.Capabilities().Has(gol.CapAdmin) {
	admin := client.(gol.Admin) // users, organisations, teams (Forgejo only)
	_ = admin
}

if ext, ok := client.Releases().(gol.ExternalAssets); ok { // Forgejo only
	_ = ext
}
```

### Errors

| Error | Meaning |
|---|---|
| `gol.ErrNotSupported` | The backend cannot do this (match with `errors.Is`; details in `*gol.NotSupportedError`). |
| `*gol.APIError` | The server returned an error: `StatusCode`, `Message`, `Details`, `DocumentationURL`. |
| `*gol.UnsupportedVersionError` | The Forgejo server is too old. |
| `gol.ErrOwnerTeam` | Refused to delete, rename or change an organisation's owner team. |

```go
repo, err := client.Repos().Get(ctx, "acme", "widget")
switch {
case gol.IsNotFound(err):
	fmt.Println("no such repository")
case gol.IsForbidden(err), gol.IsUnauthorized(err):
	fmt.Println("check the token's permissions")
case errors.Is(err, gol.ErrNotSupported):
	fmt.Println("not available on", client.Backend())
case err != nil:
	var apiErr *gol.APIError
	if errors.As(err, &apiErr) {
		fmt.Println(apiErr.StatusCode, apiErr.Message, apiErr.Details)
	}
default:
	fmt.Println(repo.FullName)
}
```

A few calls return a result **and** an `ErrNotSupported` error when the server silently ignored part of the request (for example a visibility change Forgejo does not honour). The returned value then shows the server's actual state.

### Pagination

List methods take `gol.ListOptions{Page, PerPage}` (zero values mean "first page" and "server default") and return a `gol.Page[T]` with `Items` and `NextPage` (0 on the last page). `gol.All` iterates over every page:

```go
// One page:
page, err := client.Repos().List(ctx, "acme", gol.ListOptions{PerPage: 50})
for _, r := range page.Items {
	fmt.Println(r.FullName)
}

// Everything:
fetch := func(ctx context.Context, o gol.ListOptions) (gol.Page[gol.Repo], error) {
	return client.Repos().List(ctx, "acme", o)
}
for repo, err := range gol.All(ctx, gol.ListOptions{PerPage: 50}, fetch) {
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(repo.FullName)
}
```

### Optional fields

In `Edit…Options` structs, `nil` means "leave unchanged". `gol.Ptr` builds pointers inline:

```go
_, err := client.Repos().Edit(ctx, "acme", "widget", gol.EditRepoOptions{
	Description: gol.Ptr("New description"),
	Archived:    gol.Ptr(true),
})
```

## Repositories

```go
repos := client.Repos()

// Create in the authenticated user's namespace (Owner empty), in an
// organisation (Owner: "acme"), or — Forgejo admins — for another user.
repo, err := repos.Create(ctx, gol.CreateRepoOptions{
	Owner:       "acme",
	Name:        "widget",
	Description: "Widget service",
	Visibility:  gol.VisibilityPrivate,
	AutoInit:    true,              // initial commit…
	Gitignores:  []string{"Go"},    // …with a .gitignore (GitHub: at most one)
	License:     "MIT",
	// DefaultBranch: "main",       // Forgejo only (GitHub: ErrNotSupported)
})

repo, err = repos.Get(ctx, "acme", "widget")
fmt.Println(repo.FullName, repo.Visibility, repo.DefaultBranch, repo.CloneURL, repo.SSHURL)

// List an organisation's or user's repositories; empty owner lists every
// repository the authenticated user can access.
page, err := repos.List(ctx, "acme", gol.ListOptions{})

// Rename, change settings, change the default branch.
repo, err = repos.Edit(ctx, "acme", "widget", gol.EditRepoOptions{
	Name:          gol.Ptr("widget-service"),
	Website:       gol.Ptr("https://widget.example.com"),
	Visibility:    gol.Ptr(gol.VisibilityPublic),
	DefaultBranch: gol.Ptr("main"),
})

err = repos.Delete(ctx, "acme", "widget-service")
```

## Branches and tags

```go
branches := client.Branches()

// From the default branch (From empty), a branch, a tag or a commit SHA.
br, err := branches.Create(ctx, "acme", "widget", gol.CreateBranchOptions{Name: "feature/login", From: "main"})
br, err = branches.Get(ctx, "acme", "widget", "feature/login")
fmt.Println(br.Name, br.SHA, br.Protected, br.IsDefault)

page, err := branches.List(ctx, "acme", "widget", gol.ListOptions{}) // IsDefault marks the default branch
br, err = branches.Rename(ctx, "acme", "widget", "feature/login", "feature/sign-in")
err = branches.Delete(ctx, "acme", "widget", "feature/sign-in")
```

The default branch is changed with `Repos().Edit(… EditRepoOptions{DefaultBranch: …})`.

```go
tags := client.Tags()

// Lightweight tag on the default branch.
tag, err := tags.Create(ctx, "acme", "widget", gol.CreateTagOptions{Name: "v1.0.0"})

// Annotated tag (Message set) on a branch, tag or commit.
tag, err = tags.Create(ctx, "acme", "widget", gol.CreateTagOptions{
	Name: "v1.1.0", Target: "main", Message: "Release 1.1.0",
})

tag, err = tags.Get(ctx, "acme", "widget", "v1.1.0")
fmt.Println(tag.Name, tag.SHA, tag.Message, tag.TarballURL)

page, err := tags.List(ctx, "acme", "widget", gol.ListOptions{})
err = tags.Delete(ctx, "acme", "widget", "v1.0.0")
```

To publish a release for a tag, use [`Releases().Create`](#releases-and-assets): it also creates the tag if it does not exist yet.

## Branch and tag protection

Rules are addressed by **pattern** (a branch name or glob). `Update` **replaces** a rule: get it, change it, update it. Zero values are the protective choice: deletion and force pushes are blocked unless explicitly allowed.

On GitHub, rules are repository **rulesets**; on free plans they are only available for public repositories.

```go
bp := client.BranchProtections()

rule, err := bp.Create(ctx, "acme", "widget", gol.BranchProtection{
	Pattern:               "main",
	RequiredApprovals:     1,
	DismissStaleApprovals: true,
	RequiredStatusChecks:  []string{"ci/test"},
	RequireUpToDate:       true,  // branch must be up to date before merging
	RequireSignedCommits:  true,
	ApplyToAdmins:         true,  // no exemption for repository admins
	PushAllowlist:         &gol.Allowlist{Teams: []string{"release-managers"}},
})

// Glob rules.
_, err = bp.Create(ctx, "acme", "widget", gol.BranchProtection{Pattern: "release/*", RequiredApprovals: 2})

// Change a rule.
rule, err = bp.Get(ctx, "acme", "widget", "main")
rule.RequiredApprovals = 2
rule, err = bp.Update(ctx, "acme", "widget", "main", *rule)

rules, err := bp.List(ctx, "acme", "widget")
err = bp.Delete(ctx, "acme", "widget", "release/*")
```

`PushAllowlist`: `nil` lets anyone with write access push; an empty `&gol.Allowlist{}` blocks direct pushes (changes go through pull requests). Backend-specific fields (`MergeAllowlist`, `ProtectedFilePatterns`… on Forgejo; `RequireLinearHistory`, `RequireConversationResolution` on GitHub) return `ErrNotSupported` on the other backend.

```go
tp := client.TagProtections()

// Only the release-managers team may create, move or delete v* tags.
// GitHub: an empty allowlist blocks everyone; Forgejo needs at least one entry.
tr, err := tp.Create(ctx, "acme", "widget", gol.TagProtection{
	Pattern:   "v*",
	Allowlist: gol.Allowlist{Teams: []string{"release-managers"}},
})
tr, err = tp.Update(ctx, "acme", "widget", "v*", gol.TagProtection{Pattern: "v*", Allowlist: gol.Allowlist{Users: []string{"alice"}}}) // users: Forgejo only
err = tp.Delete(ctx, "acme", "widget", "v*")
```

## Releases and assets

The usual flow: create a **draft**, upload the files, then publish.

```go
rels := client.Releases()

// Creates the tag from TargetCommitish if it does not exist yet (when the
// draft is published).
rel, err := rels.Create(ctx, "acme", "widget", gol.CreateReleaseOptions{
	TagName:         "v1.0.1",
	TargetCommitish: "main",
	Name:            "1.0.1",
	Body:            "## Changes\n- Fixed the login bug",
	Draft:           true,
})

// Upload binaries: any io.Reader, with its exact size. The content type is
// guessed from the name when empty.
f, err := os.Open("dist/tool_1.0.1_linux_amd64.tar.gz")
if err != nil {
	log.Fatal(err)
}
defer f.Close()
info, err := f.Stat()
asset, err := rels.UploadAsset(ctx, "acme", "widget", rel.ID, "tool_1.0.1_linux_amd64.tar.gz", f, info.Size(), "")
fmt.Println(asset.ID, asset.Name, asset.Size, asset.DownloadURL)

// Publish.
rel, err = rels.Edit(ctx, "acme", "widget", rel.ID, gol.EditReleaseOptions{Draft: gol.Ptr(false)})
```

Reading, editing and deleting:

```go
rel, err := rels.GetByTag(ctx, "acme", "widget", "v1.0.1")
latest, err := rels.Latest(ctx, "acme", "widget") // newest published non-prerelease
page, err := rels.List(ctx, "acme", "widget", gol.ListOptions{})

assets, err := rels.ListAssets(ctx, "acme", "widget", rel.ID, gol.ListOptions{})
asset, err := rels.EditAsset(ctx, "acme", "widget", rel.ID, assetID, gol.EditAssetOptions{Name: gol.Ptr("tool-linux.tar.gz")})

// Download (streamed; close it).
rc, err := rels.DownloadAsset(ctx, "acme", "widget", rel.ID, assetID)
if err != nil {
	log.Fatal(err)
}
defer rc.Close()
out, _ := os.Create("tool.tar.gz")
defer out.Close()
_, err = io.Copy(out, rc)

err = rels.DeleteAsset(ctx, "acme", "widget", rel.ID, assetID)
err = rels.Delete(ctx, "acme", "widget", rel.ID) // the tag is kept
```

Backend-specific options:

```go
// GitHub: generated notes and the "latest" flag.
_, err := rels.Create(ctx, "acme", "widget", gol.CreateReleaseOptions{
	TagName: "v2.0.0", GenerateReleaseNotes: true, MakeLatest: gol.Ptr(true),
})

// Forgejo: hide the source archives, and link files hosted elsewhere.
_, err = rels.Edit(ctx, "acme", "widget", rel.ID, gol.EditReleaseOptions{HideArchiveLinks: gol.Ptr(true)})
if ext, ok := rels.(gol.ExternalAssets); ok {
	_, err = ext.AddExternalAsset(ctx, "acme", "widget", rel.ID, "tool-mirror.tar.gz", "https://mirror.example.com/tool.tar.gz")
}
```

## Pull requests and reviews

```go
prs := client.PullRequests()

pr, err := prs.Create(ctx, "acme", "widget", gol.CreatePROptions{
	Title: "Add sign-in",
	Body:  "Closes #12",
	Head:  "feature/sign-in", // "user:branch" for a fork
	Base:  "main",
	Draft: true, // Forgejo: adds the "WIP: " title prefix
})
fmt.Println(pr.Number, pr.State, pr.Draft, pr.Head.SHA, pr.HTMLURL)

pr, err = prs.Get(ctx, "acme", "widget", pr.Number)
page, err := prs.List(ctx, "acme", "widget", gol.ListPROptions{State: gol.PRStateOpen})

// Mark ready for review (GitHub uses GraphQL), change the title, close, reopen.
pr, err = prs.Edit(ctx, "acme", "widget", pr.Number, gol.EditPROptions{Draft: gol.Ptr(false)})
pr, err = prs.Edit(ctx, "acme", "widget", pr.Number, gol.EditPROptions{Title: gol.Ptr("Add sign-in page")})
pr, err = prs.Edit(ctx, "acme", "widget", pr.Number, gol.EditPROptions{State: gol.Ptr(gol.PRStateClosed)})
```

On Forgejo, drafts are titles starting with `WIP:` or `[WIP]`; `Title` is returned as stored, prefix included.

Reviews:

```go
err := prs.RequestReviewers(ctx, "acme", "widget", pr.Number, []string{"bob"}, []string{"backend-team"})
err = prs.RemoveReviewers(ctx, "acme", "widget", pr.Number, []string{"bob"}, nil)

// Approving or requesting changes on one's own pull request is refused by both forges.
review, err := prs.CreateReview(ctx, "acme", "widget", pr.Number, gol.CreateReviewOptions{
	Event: gol.ReviewEventApprove, // or ReviewEventRequestChanges, ReviewEventComment
	Body:  "Looks good",
})
reviews, err := prs.ListReviews(ctx, "acme", "widget", pr.Number, gol.ListOptions{})
for _, r := range reviews.Items {
	fmt.Println(r.Reviewer, r.State) // approved, changes_requested, commented, …
}
```

Merging:

```go
err := prs.Merge(ctx, "acme", "widget", pr.Number, gol.MergeOptions{
	Method:       gol.MergeMethodSquash, // merge, squash, rebase; Forgejo also rebase-merge, fast-forward-only, manually-merged
	Title:        "Add sign-in (#42)",
	HeadSHA:      pr.Head.SHA,           // fail if the branch moved since it was reviewed
	DeleteBranch: true,
})
merged, err := prs.IsMerged(ctx, "acme", "widget", pr.Number)

// Merge automatically once required checks pass, or cancel that.
// GitHub: the repository must allow auto-merge and a required check must be pending.
err = prs.Merge(ctx, "acme", "widget", pr.Number, gol.MergeOptions{Method: gol.MergeMethodMerge, WhenChecksSucceed: true})
err = prs.CancelAutoMerge(ctx, "acme", "widget", pr.Number)
```

`PullRequest.Mergeable` is a `*bool`: GitHub computes it in the background, so it can be `nil` right after creation.

## Issues, labels, milestones and comments

Labels are addressed by **name**; colours are six hex digits without `#`.

```go
labels := client.Labels()
_, err := labels.Create(ctx, "acme", "widget", gol.Label{Name: "bug", Color: "d73a4a", Description: "Something is broken"})
_, err = labels.Edit(ctx, "acme", "widget", "bug", gol.EditLabelOptions{Name: gol.Ptr("defect"), Color: gol.Ptr("b60205")})
page, err := labels.List(ctx, "acme", "widget", gol.ListOptions{})
err = labels.Delete(ctx, "acme", "widget", "defect")
```

Milestones are addressed by `Milestone.ID` (Forgejo's ID, GitHub's milestone number):

```go
ms, err := client.Milestones().Create(ctx, "acme", "widget", gol.CreateMilestoneOptions{
	Title: "v1.1", Description: "Next minor release", DueOn: time.Now().AddDate(0, 1, 0),
})
open, err := client.Milestones().List(ctx, "acme", "widget", gol.MilestoneStateOpen, gol.ListOptions{})
ms, err = client.Milestones().Edit(ctx, "acme", "widget", ms.ID, gol.EditMilestoneOptions{State: gol.Ptr(gol.MilestoneStateClosed)})
```

Issues (pull requests are never listed as issues, but label and comment methods accept pull request numbers too):

```go
issues := client.Issues()

issue, err := issues.Create(ctx, "acme", "widget", gol.CreateIssueOptions{
	Title:     "Login fails with SSO",
	Body:      "Steps to reproduce: …",
	Labels:    []string{"bug"},
	Assignees: []string{"alice"},
	Milestone: ms.ID,
})

page, err := issues.List(ctx, "acme", "widget", gol.ListIssuesOptions{
	State:  gol.IssueStateOpen,
	Labels: []string{"bug"},
})

// Non-nil slices replace the current values; Milestone 0 removes it.
issue, err = issues.Edit(ctx, "acme", "widget", issue.Number, gol.EditIssueOptions{
	Labels:    &[]string{"bug", "needs-triage"},
	Assignees: &[]string{},           // unassign everyone
	Milestone: gol.Ptr(int64(0)),
})
err = issues.AddLabels(ctx, "acme", "widget", issue.Number, []string{"security"})
err = issues.RemoveLabel(ctx, "acme", "widget", issue.Number, "needs-triage")

// Comments.
c, err := issues.CreateComment(ctx, "acme", "widget", issue.Number, "Reproduced on main.")
c, err = issues.EditComment(ctx, "acme", "widget", c.ID, "Reproduced on main and v1.0.")
comments, err := issues.ListComments(ctx, "acme", "widget", issue.Number, gol.ListOptions{})
err = issues.DeleteComment(ctx, "acme", "widget", c.ID)

// Close, or delete permanently (repository admins; GitHub uses GraphQL).
issue, err = issues.Edit(ctx, "acme", "widget", issue.Number, gol.EditIssueOptions{State: gol.Ptr(gol.IssueStateClosed)})
err = issues.Delete(ctx, "acme", "widget", issue.Number)
```

## Webhooks

Webhooks live on a repository (`gol.RepoHooks`) or an organisation (`gol.OrgHooks`). Event names follow GitHub's and are translated for Forgejo.

```go
hooks := client.Webhooks()
scope := gol.RepoHooks("acme", "widget") // or gol.OrgHooks("acme")

hook, err := hooks.Create(ctx, scope, gol.CreateWebhookOptions{
	URL:    "https://ci.example.com/hooks/forge",
	Secret: os.Getenv("WEBHOOK_SECRET"), // signs deliveries; see "Receiving webhooks"
	Events: []gol.HookEvent{gol.HookEventPush, gol.HookEventPullRequest, gol.HookEventRelease},
	// ContentType: gol.HookContentForm, // default json
	// BranchFilter: "{main,release/*}",  // Forgejo only
})

err = hooks.Ping(ctx, scope, hook.ID) // test delivery

hook, err = hooks.Edit(ctx, scope, hook.ID, gol.EditWebhookOptions{
	Events: []gol.HookEvent{gol.HookEventPush}, // replaces the subscription list
	Active: gol.Ptr(false),
})
page, err := hooks.List(ctx, scope, gol.ListOptions{})
err = hooks.Delete(ctx, scope, hook.ID)
```

Forgejo silently drops event names it does not know; `Create` and `Edit` then return the webhook **and** an error naming the dropped events. Hook URLs can embed credentials: avoid logging `Webhook.URL`.

## Receiving webhooks

The `webhook` package verifies signatures (HMAC-SHA256, constant-time), identifies the forge and the event, and decodes common payloads. It does not depend on either backend.

```go
secret := []byte(os.Getenv("WEBHOOK_SECRET"))

http.Handle("/hooks/forge", webhook.Handler(secret, func(ctx context.Context, ev *webhook.Event) error {
	log.Printf("%s %s from %s on %s (delivery %s)", ev.Forge, ev.Name, ev.Sender, ev.Repo, ev.Delivery)

	switch ev.Name {
	case "push":
		push, err := ev.Push()
		if err != nil {
			return err
		}
		if branch := push.Branch(); branch == "main" {
			log.Printf("main moved to %s (%d commits) by %s", push.After, len(push.Commits), push.Pusher)
		}
	case "pull_request":
		pr, err := ev.PullRequest()
		if err != nil {
			return err
		}
		log.Printf("PR #%d %s: %s", pr.Number, pr.Action, pr.PullRequest.Title)
	case "release":
		rel, err := ev.Release()
		if err != nil {
			return err
		}
		if rel.Action == "published" {
			log.Printf("released %s", rel.Release.TagName)
		}
	default:
		// Anything else: ev.Payload holds the raw JSON.
	}
	return nil
}))
log.Fatal(http.ListenAndServe(":8080", nil))
```

`Handler` answers 401 for missing or invalid signatures, 400 for undecodable deliveries, 500 when your function fails and 204 otherwise. For your own server, the lower-level functions are available:

```go
func serve(w http.ResponseWriter, r *http.Request) {
	ev, err := webhook.Parse(r, secret) // reads (max 25 MB), verifies and decodes
	if errors.Is(err, webhook.ErrInvalidSignature) || errors.Is(err, webhook.ErrMissingSignature) {
		http.Error(w, "bad signature", http.StatusUnauthorized)
		return
	}
	// …
	_ = ev
}
```

Or with a body and headers you already have:

```go
if err := webhook.Verify(body, header, secret); err != nil {
	return err
}
ev, err := webhook.Decode(header, body)
```

Forgejo's test delivery (`Webhooks().Ping`) is a `push` event; GitHub's is a `ping` event.

## Administration (Forgejo)

`gol.Admin` (Forgejo only) manages accounts. User management needs a site-admin token; organisations and teams need the corresponding owner rights.

```go
admin, ok := client.(gol.Admin)
if !ok {
	log.Fatal("this backend has no administration API")
}
```

### Users

```go
users := admin.Users()

u, err := users.Create(ctx, gol.CreateUserOptions{
	Login:              "alice",
	Email:              "alice@example.com",
	Password:           "initial-password",
	FullName:           "Alice Example",
	MustChangePassword: gol.Ptr(true), // nil: Forgejo's default (true)
	Visibility:         gol.OwnerVisibilityLimited,
})

u, err = users.Edit(ctx, "alice", gol.EditUserOptions{
	Location:      gol.Ptr("Montréal"),
	ProhibitLogin: gol.Ptr(true),
})
err = users.Rename(ctx, "alice", "alice.example")
page, err := users.List(ctx, gol.ListOptions{})

// Purge also deletes the user's repositories; without it, deleting a user
// who owns repositories fails.
err = users.Delete(ctx, "alice.example", gol.DeleteUserOptions{Purge: true})
```

Forgejo 15 ignores a user visibility change back to `public`; `Edit` then returns the user together with an `ErrNotSupported` error.

### Organisations

```go
orgs := admin.Orgs()

org, err := orgs.Create(ctx, gol.CreateOrgOptions{
	Name:       "acme",
	FullName:   "Acme Inc.",
	Visibility: gol.OwnerVisibilityPrivate,
	// Owner: "alice", // create it for another user (site admin)
})
org, err = orgs.Edit(ctx, "acme", gol.EditOrgOptions{Description: gol.Ptr("Widgets since 1999")})
mine, err := orgs.List(ctx, "", gol.ListOptions{})   // the authenticated user's organisations
all, err := orgs.ListAll(ctx, gol.ListOptions{})     // every visible organisation

members, err := orgs.Members(ctx, "acme", gol.ListOptions{})
isMember, err := orgs.IsMember(ctx, "acme", "bob")
err = orgs.RemoveMember(ctx, "acme", "bob")

err = orgs.Rename(ctx, "acme", "acme-corp")
err = orgs.Delete(ctx, "acme-corp") // fails while it still owns repositories
```

Members join an organisation through its teams.

### Teams

```go
teams := admin.Teams()

// Per-unit access…
devs, err := teams.Create(ctx, "acme", gol.CreateTeamOptions{
	Name: "developers",
	Access: gol.TeamAccess{Units: map[gol.TeamUnit]gol.TeamPermission{
		gol.TeamUnitCode:   gol.TeamPermissionWrite,
		gol.TeamUnitPulls:  gol.TeamPermissionWrite,
		gol.TeamUnitIssues: gol.TeamPermissionRead,
	}},
	CanCreateOrgRepo: true,
})

// …or one level on every unit: Permission read/write, or admin.
ops, err := teams.Create(ctx, "acme", gol.CreateTeamOptions{
	Name: "ops", Access: gol.TeamAccess{Permission: gol.TeamPermissionAdmin}, IncludesAllRepos: true,
})

owners, err := teams.GetByName(ctx, "acme", "Owners")
err = teams.AddMember(ctx, devs.ID, "bob") // also makes bob an organisation member
err = teams.AddRepo(ctx, devs.ID, "acme", "widget")
members, err := teams.Members(ctx, devs.ID, gol.ListOptions{})

devs, err = teams.Edit(ctx, devs.ID, gol.EditTeamOptions{Access: &gol.TeamAccess{Permission: gol.TeamPermissionRead}})
err = teams.Delete(ctx, ops.ID)

// The owner team is protected: deleting, renaming or changing its access
// returns gol.ErrOwnerTeam.
err = teams.Delete(ctx, owners.ID) // errors.Is(err, gol.ErrOwnerTeam)
```

## Backend differences

Where the forges differ, the library either adapts or returns `ErrNotSupported`:

| Topic | Forgejo | GitHub |
|---|---|---|
| Repository default branch on create | ✅ | `ErrNotSupported` (set it afterwards with `Edit`) |
| `internal` repository visibility | `ErrNotSupported` | ✅ |
| More than one gitignore template | ✅ | `ErrNotSupported` |
| Creating repositories for another user | site admins | `ErrNotSupported` |
| `Repo.Empty` | ✅ | always `false` |
| Draft pull requests | `WIP:` title prefix | native (GraphQL for changes) |
| Merge methods | 6 | merge, squash, rebase |
| `PullRequest.Mergeable` | always set | may be `nil` while computed |
| Auto-merge | ✅ | GraphQL; needs a pending required check |
| Generated release notes, "latest" flag | `ErrNotSupported` | ✅ |
| Hidden archive links, external assets | ✅ | `ErrNotSupported` |
| Protection | protection rules | rulesets (free plans: public repositories) |
| Exempting individual users from protection | ✅ | `ErrNotSupported` (teams only) |
| Webhook branch filter, authorization header | ✅ | `ErrNotSupported` |
| Users, organisations, teams | ✅ (`Admin`) | — |

See [docs/TODO.md](docs/TODO.md) for open items and known limitations.

## Testing

Unit tests need nothing but Go:

```sh
go test -race ./...
```

Integration tests are behind the `integration` build tag and skip themselves unless their environment variables are set. They only create and delete repositories, users and organisations named `gitopslib-it-…`.

| Suite | Environment |
|---|---|
| Forgejo contract checks | `GITOPSLIB_FORGEJO_URL`, `GITOPSLIB_FORGEJO_TOKEN` (scopes: repository, organization, user, issue — read and write), `GITOPSLIB_FORGEJO_ORG` (a throwaway organisation the token's user owns) |
| GitHub contract checks | `GITOPSLIB_GITHUB_TOKEN`, `GITOPSLIB_GITHUB_ORG` (an organisation, or the account's own login) |
| Forgejo admin checks | the disposable instance below |

Admin checks need a site-admin token, so they only run against a disposable local Forgejo (Docker), and refuse any non-loopback URL:

```sh
scripts/forgejo-testenv.sh start   # starts Forgejo 15 on 127.0.0.1:3999, writes .env.testenv
set -a; source .env; source .env.testenv; set +a
go test -tags integration ./...
scripts/forgejo-testenv.sh stop    # removes the container and .env.testenv
```

GitHub's protection checks briefly create a **public** repository (rulesets need one on free plans). Running the GitHub suites many times in a row can trigger GitHub's secondary rate limit on repository creation (a temporary block of up to an hour).
