# gitoperationslib — usage guide

This guide covers every package, interface, type and option of the library, with examples and the behaviour of each backend. Start with the [README](../README.md) for an overview. [support_grid.md](support_grid.md) has the condensed feature-by-feature comparison.

## Contents

1. [Conventions used in this guide](#1-conventions-used-in-this-guide)
2. [Connecting](#2-connecting)
   - [Forgejo](#21-forgejo) · [GitHub](#22-github) · [Bitbucket Cloud](#23-bitbucket-cloud) · [Choosing at run time](#24-choosing-a-backend-at-run-time)
3. [Core concepts](#3-core-concepts)
   - [Client](#31-the-client) · [Capabilities](#32-capabilities-and-optional-interfaces) · [Errors](#33-errors) · [Pagination](#34-pagination) · [Optional fields](#35-optional-fields-and-ptr) · [Enums](#36-enums) · [JSON](#37-json)
4. [Repositories](#4-repositories)
5. [Branches](#5-branches)
6. [Tags](#6-tags)
7. [Branch protection](#7-branch-protection)
8. [Tag protection](#8-tag-protection)
9. [Releases and assets](#9-releases-and-assets)
10. [Pull requests and reviews](#10-pull-requests-and-reviews)
11. [Issues and comments](#11-issues-and-comments)
12. [Labels](#12-labels)
13. [Milestones](#13-milestones)
14. [Managing webhooks](#14-managing-webhooks)
15. [Receiving webhooks (`webhook` package)](#15-receiving-webhooks-webhook-package)
16. [Users, organisations and teams (Directory)](#16-users-organisations-and-teams-directory)
17. [Administration (Forgejo): managing users, organisations, teams](#17-administration-forgejo)
18. [Recipes](#18-recipes)
19. [Reference: errors returned without an HTTP call](#19-reference-errors-returned-without-an-http-call)

---

## 1. Conventions used in this guide

The examples use these imports:

```go
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	gol "github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/bitbucket"
	"github.com/jeanfrancoisgratton/gitoperationslib/forgejo"
	"github.com/jeanfrancoisgratton/gitoperationslib/github"
	"github.com/jeanfrancoisgratton/gitoperationslib/webhook"
)
```

- `gol` is just a short alias for the root package `gitoperationslib`.
- Unless stated otherwise, snippets assume a `ctx context.Context` and a `client gol.Client`.
- To keep the snippets short, most of them skip error checks. Real code should check every `err`.
- In this guide, *owner* means the user or organisation that owns a repository. On Bitbucket Cloud it is a **workspace** slug.
- Backend notes are tagged **Forgejo**, **GitHub** or **Bitbucket**.

---

## 2. Connecting

Each backend package has a constructor that returns a concrete `*Client`, and that type implements `gol.Client`. All three accept the same two options:

| Option | Effect |
|---|---|
| `WithHTTPClient(*http.Client)` | Custom HTTP client: timeouts, proxies, custom CA, transport middleware. The default is `http.DefaultClient`. |
| `WithUserAgent(string)` | `User-Agent` header. The default is `gitoperationslib`. |

Clients are safe for concurrent use. The few values they cache (such as the authenticated user's login) are protected by a mutex.

### 2.1 Forgejo

```go
func forgejo.New(ctx context.Context, baseURL string, creds forgejo.Credentials, opts ...forgejo.Option) (*forgejo.Client, error)
```

`New` **makes a request**: it reads the server version and refuses servers older than `forgejo.MinimumVersion` (11.0.10).

`baseURL` may be any of:
- the instance root: `https://git.example.com`
- the API root: `https://git.example.com/api/v1`
- a sub-path install: `https://example.com/forgejo`

Credentials:

| Constructor | Sends |
|---|---|
| `forgejo.Token(token)` | `Authorization: token <token>`: a personal access token, created under *Settings → Applications* |
| `forgejo.BasicAuth(user, password)` | HTTP basic authentication |
| `nil` | Nothing: anonymous access, public data only |

```go
client, err := forgejo.New(ctx, "https://git.example.com", forgejo.Token(os.Getenv("FORGEJO_TOKEN")),
	forgejo.WithHTTPClient(&http.Client{Timeout: 30 * time.Second}),
	forgejo.WithUserAgent("my-tool/1.0"),
)
if err != nil {
	var tooOld *gol.UnsupportedVersionError
	if errors.As(err, &tooOld) {
		log.Fatalf("Forgejo %s is older than the supported minimum %s", tooOld.Version, tooOld.Minimum)
	}
	log.Fatal(err)
}
fmt.Println("connected to Forgejo", client.Version()) // e.g. 15.0.2
```

Version helpers (package `forgejo`):

| Symbol | Purpose |
|---|---|
| `type Version struct{ Major, Minor, Patch int }` | A Forgejo semantic version |
| `MinimumVersion` | The oldest supported release (`11.0.10`) |
| `ParseVersion(s string) (Version, error)` | Parses strings such as `"15.0.2+gitea-1.22.0"`: only the part before `+` is used, and a leading `v` or a `-dev…` suffix is ignored |
| `(Version).Compare(o Version) int` | -1, 0 or +1 |
| `(Version).String()` | `"15.0.2"` |
| `(*Client).Version() Version` | The version detected by `New` |

```go
if client.Version().Compare(forgejo.Version{Major: 15}) >= 0 {
	// Forgejo 15 or later
}
```

The Forgejo client implements `gol.Client` and `gol.Admin`. Its `Releases()` value also implements `gol.ExternalAssets`.

### 2.2 GitHub

```go
func github.New(token string, opts ...github.Option) (*github.Client, error)
```

`New` makes no request. The token is a personal access token (fine-grained tokens are recommended) and is sent as a bearer token. An empty token means anonymous access, which is subject to GitHub's very low anonymous rate limit.

| Option | Effect |
|---|---|
| `WithEnterpriseURL(baseURL, uploadURL string)` | Targets GitHub Enterprise Server. `/api/v3/` (and `/api/uploads/` for `uploadURL`) is appended when missing; an empty `uploadURL` means `baseURL`. |
| `WithHTTPClient`, `WithUserAgent` | As above |

```go
client, err := github.New(os.Getenv("GITHUB_TOKEN"))

// GitHub Enterprise Server
client, err = github.New(token, github.WithEnterpriseURL("https://github.example.com", ""))
```

Notes:
- **Uploads and downloads.** The custom HTTP client is also used for release asset uploads (GitHub's separate upload host) and for downloads, which are redirected to GitHub's storage host. The token is never sent to the storage host.
- **Rate limits.** When GitHub enforces its primary or secondary rate limit, you get a `*gol.APIError` whose `Details` say when the limit resets or how long to wait (see [Errors](#33-errors)).
- **GraphQL.** A small GraphQL client is used where REST cannot do the job: toggling a pull request's draft status, auto-merge and its cancellation, and deleting an issue.

### 2.3 Bitbucket Cloud

```go
func bitbucket.New(creds bitbucket.Credentials, opts ...bitbucket.Option) (*bitbucket.Client, error)
```

`New` makes no request. Credentials:

| Constructor | For | Sends |
|---|---|---|
| `bitbucket.APIToken(email, token)` | Atlassian API tokens with Bitbucket scopes (`ATATT…`) | Basic authentication with the Atlassian account e-mail. This is the only way Bitbucket accepts these tokens. |
| `bitbucket.Token(token)` | Workspace, project or repository access tokens (`ATCTT…`) | `Authorization: Bearer <token>`. Atlassian API tokens are refused this way (HTTP 401). |
| `bitbucket.BasicAuth(user, password)` | Username (or account e-mail) and an app password or API token | Basic authentication |
| `nil` | Anonymous access | Nothing |

| Option | Effect |
|---|---|
| `WithBaseURL(url)` | Overrides the API root. The default is `https://api.bitbucket.org/2.0/`. Mostly useful for tests. |
| `WithHTTPClient`, `WithUserAgent` | As above |

```go
client, err := bitbucket.New(bitbucket.APIToken("alice@example.com", os.Getenv("BITBUCKET_TOKEN")))
client, err = bitbucket.New(bitbucket.Token(os.Getenv("BITBUCKET_ACCESS_TOKEN")))
```

Bitbucket specifics that apply everywhere:
- **Owners are workspaces.** New repositories go to the workspace's default project.
- **No public login names.** Fields such as `Author`, `MergedBy`, `Reviewer` or `Allowlist.Users` hold the account's **nickname** (or its display name when there is no nickname). Where a user is an *input* (reviewers, allowlists), you can pass a nickname, an account ID or a `{UUID}`. Nicknames and display names are looked up among the workspace's members.
- **Empty owner.** Where the owner may be empty, the library uses the user's only workspace. If the user belongs to several workspaces, the call fails and asks you to name one.

### 2.4 Choosing a backend at run time

All three `*Client` types implement `gol.Client`, so the rest of your code does not need to know which forge it talks to:

```go
func open(ctx context.Context, forge string) (gol.Client, error) {
	switch forge {
	case "forgejo":
		return forgejo.New(ctx, os.Getenv("FORGEJO_URL"), forgejo.Token(os.Getenv("FORGEJO_TOKEN")))
	case "github":
		return github.New(os.Getenv("GITHUB_TOKEN"))
	case "bitbucket":
		return bitbucket.New(bitbucket.APIToken(os.Getenv("BITBUCKET_EMAIL"), os.Getenv("BITBUCKET_TOKEN")))
	}
	return nil, fmt.Errorf("unknown forge %q", forge)
}
```

> A typed nil pointer stored in an interface is not `nil`. Each `case` above returns the constructor's two values directly, so on failure the caller gets a non-nil error and can ignore the client value.

---

## 3. Core concepts

### 3.1 The client

```go
type Client interface {
	Backend() Backend          // BackendForgejo, BackendGitHub or BackendBitbucket
	Capabilities() Capability  // bit set; see below
	Repos() Repos
	Branches() Branches
	Tags() Tags
	BranchProtections() BranchProtections
	TagProtections() TagProtections
	Releases() Releases
	PullRequests() PullRequests
	Issues() Issues
	Labels() Labels
	Milestones() Milestones
	Webhooks() Webhooks
	Directory() Directory
}
```

The accessors are cheap: they return small values bound to the client, so you can call them inline (`client.Repos().Get(…)`) or keep the result in a variable. Every backend implements every area interface. Where a forge has no equivalent, the methods return `ErrNotSupported`.

### 3.2 Capabilities and optional interfaces

`Capabilities()` returns a `gol.Capability` bit set:

| Constant | Name (`String()`) | Forgejo | GitHub | Bitbucket |
|---|---|---|---|---|
| `CapRepos` | `repos` | ✅ | ✅ | ✅ |
| `CapBranches` | `branches` | ✅ | ✅ | ✅ |
| `CapTags` | `tags` | ✅ | ✅ | ✅ |
| `CapBranchProtection` | `branch-protection` | ✅ | ✅ | ✅ |
| `CapTagProtection` | `tag-protection` | ✅ | ✅ | |
| `CapReleases` | `releases` | ✅ | ✅ | |
| `CapExternalAssets` | `external-assets` | ✅ | | |
| `CapPullRequests` | `pull-requests` | ✅ | ✅ | ✅ |
| `CapIssues` | `issues` (also labels, milestones) | ✅ | ✅ | |
| `CapWebhooks` | `webhooks` | ✅ | ✅ | ✅ |
| `CapDirectory` | `directory` | ✅ | ✅ | |
| `CapAdmin` | `admin` | ✅ | | |
| `CapMirrors`, `CapMigrate`, `CapSystemWebhooks` | reserved for future features | | | |

```go
caps := client.Capabilities()
fmt.Println(client.Backend(), caps) // "forgejo repos|admin|webhooks|releases|…"

if caps.Has(gol.CapReleases | gol.CapIssues) { // Has requires every bit
	// …
}
```

Features only some backends have are separate interfaces. Detect them with a type assertion:

```go
// Users, organisations, teams: Forgejo only.
if admin, ok := client.(gol.Admin); ok {
	_ = admin.Users()
}

// Release assets that link to external files: Forgejo only.
if ext, ok := client.Releases().(gol.ExternalAssets); ok {
	_ = ext
}
```

### 3.3 Errors

| Error | Matched with | Meaning |
|---|---|---|
| `gol.ErrNotSupported` | `errors.Is` | The backend cannot perform the operation or honour an option. `errors.As` gives a `*gol.NotSupportedError{Backend, Operation, Reason}`. |
| `*gol.APIError` | `errors.As` | The server answered with an error. Fields: `Backend`, `StatusCode`, `Message`, `DocumentationURL` (GitHub's `documentation_url`, Forgejo's `url`), `Details` (GitHub's `errors[]`, plus rate-limit reset or retry-after information). |
| `*gol.UnsupportedVersionError` | `errors.As` | `forgejo.New`: the server is older than the supported minimum. Fields: `Backend`, `Version`, `Minimum`. |
| `gol.ErrOwnerTeam` | `errors.Is` | The call would delete or rename an organisation's owner team, or change its permissions, so it was refused. |

Helpers, which look for the first `*APIError` in the error chain:

| Helper | True when |
|---|---|
| `gol.StatusCode(err) int` | — (returns the HTTP status, or 0 if there is no `*APIError`) |
| `gol.IsNotFound(err)` | HTTP 404 |
| `gol.IsUnauthorized(err)` | HTTP 401 |
| `gol.IsForbidden(err)` | HTTP 403 |

```go
repo, err := client.Repos().Get(ctx, "acme", "widget")
switch {
case err == nil:
	fmt.Println(repo.FullName)
case gol.IsNotFound(err):
	fmt.Println("no such repository")
case gol.IsUnauthorized(err), gol.IsForbidden(err):
	fmt.Println("check the token and its scopes")
case errors.Is(err, gol.ErrNotSupported):
	var ns *gol.NotSupportedError
	errors.As(err, &ns)
	fmt.Printf("%s cannot %s: %s\n", ns.Backend, ns.Operation, ns.Reason)
default:
	var apiErr *gol.APIError
	if errors.As(err, &apiErr) {
		fmt.Println(apiErr.StatusCode, apiErr.Message, apiErr.Details, apiErr.DocumentationURL)
	} else {
		fmt.Println("transport error:", err) // network, context cancelled, …
	}
}
```

**Result and error together.** A few calls return a valid result **and** an error. This happens when the server applied the request but silently ignored part of it, and the result shows the server's actual state:

| Call | When |
|---|---|
| `Webhooks().Create` / `Edit` (Forgejo) | Forgejo dropped event names it does not know. The error names them. |
| `PullRequests().Create` / `Edit` (Forgejo) | The instance does not treat the `WIP:` prefix as a draft marker (custom prefixes are configured). Matches `ErrNotSupported`. |
| `Users().Edit` (Forgejo) | Forgejo 15 ignored a switch of visibility back to `public`. Matches `ErrNotSupported`. |

So always check the result before discarding it on error:

```go
hook, err := client.Webhooks().Create(ctx, scope, opts)
if err != nil && hook == nil {
	log.Fatal(err) // nothing was created
}
if err != nil {
	log.Printf("webhook %d created, but: %v", hook.ID, err)
}
```

**Validation errors.** Some arguments are checked before any request is sent, such as a missing required field or an invalid enum value. These errors are plain errors, not `*APIError`. See [§19](#19-reference-errors-returned-without-an-http-call).

### 3.4 Pagination

```go
type ListOptions struct {
	Page    int // 1-based; 0 means the first page
	PerPage int // 0 means the server default
}

type Page[T any] struct {
	Items    []T
	NextPage int // 0 on the last page
}
```

Backend notes:
- **Forgejo** maps these to `page` and `limit`. The server caps `limit` at its configured maximum (50 by default).
- **GitHub** maps them to `page` and `per_page` (maximum 100).
- **Bitbucket** maps them to `page` and `pagelen`, and caps `PerPage` at 100.

To walk one page at a time:

```go
opts := gol.ListOptions{PerPage: 50}
for {
	page, err := client.Repos().List(ctx, "acme", opts)
	if err != nil {
		log.Fatal(err)
	}
	for _, r := range page.Items {
		fmt.Println(r.FullName)
	}
	if page.NextPage == 0 {
		break
	}
	opts.Page = page.NextPage
}
```

Or iterate over every item with `gol.All`, a Go 1.23+ range-over-func iterator. It starts at `opts.Page`, fetches pages as needed, and stops at the first error, which it yields once with a zero item:

```go
func All[T any](ctx context.Context, opts ListOptions, fetch func(context.Context, ListOptions) (Page[T], error)) iter.Seq2[T, error]
```

```go
for pr, err := range gol.All(ctx, gol.ListOptions{PerPage: 100},
	func(ctx context.Context, o gol.ListOptions) (gol.Page[gol.PullRequest], error) {
		return client.PullRequests().List(ctx, "acme", "widget", gol.ListPROptions{ListOptions: o, State: gol.PRStateAll})
	}) {
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(pr.Number, pr.Title)
}
```

Breaking out of the loop stops fetching. Option structs that embed `ListOptions` (`ListPROptions`, `ListIssuesOptions`) work the same way, as shown above.

A few lists are not paginated. `BranchProtections().List` and `TagProtections().List` return a plain slice. Forgejo's `Issues().ListComments` returns every comment in one page.

### 3.5 Optional fields and `Ptr`

In every `Edit…Options` struct, a **nil pointer leaves the field unchanged**, and a non-nil pointer sets it, even to a zero value. `gol.Ptr` builds pointers inline:

```go
func Ptr[T any](v T) *T
```

```go
_, err := client.Repos().Edit(ctx, "acme", "widget", gol.EditRepoOptions{
	Description: gol.Ptr(""),     // clears the description
	Archived:    gol.Ptr(true),
	// Website: nil → unchanged
})
```

Slices in edit options use a pointer to the slice (`*[]string`). In those fields, `nil` means unchanged and `&[]string{}` clears the list. `EditWebhookOptions.Events` is the exception: it is a plain slice, and a non-nil value replaces the list.

### 3.6 Enums

Enums are typed strings. The types with a fixed set of values have a `Valid()` method.

| Type | Values | Notes |
|---|---|---|
| `Backend` | `BackendForgejo`, `BackendGitHub`, `BackendBitbucket` | |
| `Visibility` | `VisibilityPublic`, `VisibilityPrivate`, `VisibilityInternal` | Repositories. `internal` is GitHub Enterprise only. The zero value means the backend default. |
| `OwnerVisibility` | `OwnerVisibilityPublic`, `OwnerVisibilityLimited` (signed-in users only), `OwnerVisibilityPrivate` | Users and organisations (Forgejo) |
| `MergeMethod` | `MergeMethodMerge`, `MergeMethodSquash`, `MergeMethodRebase`, `MergeMethodRebaseMerge`, `MergeMethodFastForwardOnly`, `MergeMethodManuallyMerged` | See [Merging](#103-merging) |
| `PRState` | `PRStateOpen`, `PRStateClosed` (includes merged), `PRStateAll` (filter only) | |
| `IssueState` | `IssueStateOpen`, `IssueStateClosed`, `IssueStateAll` (filter only) | |
| `MilestoneState` | `MilestoneStateOpen`, `MilestoneStateClosed`, `MilestoneStateAll` (filter only) | |
| `ReviewEvent` | `ReviewEventApprove`, `ReviewEventRequestChanges`, `ReviewEventComment` | The verdict you submit |
| `ReviewState` | `ReviewApproved`, `ReviewChangesRequested`, `ReviewCommented`, `ReviewPending`, `ReviewDismissed` (GitHub), `ReviewRequested` (Forgejo) | What a review reports |
| `HookEvent` | see [§14](#14-managing-webhooks) | Open set: other values pass through |
| `HookContentType` | `HookContentJSON`, `HookContentForm` | |
| `TeamPermission` | `TeamPermissionNone`, `Read`, `Write`, `Admin`, `Owner` (reported only) | |
| `TeamUnit` | `TeamUnitCode`, `Issues`, `ExternalIssues`, `Wiki`, `ExternalWiki`, `Pulls`, `Releases`, `Projects`, `Packages`, `Actions`; `AllTeamUnits` lists them | |

### 3.7 JSON

Every public struct (results, option structs, error types and the `webhook` events) has JSON tags, so you can print results with `json.Marshal` or load options from a file. The key names are part of the API and only change in a minor release, noted in the changelog.

- **Keys are camelCase.** Acronyms are written as words: `fullName`, `htmlUrl`, `sshUrl`, `mergeCommitSha`, `keyId`.
- **Embedded structs are flattened.** `UserInfo` and `CurrentUser` show the `User` fields at the top level, and `ListIssuesOptions` takes `page` and `perPage` directly.
- **Common fields are always present**, even when empty (`"email": ""`), so every backend gives the same shape.
- **Fields some backends don't report are omitted when empty**: the ones documented as "GitHub only" or "Forgejo only", optional pointers (`mergeable`, `twoFactorAuth`, `pushAllowlist`) and `missing`. A nil pointer is omitted, but `false` is still written, so "not reported" and "disabled" stay distinct.
- **Zero times are omitted**, e.g. `lastLogin` for a user who never signed in or `expiresAt` for a key that does not expire.
- **Option structs omit everything not set**, apart from their required fields. Nil `Edit…Options` fields stay nil when unmarshalled, so "leave unchanged" survives a round trip.
- **Secrets are never serialised.** `Password` (user options), `Secret` and `AuthorizationHeader` (webhook options) are tagged `json:"-"`. Set them in code.
- **`Incomplete` is not serialised**, because an `error` would come out as `{}`. Info results carry the same information in `missing`; see [§16.3](#163-info-calls).
- **`webhook.Event.Payload`** is a `json.RawMessage`, so it is embedded as JSON, not base64.

```go
me, err := client.Directory().Me(ctx)
if err != nil {
	log.Fatal(err)
}
out, _ := json.MarshalIndent(me, "", "  ")
fmt.Println(string(out))
// {
//   "id": 3,
//   "login": "alice",
//   "fullName": "Alice",
//   "htmlUrl": "https://git.example.com/alice",
//   ...
//   "missing": [
//     {"part": "SSH keys", "status": 403, "message": "forgejo: HTTP 403: token does not have …"}
//   ]
// }
```

---

## 4. Repositories

```go
type Repos interface {
	Get(ctx, owner, name string) (*Repo, error)
	List(ctx, owner string, opts ListOptions) (Page[Repo], error)
	Create(ctx, opts CreateRepoOptions) (*Repo, error)
	Edit(ctx, owner, name string, opts EditRepoOptions) (*Repo, error)
	Delete(ctx, owner, name string) error
}
```

### 4.1 The `Repo` type

| Field | Notes |
|---|---|
| `ID int64` | **Bitbucket:** always 0, because Bitbucket identifies repositories by UUID |
| `Owner`, `Name`, `FullName` | `FullName` is `"owner/name"`. On Bitbucket, `Name` is the slug. |
| `Description`, `Website` | `Website` is Forgejo's `website` and GitHub's `homepage` |
| `Visibility` | `public`, `private` or `internal` |
| `DefaultBranch` | |
| `Fork`, `Mirror`, `Archived`, `Template` | |
| `Empty` | No commits yet. **GitHub, Bitbucket:** always false (not reported). |
| `HTMLURL`, `CloneURL` (HTTPS), `SSHURL` | |
| `CreatedAt`, `UpdatedAt` | |

### 4.2 Creating

```go
type CreateRepoOptions struct {
	Owner         string     // empty: the authenticated user (Bitbucket: their only workspace)
	Name          string     // required
	Description   string
	Visibility    Visibility // zero value: backend default
	DefaultBranch string
	Template      bool       // mark as a template repository
	AutoInit      bool       // create an initial commit
	Gitignores    []string   // gitignore template names, used with AutoInit
	License       string     // license template name, used with AutoInit
}
```

```go
repo, err := client.Repos().Create(ctx, gol.CreateRepoOptions{
	Owner:       "acme",
	Name:        "widget",
	Description: "Widget service",
	Visibility:  gol.VisibilityPrivate,
	AutoInit:    true,
	Gitignores:  []string{"Go"},
	License:     "MIT",
})
```

`Owner` can name the authenticated user, an organisation, or (on Forgejo, with a site-admin token) **another user**. The backend works out which one it is.

| Option | Forgejo | GitHub | Bitbucket |
|---|---|---|---|
| `Owner` = another user | ✅ site admins | ❌ `ErrNotSupported` | — (workspaces only) |
| `VisibilityInternal` | ❌ | ✅ (Enterprise) | ❌ |
| `DefaultBranch` | ✅ | ❌ (set it afterwards with `Edit`) | ⚠️ only with `AutoInit` |
| `AutoInit` | ✅ | ✅ | ⚠️ commits a `README.md` |
| `Gitignores` | ✅ several | ⚠️ at most one | ❌ |
| `License` | ✅ | ✅ | ❌ |
| `Template` | ✅ | ✅ | ❌ |

### 4.3 Reading and listing

```go
repo, err := client.Repos().Get(ctx, "acme", "widget")

page, err := client.Repos().List(ctx, "acme", gol.ListOptions{PerPage: 50}) // a user's or organisation's repositories
mine, err := client.Repos().List(ctx, "", gol.ListOptions{})               // everything the user can access
```

With an empty owner, `List` returns every repository the authenticated user can access, including those of their organisations. **Bitbucket** has no cross-workspace listing, so the library lists each workspace in turn. With a single workspace the results are paged normally. With several, everything comes back in a single page.

### 4.4 Editing

```go
type EditRepoOptions struct {
	Name          *string // rename
	Description   *string
	Website       *string
	Visibility    *Visibility
	DefaultBranch *string
	Archived      *bool
	Template      *bool
}
```

```go
repo, err := client.Repos().Edit(ctx, "acme", "widget", gol.EditRepoOptions{
	Name:          gol.Ptr("widget-service"),
	Website:       gol.Ptr("https://widget.example.com"),
	Visibility:    gol.Ptr(gol.VisibilityPublic),
	DefaultBranch: gol.Ptr("main"),
})
```

- `DefaultBranch` is the only way to change the default branch, on every backend.
- **Bitbucket:** renaming changes the slug, and therefore every URL. `Archived` and `Template` return `ErrNotSupported`.

### 4.5 Deleting

```go
err := client.Repos().Delete(ctx, "acme", "widget-service")
```

Deletion is immediate and permanent on every backend.

---

## 5. Branches

```go
type Branches interface {
	List(ctx, owner, repo string, opts ListOptions) (Page[Branch], error)
	Get(ctx, owner, repo, name string) (*Branch, error)
	Create(ctx, owner, repo string, opts CreateBranchOptions) (*Branch, error)
	Rename(ctx, owner, repo, name, newName string) (*Branch, error)
	Delete(ctx, owner, repo, name string) error
}

type Branch struct {
	Name      string
	SHA       string // head commit
	Protected bool   // Bitbucket: always false
	IsDefault bool
}

type CreateBranchOptions struct {
	Name string // required
	From string // branch, tag or commit SHA; empty: the default branch
}
```

```go
b := client.Branches()

br, err := b.Create(ctx, "acme", "widget", gol.CreateBranchOptions{Name: "feature/login"})              // from the default branch
br, err = b.Create(ctx, "acme", "widget", gol.CreateBranchOptions{Name: "hotfix/1.0.1", From: "v1.0.0"}) // from a tag
br, err = b.Create(ctx, "acme", "widget", gol.CreateBranchOptions{Name: "bisect", From: "3f2a9c1…"})    // from a commit

br, err = b.Get(ctx, "acme", "widget", "feature/login")
fmt.Println(br.Name, br.SHA, br.Protected, br.IsDefault)

page, err := b.List(ctx, "acme", "widget", gol.ListOptions{})
for _, br := range page.Items {
	if br.IsDefault {
		fmt.Println("default branch:", br.Name)
	}
}

br, err = b.Rename(ctx, "acme", "widget", "feature/login", "feature/sign-in")
err = b.Delete(ctx, "acme", "widget", "feature/sign-in")
```

- `Rename` keeps open pull requests and protection rules attached to the branch. **Bitbucket** has no rename API and returns `ErrNotSupported`.
- Deleting a protected branch is refused by the server: HTTP 403 on Forgejo, 422 on GitHub, and 500 on Bitbucket (which is still a refusal).
- To change the default branch, use `Repos().Edit` with `DefaultBranch`.

---

## 6. Tags

```go
type Tags interface {
	List(ctx, owner, repo string, opts ListOptions) (Page[Tag], error)
	Get(ctx, owner, repo, name string) (*Tag, error)
	Create(ctx, owner, repo string, opts CreateTagOptions) (*Tag, error)
	Delete(ctx, owner, repo, name string) error
}

type Tag struct {
	Name       string
	SHA        string // the commit; annotated tags are peeled
	Message    string // annotation; empty for lightweight tags
	TarballURL string
	ZipballURL string
}

type CreateTagOptions struct {
	Name    string // required
	Target  string // branch, tag or commit SHA; empty: the default branch
	Message string // non-empty: annotated tag; empty: lightweight tag
}
```

```go
t := client.Tags()

tag, err := t.Create(ctx, "acme", "widget", gol.CreateTagOptions{Name: "v1.0.0"}) // lightweight, on the default branch
tag, err = t.Create(ctx, "acme", "widget", gol.CreateTagOptions{
	Name: "v1.1.0", Target: "main", Message: "Release 1.1.0", // annotated
})

tag, err = t.Get(ctx, "acme", "widget", "v1.1.0")
fmt.Println(tag.Name, tag.SHA, tag.Message, tag.TarballURL)

page, err := t.List(ctx, "acme", "widget", gol.ListOptions{})
err = t.Delete(ctx, "acme", "widget", "v1.0.0")
```

- **GitHub:** `List` does not report `Message`; `Get` does.
- **Bitbucket:** every tag is annotated. A "lightweight" tag gets an annotation generated by Bitbucket, and it reads back as an empty `Message`.
- To publish a release, use `Releases().Create`, which creates the tag when needed (see [§9](#9-releases-and-assets)).

---

## 7. Branch protection

```go
type BranchProtections interface {
	List(ctx, owner, repo string) ([]BranchProtection, error)
	Get(ctx, owner, repo, pattern string) (*BranchProtection, error)
	Create(ctx, owner, repo string, rule BranchProtection) (*BranchProtection, error)
	Update(ctx, owner, repo, pattern string, rule BranchProtection) (*BranchProtection, error)
	Delete(ctx, owner, repo, pattern string) error
}
```

Rules are addressed by **pattern**: a branch name (`main`) or a glob (`release/*`). How each forge implements them:

| | Forgejo | GitHub | Bitbucket |
|---|---|---|---|
| Implemented as | branch protection rules | repository **rulesets** (one per pattern) | branch **restrictions** (one per setting and pattern) |
| Availability | all plans | public repositories on free plans | all plans (some settings need Premium) |

### 7.1 The `BranchProtection` type

**The zero value is the protective choice:** deletion and force pushes are blocked unless explicitly allowed.

| Field | Meaning | Forgejo | GitHub | Bitbucket |
|---|---|---|---|---|
| `ID int64` | GitHub's ruleset ID | 0 | ✅ | 0 |
| `Pattern string` | Branch name or glob | ✅ | ✅ | ✅ |
| `RequiredApprovals int` | Approving reviews needed to merge | ✅ | ✅ | ✅ |
| `DismissStaleApprovals bool` | A new push dismisses earlier approvals | ✅ | ✅ | ⚠️ Premium plans |
| `RequiredStatusChecks []string` | Status check contexts that must pass | ✅ | ✅ | ❌ (Bitbucket only counts passing builds) |
| `RequireUpToDate bool` | The branch must be up to date with the base before merging | ✅ | ⚠️ only together with status checks | ❌ |
| `RequireSignedCommits bool` | | ✅ | ✅ | ❌ |
| `ApplyToAdmins bool` | Enforce the rule for repository admins too | ✅ | ✅ (when false, the admin role bypasses the rule) | ⚠️ no equivalent: ignored, and reported as true |
| `PushAllowlist *Allowlist` | Who may push directly (see below) | ✅ users and teams | ⚠️ teams only; they bypass the whole ruleset | ✅ users, ⚠️ teams = groups |
| `AllowDeletion bool` | | ❌ | ✅ | ✅ |
| `AllowForcePush bool` | | ❌ | ✅ | ✅ |
| `RequireLinearHistory bool` | | ❌ | ✅ | ❌ |
| `RequireConversationResolution bool` | | ❌ | ✅ | ❌ |
| `MergeAllowlist *Allowlist` | Who may merge pull requests | ✅ | ❌ | ✅ |
| `ApprovalsAllowlist *Allowlist` | Whose approvals count | ✅ | ❌ | ❌ |
| `BlockOnRejectedReviews bool` | | ✅ | ❌ | ❌ |
| `BlockOnOfficialReviewRequests bool` | | ✅ | ❌ | ❌ |
| `ProtectedFilePatterns string` | Semicolon-separated globs of files that cannot be changed | ✅ | ❌ | ❌ |
| `UnprotectedFilePatterns string` | Files exempt from push restrictions | ✅ | ❌ | ❌ |

❌ means that setting the field (to `true`, a non-nil allowlist or a non-empty value) makes `Create` or `Update` fail with `ErrNotSupported`. Nothing is sent to the server in that case. Leaving the field at its zero value is always accepted.

`Allowlist`:

```go
type Allowlist struct {
	Users []string // logins (Bitbucket: nickname, account ID or {UUID})
	Teams []string // team names (GitHub: team slugs, organisations only)
}
```

How `PushAllowlist` is interpreted:
- `nil`: anyone with write access may push.
- `&gol.Allowlist{}` (empty): nobody may push directly, so changes must go through pull requests.
- `&gol.Allowlist{Teams: …, Users: …}`: only the listed users and teams may push.

`MergeAllowlist` and `ApprovalsAllowlist` work the same way: `nil` means anyone with write access.

### 7.2 Examples

```go
bp := client.BranchProtections()

// A typical protected main branch.
rule, err := bp.Create(ctx, "acme", "widget", gol.BranchProtection{
	Pattern:               "main",
	RequiredApprovals:     1,
	DismissStaleApprovals: true,
	RequiredStatusChecks:  []string{"ci/test"},
	RequireUpToDate:       true,
	RequireSignedCommits:  true,
	ApplyToAdmins:         true,
	PushAllowlist:         &gol.Allowlist{Teams: []string{"release-managers"}},
})

// Glob rule: two approvals for every release branch, no direct pushes.
_, err = bp.Create(ctx, "acme", "widget", gol.BranchProtection{
	Pattern:           "release/*",
	RequiredApprovals: 2,
	PushAllowlist:     &gol.Allowlist{},
})

// Forgejo-only extras.
_, err = bp.Create(ctx, "acme", "widget", gol.BranchProtection{
	Pattern:                "stable",
	MergeAllowlist:         &gol.Allowlist{Users: []string{"alice"}},
	ApprovalsAllowlist:     &gol.Allowlist{Teams: []string{"reviewers"}},
	BlockOnRejectedReviews: true,
	ProtectedFilePatterns:  "go.mod;go.sum;.forgejo/**",
})

// GitHub-only extras.
_, err = bp.Create(ctx, "acme", "widget", gol.BranchProtection{
	Pattern:                       "main",
	RequireLinearHistory:          true,
	RequireConversationResolution: true,
})
```

`Update` **replaces** the whole rule, so read it first, change it, then write it back:

```go
rule, err := bp.Get(ctx, "acme", "widget", "main")
rule.RequiredApprovals = 2
rule, err = bp.Update(ctx, "acme", "widget", "main", *rule)
```

- **Forgejo:** a rule cannot be renamed. If `rule.Pattern` differs from the `pattern` argument, `Update` returns `ErrNotSupported`; delete the rule and create a new one instead.
- **Bitbucket:** `Update` is not atomic. It deletes the pattern's restrictions and then recreates them, so if a call fails halfway through, the branch may be left partly unprotected.

```go
rules, err := bp.List(ctx, "acme", "widget")
for _, r := range rules {
	fmt.Println(r.Pattern, r.RequiredApprovals)
}
err = bp.Delete(ctx, "acme", "widget", "release/*")
```

---

## 8. Tag protection

```go
type TagProtections interface {
	List(ctx, owner, repo string) ([]TagProtection, error)
	Get(ctx, owner, repo, pattern string) (*TagProtection, error)
	Create(ctx, owner, repo string, rule TagProtection) (*TagProtection, error)
	Update(ctx, owner, repo, pattern string, rule TagProtection) (*TagProtection, error)
	Delete(ctx, owner, repo, pattern string) error
}

type TagProtection struct {
	ID        int64     // GitHub ruleset ID
	Pattern   string    // "v*"
	Allowlist Allowlist // who may create, move or delete matching tags
}
```

```go
tp := client.TagProtections()

tr, err := tp.Create(ctx, "acme", "widget", gol.TagProtection{
	Pattern:   "v*",
	Allowlist: gol.Allowlist{Teams: []string{"release-managers"}},
})
tr, err = tp.Update(ctx, "acme", "widget", "v*", gol.TagProtection{
	Pattern:   "v*",
	Allowlist: gol.Allowlist{Users: []string{"alice"}}, // users: Forgejo only
})
rules, err := tp.List(ctx, "acme", "widget")
err = tp.Delete(ctx, "acme", "widget", "v*")
```

| | Forgejo | GitHub | Bitbucket |
|---|---|---|---|
| Implemented as | protected tags | tag rulesets | ❌ `ErrNotSupported` |
| Users in the allowlist | ✅ | ❌ | — |
| Teams in the allowlist | ✅ | ✅ organisation teams | — |
| Empty allowlist | ❌ (at least one entry required) | ✅ blocks everyone, admins included | — |

---

## 9. Releases and assets

```go
type Releases interface {
	List(ctx, owner, repo string, opts ListOptions) (Page[Release], error)
	Get(ctx, owner, repo string, id int64) (*Release, error)
	GetByTag(ctx, owner, repo, tag string) (*Release, error)
	Latest(ctx, owner, repo string) (*Release, error)
	Create(ctx, owner, repo string, opts CreateReleaseOptions) (*Release, error)
	Edit(ctx, owner, repo string, id int64, opts EditReleaseOptions) (*Release, error)
	Delete(ctx, owner, repo string, id int64) error

	ListAssets(ctx, owner, repo string, releaseID int64, opts ListOptions) (Page[Asset], error)
	GetAsset(ctx, owner, repo string, releaseID, assetID int64) (*Asset, error)
	UploadAsset(ctx, owner, repo string, releaseID int64, name string, r io.Reader, size int64, contentType string) (*Asset, error)
	EditAsset(ctx, owner, repo string, releaseID, assetID int64, opts EditAssetOptions) (*Asset, error)
	DeleteAsset(ctx, owner, repo string, releaseID, assetID int64) error
	DownloadAsset(ctx, owner, repo string, releaseID, assetID int64) (io.ReadCloser, error)
}
```

**Bitbucket** has no releases: every method returns `ErrNotSupported`.

Asset methods take both the release ID and the asset ID: Forgejo needs both, and GitHub ignores the release ID.

### 9.1 Types

`Release`:

| Field | Notes |
|---|---|
| `ID int64` | |
| `TagName`, `TargetCommitish` | `TargetCommitish` is the branch or commit the tag is created from |
| `Name`, `Body` | Title, and notes in Markdown |
| `Draft`, `Prerelease` | |
| `Author` | Login |
| `HTMLURL`, `TarballURL`, `ZipballURL` | Source archives generated by the forge |
| `CreatedAt`, `PublishedAt` | `PublishedAt` is zero for drafts |
| `Assets []Asset` | |

`Asset`:

| Field | Notes |
|---|---|
| `ID`, `Name`, `Size`, `DownloadCount` | |
| `ContentType` | Reported by GitHub only |
| `DownloadURL` | Browser download URL (for external assets: the linked URL) |
| `External bool` | Forgejo: a link to a file hosted elsewhere |
| `CreatedAt` | |

### 9.2 Creating and publishing

```go
type CreateReleaseOptions struct {
	TagName         string // required; created from TargetCommitish if missing
	TargetCommitish string // empty: the default branch
	Name            string
	Body            string
	Draft           bool
	Prerelease      bool

	GenerateReleaseNotes bool  // GitHub only
	MakeLatest           *bool // GitHub only; nil: the server decides
	HideArchiveLinks     *bool // Forgejo only
}
```

If `TagName` does not exist yet, the forge creates it from `TargetCommitish`. For a **draft**, the tag is only created when the draft is published.

The usual flow is to create a draft, upload the files, then publish:

```go
rels := client.Releases()

rel, err := rels.Create(ctx, "acme", "widget", gol.CreateReleaseOptions{
	TagName:         "v1.0.1",
	TargetCommitish: "main",
	Name:            "1.0.1",
	Body:            "## Changes\n- Fixed the login bug",
	Draft:           true,
})

// Upload: any io.Reader, with its exact size.
f, err := os.Open("dist/tool_1.0.1_linux_amd64.tar.gz")
if err != nil {
	log.Fatal(err)
}
defer f.Close()
info, err := f.Stat()
if err != nil {
	log.Fatal(err)
}
asset, err := rels.UploadAsset(ctx, "acme", "widget", rel.ID, "tool_1.0.1_linux_amd64.tar.gz", f, info.Size(), "")
fmt.Println(asset.ID, asset.Name, asset.Size, asset.DownloadURL)

// Publish.
rel, err = rels.Edit(ctx, "acme", "widget", rel.ID, gol.EditReleaseOptions{Draft: gol.Ptr(false)})
```

`UploadAsset`:
- It streams the reader and never buffers the whole file in memory. `size` must be exact.
- An empty `contentType` is guessed from the file name's extension, falling back to `application/octet-stream`.
- **Forgejo** sends the file as multipart on the API host, subject to the instance's attachment size and type limits. **GitHub** sends a raw body to its upload host.

Backend-specific options:

```go
// GitHub: notes generated from merged pull requests, and the "latest" flag.
_, err := rels.Create(ctx, "acme", "widget", gol.CreateReleaseOptions{
	TagName: "v2.0.0", GenerateReleaseNotes: true, MakeLatest: gol.Ptr(true),
})

// Forgejo: hide the generated source archive links.
_, err = rels.Create(ctx, "acme", "widget", gol.CreateReleaseOptions{
	TagName: "v2.0.0", HideArchiveLinks: gol.Ptr(true),
})
```

On the other backend, these options return `ErrNotSupported`.

### 9.3 Reading

```go
rel, err := rels.Get(ctx, "acme", "widget", id)
rel, err = rels.GetByTag(ctx, "acme", "widget", "v1.0.1")
latest, err := rels.Latest(ctx, "acme", "widget") // newest published release that is not a prerelease
page, err := rels.List(ctx, "acme", "widget", gol.ListOptions{})
```

`Latest` returns a 404 `*APIError` (`gol.IsNotFound`) when there is no published release.

### 9.4 Editing and deleting

```go
type EditReleaseOptions struct {
	TagName, TargetCommitish, Name, Body *string
	Draft, Prerelease                    *bool // Draft false publishes a draft
	MakeLatest                           *bool // GitHub only
	HideArchiveLinks                     *bool // Forgejo only
}
```

```go
rel, err = rels.Edit(ctx, "acme", "widget", rel.ID, gol.EditReleaseOptions{
	Body:       gol.Ptr("## Changes\n- Fixed the login bug\n- Faster startup"),
	Prerelease: gol.Ptr(false),
})
err = rels.Delete(ctx, "acme", "widget", rel.ID) // the tag is kept; delete it with Tags().Delete
```

### 9.5 Assets

```go
assets, err := rels.ListAssets(ctx, "acme", "widget", rel.ID, gol.ListOptions{})
asset, err := rels.GetAsset(ctx, "acme", "widget", rel.ID, assetID)
asset, err = rels.EditAsset(ctx, "acme", "widget", rel.ID, assetID, gol.EditAssetOptions{Name: gol.Ptr("tool-linux.tar.gz")})
err = rels.DeleteAsset(ctx, "acme", "widget", rel.ID, assetID)

// Streamed download (private repositories included); close the reader.
rc, err := rels.DownloadAsset(ctx, "acme", "widget", rel.ID, assetID)
if err != nil {
	log.Fatal(err)
}
defer rc.Close()
out, err := os.Create("tool.tar.gz")
if err != nil {
	log.Fatal(err)
}
defer out.Close()
_, err = io.Copy(out, rc)
```

`DownloadAsset` does not download external assets; it returns an error that names their URL. Fetch `asset.DownloadURL` yourself instead.

### 9.6 External assets (Forgejo)

`gol.ExternalAssets`, implemented by the Forgejo backend's `Releases()`, adds an asset that only *links* to a file hosted elsewhere:

```go
type ExternalAssets interface {
	AddExternalAsset(ctx, owner, repo string, releaseID int64, name, url string) (*Asset, error)
}
```

```go
if ext, ok := client.Releases().(gol.ExternalAssets); ok {
	a, err := ext.AddExternalAsset(ctx, "acme", "widget", rel.ID, "tool-mirror.tar.gz", "https://mirror.example.com/tool.tar.gz")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(a.External, a.DownloadURL) // true https://mirror.example.com/tool.tar.gz
}
```

---

## 10. Pull requests and reviews

```go
type PullRequests interface {
	List(ctx, owner, repo string, opts ListPROptions) (Page[PullRequest], error)
	Get(ctx, owner, repo string, number int) (*PullRequest, error)
	Create(ctx, owner, repo string, opts CreatePROptions) (*PullRequest, error)
	Edit(ctx, owner, repo string, number int, opts EditPROptions) (*PullRequest, error)
	Merge(ctx, owner, repo string, number int, opts MergeOptions) error
	CancelAutoMerge(ctx, owner, repo string, number int) error
	IsMerged(ctx, owner, repo string, number int) (bool, error)

	RequestReviewers(ctx, owner, repo string, number int, users, teams []string) error
	RemoveReviewers(ctx, owner, repo string, number int, users, teams []string) error
	ListReviews(ctx, owner, repo string, number int, opts ListOptions) (Page[Review], error)
	CreateReview(ctx, owner, repo string, number int, opts CreateReviewOptions) (*Review, error)
}
```

### 10.1 The `PullRequest` type

| Field | Notes |
|---|---|
| `Number int` | Forgejo and GitHub share this number sequence with issues |
| `Title`, `Body` | `Title` is returned as stored, so on Forgejo it includes the draft prefix |
| `State PRState` | `open` or `closed`. Merged pull requests are `closed` with `Merged` true. |
| `Draft bool` | |
| `Author` | Login (Bitbucket: nickname) |
| `Head`, `Base PRBranch` | `{Ref, SHA, Repo}`. `Repo` is `"owner/name"` and differs from the base repository for forks. |
| `Mergeable *bool` | `nil` when unknown. GitHub computes it in the background, so it can be `nil` right after creation. Bitbucket: always `nil`. |
| `Merged`, `MergedBy`, `MergedAt`, `MergeCommitSHA` | |
| `HTMLURL`, `CreatedAt`, `UpdatedAt`, `ClosedAt` | |

### 10.2 Creating, listing and editing

```go
type CreatePROptions struct {
	Title               string // required
	Body                string
	Head                string // required: branch with the changes; "user:branch" for a fork
	Base                string // required: branch to merge into
	Draft               bool
	AllowMaintainerEdit *bool  // let base maintainers push to the head branch
}

type ListPROptions struct {
	ListOptions
	State PRState // zero value: open
}

type EditPROptions struct {
	Title, Body, Base   *string
	State               *PRState // PRStateClosed closes, PRStateOpen reopens
	Draft               *bool    // convert to or from a draft
	AllowMaintainerEdit *bool
}
```

```go
prs := client.PullRequests()

pr, err := prs.Create(ctx, "acme", "widget", gol.CreatePROptions{
	Title: "Add sign-in",
	Body:  "Closes #12",
	Head:  "feature/sign-in",
	Base:  "main",
	Draft: true,
})
fmt.Println(pr.Number, pr.State, pr.Draft, pr.Head.SHA, pr.HTMLURL)

pr, err = prs.Get(ctx, "acme", "widget", pr.Number)
open, err := prs.List(ctx, "acme", "widget", gol.ListPROptions{})                     // open
all, err := prs.List(ctx, "acme", "widget", gol.ListPROptions{State: gol.PRStateAll}) // everything

pr, err = prs.Edit(ctx, "acme", "widget", pr.Number, gol.EditPROptions{Draft: gol.Ptr(false)}) // ready for review
pr, err = prs.Edit(ctx, "acme", "widget", pr.Number, gol.EditPROptions{Title: gol.Ptr("Add sign-in page"), Base: gol.Ptr("develop")})
pr, err = prs.Edit(ctx, "acme", "widget", pr.Number, gol.EditPROptions{State: gol.Ptr(gol.PRStateClosed)})
pr, err = prs.Edit(ctx, "acme", "widget", pr.Number, gol.EditPROptions{State: gol.Ptr(gol.PRStateOpen)}) // reopen
```

Drafts:
- **Forgejo** has no separate draft flag. A draft is a pull request whose title starts with a work-in-progress prefix (`WIP:` or `[WIP]` by default). `Draft: true` adds `WIP: ` unless the title already has a prefix, and `Draft: false` removes it. If the instance uses other prefixes and the server does not report the pull request as a draft, you get the pull request together with an `ErrNotSupported` error (see [§3.3](#33-errors)).
- **GitHub** creates drafts through REST and toggles the draft status through GraphQL.
- **Bitbucket** supports drafts natively.

| | Forgejo | GitHub | Bitbucket |
|---|---|---|---|
| Fork head (`user:branch`) | ✅ | ✅ | ❌ |
| `AllowMaintainerEdit` | ✅ | ✅ | ❌ |
| Close | ✅ | ✅ | ✅ (declines it) |
| Reopen | ✅ | ✅ | ❌ declines are final |

### 10.3 Merging

```go
type MergeOptions struct {
	Method            MergeMethod // zero value: merge
	Title             string      // merge or squash commit title; empty: server default
	Message           string      // commit message body; empty: server default
	HeadSHA           string      // fail unless the head is still at this commit
	DeleteBranch      bool        // delete the head branch after merging
	WhenChecksSucceed bool        // schedule the merge (auto-merge)
}
```

| Method | What it does | Forgejo | GitHub | Bitbucket |
|---|---|---|---|---|
| `MergeMethodMerge` | Merge commit | ✅ | ✅ | ✅ |
| `MergeMethodSquash` | One squashed commit | ✅ | ✅ | ✅ |
| `MergeMethodRebase` | Replay the commits, no merge commit | ✅ | ✅ | ✅ |
| `MergeMethodRebaseMerge` | Rebase, then add a merge commit | ✅ | ❌ | ✅ |
| `MergeMethodFastForwardOnly` | Fast-forward, or fail | ✅ | ❌ | ✅ |
| `MergeMethodManuallyMerged` | Mark as merged when its commits are already in the base | ✅ | ❌ | ❌ |

```go
err := prs.Merge(ctx, "acme", "widget", pr.Number, gol.MergeOptions{
	Method:       gol.MergeMethodSquash,
	Title:        "Add sign-in (#42)",
	Message:      "Adds the sign-in page and its tests.",
	HeadSHA:      pr.Head.SHA, // refuse the merge if someone pushed since review
	DeleteBranch: true,
})
merged, err := prs.IsMerged(ctx, "acme", "widget", pr.Number)
```

- Drafts cannot be merged on any backend.
- `HeadSHA`: Forgejo and GitHub check it atomically. **Bitbucket** checks it just before merging, which leaves a short race.
- `DeleteBranch`: **GitHub** deletes the branch with a separate call after the merge, and refuses to delete a branch in a fork. If that deletion fails, the error says the merge itself succeeded.

Auto-merge, which merges once the required checks pass:

```go
err := prs.Merge(ctx, "acme", "widget", pr.Number, gol.MergeOptions{
	Method:            gol.MergeMethodSquash,
	WhenChecksSucceed: true,
})
err = prs.CancelAutoMerge(ctx, "acme", "widget", pr.Number)
```

- **GitHub** schedules it through GraphQL. The repository must allow auto-merge, and a required check must still be pending. It cannot be combined with `DeleteBranch`; use the repository's *delete branch on merge* setting instead.
- **Bitbucket** has no auto-merge, so both calls return `ErrNotSupported`.

### 10.4 Reviews

```go
type CreateReviewOptions struct {
	Event    ReviewEvent // required: approve, request_changes, comment
	Body     string      // required for request_changes and comment
	CommitID string      // empty: the current head
}

type Review struct {
	ID          int64
	Reviewer    string
	State       ReviewState
	Body        string
	CommitID    string
	HTMLURL     string
	SubmittedAt time.Time
}
```

```go
// Request or withdraw review requests from users and teams.
err := prs.RequestReviewers(ctx, "acme", "widget", pr.Number, []string{"bob"}, []string{"backend-team"})
err = prs.RemoveReviewers(ctx, "acme", "widget", pr.Number, []string{"bob"}, nil)

review, err := prs.CreateReview(ctx, "acme", "widget", pr.Number, gol.CreateReviewOptions{
	Event: gol.ReviewEventApprove,
	Body:  "Looks good",
})
_, err = prs.CreateReview(ctx, "acme", "widget", pr.Number, gol.CreateReviewOptions{
	Event: gol.ReviewEventRequestChanges,
	Body:  "Please add a test for the error path.",
})

reviews, err := prs.ListReviews(ctx, "acme", "widget", pr.Number, gol.ListOptions{})
for _, r := range reviews.Items {
	fmt.Println(r.Reviewer, r.State)
}
```

| | Forgejo | GitHub | Bitbucket |
|---|---|---|---|
| Team reviewers | ✅ | ✅ | ❌ |
| Review a specific `CommitID` | ✅ | ✅ | ❌ |
| `ListReviews` | full reviews | full reviews | ⚠️ verdicts only (approved or changes requested), with no IDs |
| Approving one's own pull request | refused by the server | refused by the server | allowed |

On Forgejo, `ListReviews` also reports pending review requests (`ReviewRequested`).

---

## 11. Issues and comments

```go
type Issues interface {
	List(ctx, owner, repo string, opts ListIssuesOptions) (Page[Issue], error)
	Get(ctx, owner, repo string, number int) (*Issue, error)
	Create(ctx, owner, repo string, opts CreateIssueOptions) (*Issue, error)
	Edit(ctx, owner, repo string, number int, opts EditIssueOptions) (*Issue, error)
	Delete(ctx, owner, repo string, number int) error

	AddLabels(ctx, owner, repo string, number int, labels []string) error
	RemoveLabel(ctx, owner, repo string, number int, label string) error

	ListComments(ctx, owner, repo string, number int, opts ListOptions) (Page[Comment], error)
	CreateComment(ctx, owner, repo string, number int, body string) (*Comment, error)
	EditComment(ctx, owner, repo string, id int64, body string) (*Comment, error)
	DeleteComment(ctx, owner, repo string, id int64) error
}
```

**Bitbucket** retired its issue tracker, so issues, labels and milestones all return `ErrNotSupported`.

Issues and pull requests share one number sequence. `List` never includes pull requests, but the label, assignee, milestone and comment methods accept pull request numbers too.

### 11.1 Types

| `Issue` field | Notes |
|---|---|
| `Number`, `Title`, `Body`, `State` | |
| `Author` | Login |
| `Labels []string` | Names |
| `Assignees []string` | Logins |
| `Milestone *Milestone` | `nil` when there is none |
| `Comments int` | Number of comments |
| `Locked` | |
| `HTMLURL`, `CreatedAt`, `UpdatedAt`, `ClosedAt` | |

```go
type ListIssuesOptions struct {
	ListOptions
	State  IssueState // zero value: open
	Labels []string   // only issues carrying all these labels
}

type CreateIssueOptions struct {
	Title     string   // required
	Body      string
	Labels    []string // names; they must exist
	Assignees []string
	Milestone int64    // Milestone.ID; 0 for none
}

type EditIssueOptions struct {
	Title, Body *string
	State       *IssueState // close or reopen
	Labels      *[]string   // replaces; &[]string{} clears
	Assignees   *[]string   // replaces; &[]string{} clears
	Milestone   *int64      // 0 removes it
}
```

### 11.2 Examples

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
	State:  gol.IssueStateAll,
	Labels: []string{"bug", "security"},
})

issue, err = issues.Edit(ctx, "acme", "widget", issue.Number, gol.EditIssueOptions{
	Labels:    &[]string{"bug", "needs-triage"},
	Assignees: &[]string{}, // unassign everyone
	Milestone: gol.Ptr(int64(0)),
})

// Add and remove single labels without replacing the others.
err = issues.AddLabels(ctx, "acme", "widget", issue.Number, []string{"security"})
err = issues.RemoveLabel(ctx, "acme", "widget", issue.Number, "needs-triage")

// Close, reopen.
issue, err = issues.Edit(ctx, "acme", "widget", issue.Number, gol.EditIssueOptions{State: gol.Ptr(gol.IssueStateClosed)})
issue, err = issues.Edit(ctx, "acme", "widget", issue.Number, gol.EditIssueOptions{State: gol.Ptr(gol.IssueStateOpen)})

// Delete permanently (repository admins only; GitHub uses GraphQL).
err = issues.Delete(ctx, "acme", "widget", issue.Number)
```

### 11.3 Comments

```go
type Comment struct {
	ID        int64
	Author    string
	Body      string
	HTMLURL   string
	CreatedAt time.Time
	UpdatedAt time.Time
}
```

```go
c, err := issues.CreateComment(ctx, "acme", "widget", issue.Number, "Reproduced on main.")
c, err = issues.EditComment(ctx, "acme", "widget", c.ID, "Reproduced on main and v1.0.")
comments, err := issues.ListComments(ctx, "acme", "widget", issue.Number, gol.ListOptions{})
err = issues.DeleteComment(ctx, "acme", "widget", c.ID)

// Comment on a pull request's conversation: same methods, PR number.
_, err = issues.CreateComment(ctx, "acme", "widget", pr.Number, "CI is green, merging.")
```

**Forgejo:** `ListComments` returns every comment in one page and ignores `ListOptions`, because Forgejo does not paginate comments.

---

## 12. Labels

```go
type Labels interface {
	List(ctx, owner, repo string, opts ListOptions) (Page[Label], error)
	Get(ctx, owner, repo, name string) (*Label, error)
	Create(ctx, owner, repo string, label Label) (*Label, error)
	Edit(ctx, owner, repo, name string, opts EditLabelOptions) (*Label, error)
	Delete(ctx, owner, repo, name string) error
}

type Label struct {
	ID          int64
	Name        string
	Color       string // six hex digits, lower case, without "#"
	Description string
}

type EditLabelOptions struct {
	Name        *string // renames the label
	Color       *string
	Description *string
}
```

Labels are addressed by **name**.

```go
labels := client.Labels()

_, err := labels.Create(ctx, "acme", "widget", gol.Label{Name: "bug", Color: "d73a4a", Description: "Something is broken"})
l, err := labels.Get(ctx, "acme", "widget", "bug")
_, err = labels.Edit(ctx, "acme", "widget", "bug", gol.EditLabelOptions{Name: gol.Ptr("defect"), Color: gol.Ptr("b60205")})
page, err := labels.List(ctx, "acme", "widget", gol.ListOptions{})
err = labels.Delete(ctx, "acme", "widget", "defect")
```

---

## 13. Milestones

```go
type Milestones interface {
	List(ctx, owner, repo string, state MilestoneState, opts ListOptions) (Page[Milestone], error)
	Get(ctx, owner, repo string, id int64) (*Milestone, error)
	Create(ctx, owner, repo string, opts CreateMilestoneOptions) (*Milestone, error)
	Edit(ctx, owner, repo string, id int64, opts EditMilestoneOptions) (*Milestone, error)
	Delete(ctx, owner, repo string, id int64) error
}

type Milestone struct {
	ID           int64 // Forgejo: the milestone ID; GitHub: the milestone number
	Title        string
	Description  string
	State        MilestoneState
	DueOn        time.Time // zero if none
	OpenIssues   int
	ClosedIssues int
	ClosedAt     time.Time
}

type CreateMilestoneOptions struct {
	Title       string // required
	Description string
	DueOn       time.Time // zero: none
}

type EditMilestoneOptions struct {
	Title, Description *string
	State              *MilestoneState // close or reopen
	DueOn              *time.Time
}
```

Milestones are addressed by `Milestone.ID`. Use the value the library returns; do not build IDs yourself.

```go
m := client.Milestones()

ms, err := m.Create(ctx, "acme", "widget", gol.CreateMilestoneOptions{
	Title:       "v1.1",
	Description: "Next minor release",
	DueOn:       time.Now().AddDate(0, 1, 0),
})
open, err := m.List(ctx, "acme", "widget", gol.MilestoneStateOpen, gol.ListOptions{})
ms, err = m.Get(ctx, "acme", "widget", ms.ID)
ms, err = m.Edit(ctx, "acme", "widget", ms.ID, gol.EditMilestoneOptions{State: gol.Ptr(gol.MilestoneStateClosed)})
err = m.Delete(ctx, "acme", "widget", ms.ID)
```

---

## 14. Managing webhooks

```go
type Webhooks interface {
	List(ctx, scope HookScope, opts ListOptions) (Page[Webhook], error)
	Get(ctx, scope HookScope, id int64) (*Webhook, error)
	Create(ctx, scope HookScope, opts CreateWebhookOptions) (*Webhook, error)
	Edit(ctx, scope HookScope, id int64, opts EditWebhookOptions) (*Webhook, error)
	Delete(ctx, scope HookScope, id int64) error
	Ping(ctx, scope HookScope, id int64) error
}
```

### 14.1 Scope

A webhook belongs either to a repository or to an organisation (a Bitbucket workspace):

```go
gol.RepoHooks("acme", "widget") // HookScope{Owner: "acme", Repo: "widget"}
gol.OrgHooks("acme")            // HookScope{Owner: "acme"}
```

### 14.2 Types

```go
type Webhook struct {
	ID           int64 // Bitbucket: derived from the webhook's UUID (stable)
	URL          string
	ContentType  HookContentType
	Events       []HookEvent
	Active       bool
	BranchFilter string // Forgejo only
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type CreateWebhookOptions struct {
	URL                 string          // required
	ContentType         HookContentType // zero value: json
	Secret              string          // signs deliveries; strongly recommended
	Events              []HookEvent     // required
	Active              *bool           // nil: active
	BranchFilter        string          // Forgejo only, e.g. "{main,release/*}"
	AuthorizationHeader string          // Forgejo only, sent with every delivery
}

type EditWebhookOptions struct {
	URL                 *string
	ContentType         *HookContentType
	Secret              *string
	Events              []HookEvent // nil: unchanged; non-empty: replaces the list (empty non-nil is refused)
	Active              *bool
	BranchFilter        *string // Forgejo only
	AuthorizationHeader *string // Forgejo only
}
```

The secret is never returned by the server. Hook URLs can embed credentials, so avoid logging `Webhook.URL`.

### 14.3 Events

The constants use GitHub's names. The library translates them for the other forges:

| Constant | Forgejo | GitHub | Bitbucket keys |
|---|---|---|---|
| `HookEventPush` | `push` | `push` | `repo:push` |
| `HookEventCreate` (branch or tag created) | `create` | `create` | ❌ (covered by `repo:push`) |
| `HookEventDelete` (branch or tag deleted) | `delete` | `delete` | ❌ (covered by `repo:push`) |
| `HookEventFork` | `fork` | `fork` | `repo:fork` |
| `HookEventRelease` | `release` | `release` | ❌ |
| `HookEventIssues` | `issues` | `issues` | ❌ |
| `HookEventIssueComment` | `issue_comment` | `issue_comment` | ❌ |
| `HookEventPullRequest` | `pull_request` | `pull_request` | `pullrequest:created`, `:updated`, `:fulfilled`, `:rejected` |
| `HookEventPullRequestReview` | `pull_request_review_approved`, `_rejected`, `_comment` | `pull_request_review` | `pullrequest:approved`, `:unapproved`, `:changes_request_created`, `:changes_request_removed` |
| `HookEventRepository` | `repository` | `repository` | `repo:updated` |
| `HookEventWiki` | `wiki` | `gollum` | ❌ |

Any other value is passed to the server unchanged, so backend-specific events still work: for example Forgejo's `issue_assign`, GitHub's `workflow_run`, or any Bitbucket key containing `:`. When reading hooks back, groups of translated names are folded back into the constants. Names the library does not translate keep the forge's own spelling. For example, Forgejo may add finer-grained events implied by the ones you asked for, such as `issue_assign` with `issues`.

What happens with an unknown event name:
- **Forgejo** silently drops it. `Create` and `Edit` then return the webhook **and** an error naming the dropped events.
- **GitHub** rejects it (`*APIError`).
- **Bitbucket** returns `ErrNotSupported` for constants without an equivalent, and the server rejects unknown keys.

### 14.4 Examples

```go
hooks := client.Webhooks()
scope := gol.RepoHooks("acme", "widget")

hook, err := hooks.Create(ctx, scope, gol.CreateWebhookOptions{
	URL:    "https://ci.example.com/hooks/forge",
	Secret: os.Getenv("WEBHOOK_SECRET"),
	Events: []gol.HookEvent{gol.HookEventPush, gol.HookEventPullRequest, gol.HookEventRelease},
})

// Forgejo extras.
hook, err = hooks.Create(ctx, scope, gol.CreateWebhookOptions{
	URL:                 "https://ci.example.com/hooks/forge",
	Secret:              os.Getenv("WEBHOOK_SECRET"),
	Events:              []gol.HookEvent{gol.HookEventPush},
	ContentType:         gol.HookContentForm,
	BranchFilter:        "{main,release/*}",
	AuthorizationHeader: "Bearer " + os.Getenv("CI_TOKEN"),
})

err = hooks.Ping(ctx, scope, hook.ID)

hook, err = hooks.Edit(ctx, scope, hook.ID, gol.EditWebhookOptions{
	Events: []gol.HookEvent{gol.HookEventPush}, // replaces the list
	Active: gol.Ptr(false),
})
page, err := hooks.List(ctx, scope, gol.ListOptions{})
err = hooks.Delete(ctx, scope, hook.ID)

// Organisation-wide.
orgHook, err := hooks.Create(ctx, gol.OrgHooks("acme"), gol.CreateWebhookOptions{
	URL: "https://audit.example.com/forge", Secret: secret, Events: []gol.HookEvent{gol.HookEventRepository},
})
```

| | Forgejo | GitHub | Bitbucket |
|---|---|---|---|
| `Ping` | ⚠️ repositories only; sends a `push` of the latest commit | ✅ sends a `ping` event | ❌ |
| Form-encoded payloads | ✅ | ✅ | ❌ |
| `BranchFilter`, `AuthorizationHeader` | ✅ | ❌ | ❌ |
| Target URL | any | any | must resolve in DNS |

---

## 15. Receiving webhooks (`webhook` package)

The `webhook` package handles incoming deliveries from all three forges:
- It verifies the signature (HMAC-SHA256, compared in constant time).
- It identifies the forge and the event.
- It decodes the common payloads into the library's types.

It depends only on the root package.

### 15.1 The quick way: `Handler`

```go
func Handler(secret []byte, fn func(context.Context, *webhook.Event) error) http.Handler
```

```go
secret := []byte(os.Getenv("WEBHOOK_SECRET"))

http.Handle("/hooks/forge", webhook.Handler(secret, func(ctx context.Context, ev *webhook.Event) error {
	log.Printf("%s %s %s from %s on %s (delivery %s)", ev.Forge, ev.Name, ev.Action, ev.Sender, ev.Repo, ev.Delivery)

	switch ev.Name {
	case "push":
		push, err := ev.Push()
		if err != nil {
			return err
		}
		switch {
		case push.Branch() == "main":
			log.Printf("main moved %s → %s (%d commits) by %s", push.Before, push.After, len(push.Commits), push.Pusher)
		case push.Tag() != "":
			log.Printf("tag %s pushed", push.Tag())
		}
	case "pull_request":
		pr, err := ev.PullRequest()
		if err != nil {
			return err
		}
		log.Printf("PR #%d %s: %s (%s → %s)", pr.Number, pr.Action, pr.PullRequest.Title, pr.PullRequest.Head.Ref, pr.PullRequest.Base.Ref)
	case "release":
		rel, err := ev.Release()
		if err != nil {
			return err
		}
		if rel.Action == "published" {
			log.Printf("released %s", rel.Release.TagName)
		}
	case "ping":
		// GitHub's test delivery.
	default:
		// Anything else: decode ev.Payload yourself.
	}
	return nil
}))
log.Fatal(http.ListenAndServe(":8080", nil))
```

`Handler` responds with:

| Status | When |
|---|---|
| 405 | The request is not a `POST` |
| 401 | The signature is missing or invalid |
| 400 | The body is unreadable or too large, the forge is unknown, or the payload is malformed |
| 500 | `fn` returned an error (the error text is not sent to the forge) |
| 204 | Success |

Forges retry or flag failed deliveries, so return an error from `fn` only when a retry makes sense. For long-running work, queue the event and return quickly, because forges time out slow receivers.

### 15.2 Lower-level functions

```go
// Read (max webhook.MaxBodySize = 25 MB), verify and decode. Consumes r.Body.
func Parse(r *http.Request, secret []byte) (*webhook.Event, error)

// Verify a signature over a body you already hold.
func Verify(body []byte, h http.Header, secret []byte) error

// Identify and decode, WITHOUT verifying (call Verify first).
func Decode(h http.Header, body []byte) (*webhook.Event, error)
```

```go
func serve(w http.ResponseWriter, r *http.Request) {
	ev, err := webhook.Parse(r, secret)
	switch {
	case errors.Is(err, webhook.ErrMissingSignature), errors.Is(err, webhook.ErrInvalidSignature):
		http.Error(w, "bad signature", http.StatusUnauthorized)
		return
	case errors.Is(err, webhook.ErrUnknownForge):
		http.Error(w, "unknown sender", http.StatusBadRequest)
		return
	case err != nil:
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	enqueue(ev)
	w.WriteHeader(http.StatusAccepted)
}
```

Errors:

| Error | Meaning |
|---|---|
| `webhook.ErrMissingSignature` | The delivery is not signed (no secret configured on the hook?) |
| `webhook.ErrInvalidSignature` | The signature does not match, or is malformed |
| `webhook.ErrUnknownForge` | No Forgejo, GitHub or Bitbucket event header was found |

`Verify` also refuses an empty secret.

Signature headers checked, in order:

| Header | Format | Sent by |
|---|---|---|
| `X-Hub-Signature-256` | `sha256=<hex>` | GitHub, Forgejo |
| `X-Hub-Signature` | `sha256=<hex>` | Bitbucket. GitHub's legacy SHA-1 form is ignored. |
| `X-Forgejo-Signature` | `<hex>` | Forgejo |

Forge detection: `X-Forgejo-Event` means Forgejo (checked first, because Forgejo also sends `X-GitHub-Event`). `X-GitHub-Event` without `X-Gitea-Event` means GitHub, and `X-Event-Key` means Bitbucket. Form-encoded deliveries (`payload=<json>`) are unwrapped automatically.

### 15.3 `Event`

| Field | Notes |
|---|---|
| `Forge gol.Backend` | |
| `Name string` | `push`, `pull_request`, `pull_request_review`, `release`, `issues`, `ping`, … Bitbucket keys are mapped to these names where they match; others keep their key. |
| `Key string` | Bitbucket's raw `X-Event-Key` (e.g. `pullrequest:fulfilled`); empty for other forges |
| `Delivery string` | Unique delivery ID, useful for deduplication |
| `Action string` | `opened`, `closed`, `published`, …; empty for push |
| `Repo string` | `"owner/name"`; empty for events without a repository |
| `Sender string` | Login of the user who triggered the event |
| `Payload json.RawMessage` | The JSON payload, for fields the typed accessors omit |

### 15.4 Typed payloads

| Accessor | For `Name` | Returns |
|---|---|---|
| `ev.Push()` | `push` | `*PushEvent` |
| `ev.PullRequest()` | `pull_request` (on Bitbucket: any `pullrequest:…` key, reviews included) | `*PullRequestEvent` |
| `ev.Release()` | `release` (not Bitbucket) | `*ReleaseEvent` |

Calling an accessor for the wrong event returns an error.

```go
type PushEvent struct {
	Ref        string // "refs/heads/main" or "refs/tags/v1"
	Before     string
	After      string
	CompareURL string
	Pusher     string
	Commits    []Commit
	HeadCommit *Commit // nil when the push deletes the ref
}
func (p *PushEvent) Branch() string // "" unless the push is to a branch
func (p *PushEvent) Tag() string    // "" unless the push is to a tag

type Commit struct {
	ID, Message, URL string
	Author           CommitAuthor // {Name, Email, Username}; Username is empty if no account matches
	Timestamp        time.Time
	Added, Removed, Modified []string
}

type PullRequestEvent struct {
	Action      string // "opened", "closed", "reopened", "edited", …
	Number      int
	PullRequest gol.PullRequest
}

type ReleaseEvent struct {
	Action  string // "published", "updated", "deleted", …
	Release gol.Release // assets are not included
}
```

For any other event, decode `ev.Payload` with `encoding/json`:

```go
case "issues":
	var p struct {
		Issue struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
		} `json:"issue"`
	}
	if err := json.Unmarshal(ev.Payload, &p); err != nil {
		return err
	}
```

### 15.5 Forge specifics

- **Test deliveries.** Forgejo's (`Webhooks().Ping`) is a `push` event; GitHub's is a `ping` event.
- **Bitbucket keys** are mapped as follows:

| Bitbucket key | `Name` | `Action` |
|---|---|---|
| `repo:push` | `push` | |
| `pullrequest:created` | `pull_request` | `opened` |
| `pullrequest:updated` | `pull_request` | `edited` |
| `pullrequest:fulfilled` (merged) | `pull_request` | `closed` |
| `pullrequest:rejected` (declined) | `pull_request` | `closed` |
| `pullrequest:approved` | `pull_request_review` | `approved` |
| `pullrequest:unapproved` | `pull_request_review` | `dismissed` |
| `pullrequest:changes_request_created` | `pull_request_review` | `changes_requested` |
| `pullrequest:changes_request_removed` | `pull_request_review` | `dismissed` |
| anything else | the key itself | |

  A Bitbucket push can update several refs in one delivery. `Push()` decodes the first change; the others are in `Payload`. Bitbucket support follows Bitbucket's documentation and has been tested with synthetic fixtures only, not yet against live deliveries.

## 16. Users, organisations and teams (Directory)

`Directory` reads accounts **with whatever rights the token has**. A site admin sees everything. Any other user sees what they could see in the web interface: public profiles, the organisations and teams they belong to, public members of other organisations. Forgejo and GitHub implement it (`CapDirectory`). **Bitbucket** implements `Me` only and returns `ErrNotSupported` for the rest.

```go
dir := client.Directory()
```

For managing accounts (creating, editing, deleting) on Forgejo, see [Administration](#17-administration-forgejo). On Forgejo, `Directory` and `Admin` return the same types and the same data.

```go
type Directory interface {
	Me(ctx) (*CurrentUser, error) // the authenticated user; Bitbucket too

	User(ctx, login string) (*User, error)
	UserInfo(ctx, login string) (*UserInfo, error)

	Org(ctx, name string) (*Org, error)
	ListOrgs(ctx, user string, opts ListOptions) (Page[Org], error) // user "": the authenticated user
	ListAllOrgs(ctx, opts ListOptions) (Page[Org], error)
	OrgInfo(ctx, name string) (*OrgInfo, error)
	OrgMembers(ctx, org string, opts ListOptions) (Page[User], error)

	ListTeams(ctx, org string, opts ListOptions) (Page[Team], error)
	Team(ctx, org, name string) (*Team, error)
	TeamInfo(ctx, org, name string) (*TeamInfo, error)
	TeamMembers(ctx, org, name string, opts ListOptions) (Page[User], error)
	TeamRepos(ctx, org, name string, opts ListOptions) (Page[Repo], error)
}
```

**Teams are addressed by organisation and name.** This is the only addressing Forgejo and GitHub share: GitHub has no team-ID-only endpoints.
- On Forgejo, `name` is the team name, matched case-insensitively.
- On GitHub, `name` is the team **slug** (`"back-end"` for "Back End"). `Team.Slug` always holds the value to pass back.
- On Forgejo, `TeamMembers` and `TeamRepos` look the team up first, which costs one extra request per call.

### 16.1 Account types

`User`:

| Field | Forgejo | GitHub | Notes |
|---|---|---|---|
| `ID`, `Login`, `FullName`, `Description`, `Website`, `Location`, `HTMLURL`, `AvatarURL`, `CreatedAt` | ✅ | ✅ | GitHub: `Description` is the bio and `Website` the blog |
| `Email` | ✅ | ✅ | Only when the server shows it to the caller |
| `Visibility` | ✅ | always `public` | |
| `Followers`, `Following` | ✅ | ✅ | |
| `IsAdmin` | ✅ | ✅ site admin | |
| `Active` | ✅ | ✅ (false when suspended) | |
| `Restricted`, `ProhibitLogin`, `LastLogin` | ✅ (admins see them) | — | `LastLogin` is zero if the user never signed in |
| `Pronouns`, `Language`, `StarredRepos` | ✅ | — | `Language` is the interface language |
| `Company` | — | ✅ | |

`Org`:

| Field | Forgejo | GitHub |
|---|---|---|
| `ID`, `Name`, `FullName`, `Description`, `Website`, `Location`, `Email`, `AvatarURL` | ✅ | ✅ |
| `Visibility` | ✅ | always `public` |
| `HTMLURL` | — | ✅ |
| `RepoAdminChangeTeamAccess` (whether repository admins may change team access) | ✅ | — |

`Team`:

| Field | Forgejo | GitHub |
|---|---|---|
| `ID`, `Org`, `Name`, `Description` | ✅ | ✅ |
| `Slug` | = `Name` | ✅ |
| `Permission` | overall level (`read`, `write`, `admin`, `owner`) | the team's default repository permission: `pull`/`triage` → `read`, `push`/`maintain` → `write`, `admin` → `admin` |
| `Units` | ✅ per-unit access | — |
| `IncludesAllRepos`, `CanCreateOrgRepo` | ✅ | — |

### 16.2 Single objects and lists

```go
u, err := dir.User(ctx, "alice")
fmt.Println(u.FullName, u.Followers, u.AvatarURL)

org, err := dir.Org(ctx, "acme")

mine, err := dir.ListOrgs(ctx, "", gol.ListOptions{})     // the authenticated user's organisations, private memberships included
bobs, err := dir.ListOrgs(ctx, "bob", gol.ListOptions{})  // bob's organisations, as far as the caller can see
all, err := dir.ListAllOrgs(ctx, gol.ListOptions{})       // every organisation visible to the caller

members, err := dir.OrgMembers(ctx, "acme", gol.ListOptions{}) // non-members: public members only

teams, err := dir.ListTeams(ctx, "acme", gol.ListOptions{})
team, err := dir.Team(ctx, "acme", "back-end")
tm, err := dir.TeamMembers(ctx, "acme", team.Slug, gol.ListOptions{})
tr, err := dir.TeamRepos(ctx, "acme", team.Slug, gol.ListOptions{})
```

Visibility rules applied by the servers:
- **Teams.** Forgejo lists an organisation's teams only to its members. GitHub hides secret teams from non-members, and fine-grained tokens without organisation access get HTTP 403.
- **Members.** Callers outside an organisation only see its public members.
- **Private organisations** (Forgejo) are invisible to non-members and return 404.

**GitHub, `ListAllOrgs`.** This listing uses GitHub's `since` cursor (an organisation ID) instead of page numbers, and the cursor travels in `Page` and `NextPage`. Pass `NextPage` back unchanged, or use `gol.All`. On github.com this lists every organisation on the site, so use it mostly on GitHub Enterprise Server.

### 16.3 Info calls

`UserInfo`, `OrgInfo` and `TeamInfo` combine several reads into one result:

```go
type UserInfo struct {
	User
	Orgs       []Org  // organisations the user belongs to
	Repos      []Repo // repositories the user owns
	Incomplete error
	Missing    []PartError // the same refusals, as data (and in JSON)
}

type OrgInfo struct {
	Org
	Teams      []Team
	Members    []User
	Repos      []Repo
	Incomplete error
	Missing    []PartError // the same refusals, as data (and in JSON)
}

type TeamInfo struct {
	Team
	Members    []User
	Repos      []Repo
	Incomplete error
	Missing    []PartError // the same refusals, as data (and in JSON)
}
```

The object itself is embedded, so `info.Name`, `info.Description` and so on work directly. The lists are complete: the library walks every page for you, 50 items per request.

**`Incomplete`.** A token sees only part of what an admin sees, so an info call does not fail when one of its lists is off limits:
- If the server refuses a list (HTTP 401, 403 or 404), or the backend cannot read it (`ErrNotSupported`), that list is left empty. The refusal is recorded in `Incomplete`, prefixed with the part's name.
- If every part was read, `Incomplete` is nil.
- Any other failure fails the whole call: a 5xx, a network error, a cancelled context, or the main object itself being missing (a 404 on the user, organisation or team).

`Incomplete` keeps the original errors, so `gol.IsForbidden(info.Incomplete)` and `errors.Is(info.Incomplete, gol.ErrNotSupported)` work.

**`Missing`.** `Incomplete` is an `error`, so it is not serialised. `Missing` holds the same refusals as data, one `PartError` per part, and is omitted from JSON when everything was read:

```go
type PartError struct {
	Part    string // "teams", "e-mail addresses", …
	Status  int    // HTTP status of the refusal; 0 when the backend cannot read the part (ErrNotSupported)
	Message string
}
```

```go
info, err := client.Directory().OrgInfo(ctx, "acme")
if err != nil {
	log.Fatal(err) // no such organisation, server error, …
}
fmt.Printf("%s (%s): %d teams, %d members, %d repositories\n",
	info.Name, info.FullName, len(info.Teams), len(info.Members), len(info.Repos))
for _, t := range info.Teams {
	fmt.Printf("  team %s (%s)\n", t.Name, t.Permission)
}
if info.Incomplete != nil {
	// e.g. "teams: forgejo: HTTP 403: Must be an organization member"
	log.Printf("partial view: %v", info.Incomplete)
}

ui, err := client.Directory().UserInfo(ctx, "alice")
fmt.Println(ui.Login, len(ui.Orgs), len(ui.Repos), ui.Incomplete)

ti, err := client.Directory().TeamInfo(ctx, "acme", "back-end")
for _, m := range ti.Members {
	fmt.Println(m.Login)
}
```

The info calls cost one request for the object, plus one per page of each list. For a large organisation (thousands of repositories), consider calling the list methods yourself, page by page.

| | Forgejo | GitHub |
|---|---|---|
| `UserInfo.Orgs` | `/users/{login}/orgs` | `/users/{login}/orgs` (public memberships only, even for yourself) |
| `UserInfo.Repos` | repositories the user owns that the caller can see | the same (for the authenticated user, their own repositories, private included) |
| `OrgInfo.Teams` | members only | visible teams; 403 for tokens without organisation access |
| `OrgInfo.Members` | all members for members; public members otherwise | the same |

### 16.4 The authenticated user (`Me`)

`Me` answers "who am I?" from the credentials themselves, not from a stored username. It reads `GET /user`, which shows fields that `User(login)` hides even for yourself (e-mail, admin flag, last login, GitHub's plan and private counts), then gathers everything else the token may read about the account. Nothing is filtered: keep what you need.

```go
type CurrentUser struct {
	User                      // profile, private fields included
	LoginName    string       // Forgejo: name on the external auth source
	AuthSourceID int64        // Forgejo: 0 for a local account
	UUID, AccountID string    // Bitbucket
	TwoFactorAuth *bool       // GitHub, Bitbucket; nil if not reported
	Type, Plan, TwitterUsername string                    // GitHub
	Hireable bool                                         // GitHub
	PublicRepos, PublicGists, PrivateGists int            // GitHub
	TotalPrivateRepos, OwnedPrivateRepos int64            // GitHub
	DiskUsage, Collaborators int                          // GitHub (DiskUsage in KiB)
	UpdatedAt time.Time                                   // GitHub
	Settings *UserSettings    // Forgejo: theme, diff view, hide e-mail/activity/pronouns

	Emails  []Email           // address, primary, verified (GitHub: visibility)
	Orgs    []Org             // private memberships included
	Teams   []Team
	Repos   []Repo            // owned, collaborator and organisation repositories, private included
	SSHKeys []SSHKey
	GPGKeys []GPGKey          // with e-mail addresses and subkeys
	Incomplete error
	Missing    []PartError // the same refusals, as data (and in JSON)
}
```

```go
me, err := client.Directory().Me(ctx)
if err != nil {
	log.Fatal(err) // bad credentials, server error, …
}
fmt.Println(me.Login, me.FullName, me.Email, me.IsAdmin)
for _, e := range me.Emails {
	fmt.Println(e.Address, e.Primary, e.Verified)
}
if me.Incomplete != nil {
	log.Printf("partial view: %v", me.Incomplete) // e.g. "e-mail addresses: github: HTTP 404: Not Found"
}
```

`Incomplete` works as for the info calls (§16.3): a failure on `/user` itself fails the call, a refused part is left empty and recorded. Missing token scopes are the usual cause:

| Part | Forgejo scope | GitHub classic scope | Bitbucket scope |
|---|---|---|---|
| profile (`/user`), settings, e-mail addresses, SSH and GPG keys | `read:user` | e-mail: `user:email`; keys: `read:public_key`, `read:gpg_key` | `account`, e-mail: `email` |
| organisations, teams | `read:organization` | `read:org` (teams) | workspaces: `account` |
| repositories | `read:repository` | `repo` for private ones | `repository` |

Per backend:

| Part | Forgejo | GitHub | Bitbucket |
|---|---|---|---|
| `Settings` | `/user/settings` | — | — |
| `Emails` | `/user/emails` | `/user/emails` | `/user/emails`; the primary one also fills `Email` |
| `Orgs` | `/user/orgs` | `/user/orgs` | workspaces (`/user/workspaces`): `Name` = slug, `FullName` = name |
| `Teams` | `/user/teams` | `/user/teams` | not supported (recorded in `Incomplete`) |
| `Repos` | `/user/repos` | `/user/repos` | every workspace's repositories |
| `SSHKeys` | `/user/keys` | `/user/keys` | `/users/{uuid}/ssh-keys`; `Title` is the label, else the key comment |
| `GPGKeys` | `/user/gpg_keys` | `/user/gpg_keys` | not supported (recorded in `Incomplete`) |

On Bitbucket, `Me` needs credentials that belong to a user (API token or app password). Workspace, project and repository access tokens have no user behind them and get an error from `/user`.

`Me` costs about eight requests plus one per extra page of each list.

---

## 17. Administration (Forgejo)

`gol.Admin` manages accounts: it creates, edits and deletes users, organisations and teams. Only the Forgejo backend implements it. To only *read* accounts, with any token and on GitHub too, use [`Directory`](#16-users-organisations-and-teams-directory).

```go
type Admin interface {
	Users() Users
	Orgs() Orgs
	Teams() Teams
}
```

```go
admin, ok := client.(gol.Admin)
if !ok {
	log.Fatalf("%s has no administration API", client.Backend())
}
```

Required rights:
- **User management** (everything except `Users().Get`) needs a **site-admin** token.
- **Organisations and teams** need owner rights on the organisation. Creating an organisation for another user needs a site-admin token.

### 17.1 Users

```go
type Users interface {
	Get(ctx, login string) (*User, error)
	List(ctx, opts ListOptions) (Page[User], error)
	Create(ctx, opts CreateUserOptions) (*User, error)
	Edit(ctx, login string, opts EditUserOptions) (*User, error)
	Rename(ctx, login, newLogin string) error
	Delete(ctx, login string, opts DeleteUserOptions) error
}
```

`User` fields are listed in [§16.1](#161-account-types). `Users().List` uses the admin endpoint; the other read calls work with any token.

```go
type CreateUserOptions struct {
	Login, Email       string // required
	Password           string
	FullName           string
	MustChangePassword *bool  // nil: Forgejo's default (true)
	Visibility         OwnerVisibility
	Restricted         bool
	SendNotify         bool   // e-mail the user about the account
}

type EditUserOptions struct {
	Email, FullName, Description, Website, Location, Password *string
	MustChangePassword, Admin, Active, ProhibitLogin, Restricted, AllowCreateOrganization *bool
	Visibility      *OwnerVisibility
	MaxRepoCreation *int // -1: the instance default
}

type DeleteUserOptions struct {
	Purge bool // also delete the user's repositories, memberships and content
}
```

```go
users := admin.Users()

u, err := users.Create(ctx, gol.CreateUserOptions{
	Login:              "alice",
	Email:              "alice@example.com",
	Password:           "initial-password",
	FullName:           "Alice Example",
	MustChangePassword: gol.Ptr(true),
	Visibility:         gol.OwnerVisibilityLimited,
})

u, err = users.Edit(ctx, "alice", gol.EditUserOptions{
	Location:        gol.Ptr("Montréal"),
	Admin:           gol.Ptr(false),
	MaxRepoCreation: gol.Ptr(10),
	ProhibitLogin:   gol.Ptr(true), // suspend the account
})

err = users.Rename(ctx, "alice", "alice.example") // the old login keeps redirecting
page, err := users.List(ctx, gol.ListOptions{})

err = users.Delete(ctx, "alice.example", gol.DeleteUserOptions{Purge: true})
```

- Without `Purge`, deleting a user who still owns repositories fails.
- Forgejo 15 ignores a visibility change back to `public`. `Edit` then returns the updated user **and** an `ErrNotSupported` error.

### 17.2 Organisations

```go
type Orgs interface {
	Get(ctx, name string) (*Org, error)
	List(ctx, user string, opts ListOptions) (Page[Org], error) // user "": the authenticated user
	ListAll(ctx, opts ListOptions) (Page[Org], error)           // every visible org (all, for a site admin)
	Create(ctx, opts CreateOrgOptions) (*Org, error)
	Edit(ctx, name string, opts EditOrgOptions) (*Org, error)
	Rename(ctx, name, newName string) error
	Delete(ctx, name string) error

	Members(ctx, org string, opts ListOptions) (Page[User], error)
	IsMember(ctx, org, user string) (bool, error)
	RemoveMember(ctx, org, user string) error
}
```

`Org` fields are listed in [§16.1](#161-account-types).

```go
orgs := admin.Orgs()

org, err := orgs.Create(ctx, gol.CreateOrgOptions{
	Name:       "acme",
	FullName:   "Acme Inc.",
	Visibility: gol.OwnerVisibilityPrivate,
	// Owner: "alice", // create it for another user (site admin)
})
org, err = orgs.Edit(ctx, "acme", gol.EditOrgOptions{Description: gol.Ptr("Widgets since 1999")})

mine, err := orgs.List(ctx, "", gol.ListOptions{})
bobs, err := orgs.List(ctx, "bob", gol.ListOptions{})
all, err := orgs.ListAll(ctx, gol.ListOptions{})

members, err := orgs.Members(ctx, "acme", gol.ListOptions{})
isMember, err := orgs.IsMember(ctx, "acme", "bob") // a user that does not exist is reported as not a member
err = orgs.RemoveMember(ctx, "acme", "bob")        // also removes them from every team; removing the last owner fails

err = orgs.Rename(ctx, "acme", "acme-corp") // the old name keeps redirecting
err = orgs.Delete(ctx, "acme-corp")         // fails while it still owns repositories
```

There is no direct "add member": users join an organisation through one of its teams (`Teams().AddMember`).

### 17.3 Teams

```go
type Teams interface {
	List(ctx, org string, opts ListOptions) (Page[Team], error)
	Get(ctx, id int64) (*Team, error)
	GetByName(ctx, org, name string) (*Team, error) // case-insensitive; 404 *APIError if absent
	Create(ctx, org string, opts CreateTeamOptions) (*Team, error)
	Edit(ctx, id int64, opts EditTeamOptions) (*Team, error)
	Delete(ctx, id int64) error

	Members(ctx, id int64, opts ListOptions) (Page[User], error)
	IsMember(ctx, id int64, user string) (bool, error)
	AddMember(ctx, id int64, user string) error // also makes them an organisation member
	RemoveMember(ctx, id int64, user string) error

	Repos(ctx, id int64, opts ListOptions) (Page[Repo], error)
	AddRepo(ctx, id int64, owner, repo string) error
	RemoveRepo(ctx, id int64, owner, repo string) error
}
```

Teams are addressed by **ID**. Use `GetByName` to look one up.

`Team` fields (see also [§16.1](#161-account-types)):
- `ID`, `Org`, `Name`, `Slug` (the name, on Forgejo), `Description`
- `Permission`: the overall level
- `Units map[TeamUnit]TeamPermission`: per-unit access
- `IncludesAllRepos`: access to every repository of the organisation, current and future
- `CanCreateOrgRepo`

**Access.** `TeamAccess` sets a team's permissions in exactly one of three forms:

| Form | `Permission` | `Units` | Result |
|---|---|---|---|
| Admin | `TeamPermissionAdmin` | empty | Admin on every unit |
| Uniform | `TeamPermissionRead` or `TeamPermissionWrite` | empty | That level on every unit |
| Per unit | empty | `{unit: none/read/write}` | Per-unit permissions |

Any other combination is refused before a request is sent. `TeamPermissionOwner` is only ever reported, for the owner team, and cannot be assigned.

Units:
- `TeamUnitCode`, `TeamUnitIssues`, `TeamUnitPulls`, `TeamUnitReleases`, `TeamUnitWiki`
- `TeamUnitExternalIssues`, `TeamUnitExternalWiki`
- `TeamUnitProjects`, `TeamUnitPackages`, `TeamUnitActions`
- `gol.AllTeamUnits` lists them all.

```go
teams := admin.Teams()

devs, err := teams.Create(ctx, "acme", gol.CreateTeamOptions{
	Name:        "developers",
	Description: "Everyone who writes code",
	Access: gol.TeamAccess{Units: map[gol.TeamUnit]gol.TeamPermission{
		gol.TeamUnitCode:     gol.TeamPermissionWrite,
		gol.TeamUnitPulls:    gol.TeamPermissionWrite,
		gol.TeamUnitIssues:   gol.TeamPermissionWrite,
		gol.TeamUnitReleases: gol.TeamPermissionRead,
		gol.TeamUnitWiki:     gol.TeamPermissionRead,
	}},
	CanCreateOrgRepo: true,
})

readers, err := teams.Create(ctx, "acme", gol.CreateTeamOptions{
	Name: "readers", Access: gol.TeamAccess{Permission: gol.TeamPermissionRead}, IncludesAllRepos: true,
})

ops, err := teams.Create(ctx, "acme", gol.CreateTeamOptions{
	Name: "ops", Access: gol.TeamAccess{Permission: gol.TeamPermissionAdmin}, IncludesAllRepos: true,
})

err = teams.AddMember(ctx, devs.ID, "bob")
err = teams.AddRepo(ctx, devs.ID, "acme", "widget")
members, err := teams.Members(ctx, devs.ID, gol.ListOptions{})
repos, err := teams.Repos(ctx, devs.ID, gol.ListOptions{})
inTeam, err := teams.IsMember(ctx, devs.ID, "bob")

devs, err = teams.Edit(ctx, devs.ID, gol.EditTeamOptions{
	Name:   gol.Ptr("engineering"),
	Access: &gol.TeamAccess{Permission: gol.TeamPermissionWrite},
})
err = teams.RemoveRepo(ctx, devs.ID, "acme", "widget")
err = teams.RemoveMember(ctx, devs.ID, "bob")
err = teams.Delete(ctx, readers.ID)
```

**The owner team is protected.** Forgejo itself lets you delete an organisation's owner team, which leaves the organisation without owners. The library refuses to delete the owner team, rename it or change its access, and returns `gol.ErrOwnerTeam` instead:

```go
owners, err := teams.GetByName(ctx, "acme", "Owners")
err = teams.Delete(ctx, owners.ID)
fmt.Println(errors.Is(err, gol.ErrOwnerTeam)) // true

// Changing its description, or its members, is allowed.
_, err = teams.Edit(ctx, owners.ID, gol.EditTeamOptions{Description: gol.Ptr("Organisation owners")})
err = teams.AddMember(ctx, owners.ID, "carol")
```

---

## 18. Recipes

### 18.1 Release pipeline (any backend with releases)

```go
func publish(ctx context.Context, c gol.Client, owner, repo, tag string, files []string) error {
	if !c.Capabilities().Has(gol.CapReleases) {
		return fmt.Errorf("%s has no releases", c.Backend())
	}
	rels := c.Releases()

	rel, err := rels.Create(ctx, owner, repo, gol.CreateReleaseOptions{
		TagName: tag, TargetCommitish: "main", Name: tag, Draft: true,
	})
	if err != nil {
		return err
	}
	for _, path := range files {
		if err := upload(ctx, rels, owner, repo, rel.ID, path); err != nil {
			_ = rels.Delete(ctx, owner, repo, rel.ID) // drafts have no tag yet
			return err
		}
	}
	_, err = rels.Edit(ctx, owner, repo, rel.ID, gol.EditReleaseOptions{Draft: gol.Ptr(false)})
	return err
}

func upload(ctx context.Context, rels gol.Releases, owner, repo string, id int64, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	_, err = rels.UploadAsset(ctx, owner, repo, id, filepath.Base(path), f, info.Size(), "")
	return err
}
```

### 18.2 Apply the same protection everywhere, degrading gracefully

```go
rule := gol.BranchProtection{
	Pattern:              "main",
	RequiredApprovals:    1,
	RequiredStatusChecks: []string{"ci/test"},
	RequireSignedCommits: true,
}
_, err := client.BranchProtections().Create(ctx, owner, repo, rule)
if errors.Is(err, gol.ErrNotSupported) {
	// Bitbucket: no named checks, no signed commits. Keep what it can do.
	rule.RequiredStatusChecks, rule.RequireSignedCommits = nil, false
	_, err = client.BranchProtections().Create(ctx, owner, repo, rule)
}
if err != nil {
	log.Fatal(err)
}
```

### 18.3 Copy labels from one repository to another (even across forges)

```go
func copyLabels(ctx context.Context, src, dst gol.Client, srcOwner, srcRepo, dstOwner, dstRepo string) error {
	for l, err := range gol.All(ctx, gol.ListOptions{PerPage: 50},
		func(ctx context.Context, o gol.ListOptions) (gol.Page[gol.Label], error) {
			return src.Labels().List(ctx, srcOwner, srcRepo, o)
		}) {
		if err != nil {
			return err
		}
		_, err := dst.Labels().Create(ctx, dstOwner, dstRepo, gol.Label{Name: l.Name, Color: l.Color, Description: l.Description})
		if err != nil && gol.StatusCode(err) != http.StatusUnprocessableEntity && gol.StatusCode(err) != http.StatusConflict {
			return err // 409/422: the label already exists
		}
	}
	return nil
}
```

### 18.4 Onboard a developer (Forgejo)

```go
func onboard(ctx context.Context, admin gol.Admin, login, email string) error {
	if _, err := admin.Users().Create(ctx, gol.CreateUserOptions{
		Login: login, Email: email, Password: randomPassword(), SendNotify: true,
	}); err != nil {
		return err
	}
	devs, err := admin.Teams().GetByName(ctx, "acme", "developers")
	if err != nil {
		return err
	}
	return admin.Teams().AddMember(ctx, devs.ID, login) // also joins the organisation
}
```

### 18.5 CI trigger on push to main, signed and verified

```go
http.Handle("/hooks", webhook.Handler(secret, func(ctx context.Context, ev *webhook.Event) error {
	if ev.Name != "push" {
		return nil
	}
	push, err := ev.Push()
	if err != nil {
		return err
	}
	if push.Branch() != "main" || push.HeadCommit == nil { // HeadCommit nil: branch deleted
		return nil
	}
	return ciQueue.Enqueue(ctx, ev.Repo, push.After, ev.Delivery) // Delivery deduplicates retries
}))
```

---

## 19. Reference: errors returned without an HTTP call

These errors are detected by the library itself, before it changes anything on the server. Some checks, such as the owner-team check or a Bitbucket user lookup, read from the server first. The result-and-error cases in [§3.3](#33-errors) are different: there, the change has already been applied.

| Situation | Error |
|---|---|
| A required field is missing (`Name`, `Title`, `TagName`, `URL`, `Events`, …) | plain error naming the field |
| An invalid enum value (merge method, visibility, team permission, …) | plain error |
| An invalid `TeamAccess` combination | plain error |
| An option or method the backend cannot honour | `ErrNotSupported` (`*NotSupportedError`) |
| Deleting, renaming or changing the access of an owner team | `ErrOwnerTeam` |
| A webhook scope with an empty owner | plain error |
| `Webhooks().Edit` with a non-nil but empty `Events` | plain error |
| Bitbucket: a branch protection rule that restricts nothing | plain error |
| Bitbucket, with an empty owner and several workspaces | plain error asking you to name one |
| `webhook.Verify` with an empty secret | plain error |

Server-side refusals, such as permissions, conflicts or protected branches, are always `*APIError`.
