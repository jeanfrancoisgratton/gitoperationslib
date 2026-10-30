<p align="center">
  <img src="images/gitoperationslib_banner.png" alt="gitoperationslib banner" width="100%">
</p>
___

# gitoperationslib

[![Go Reference](https://pkg.go.dev/badge/github.com/jeanfrancoisgratton/gitoperationslib.svg)](https://pkg.go.dev/github.com/jeanfrancoisgratton/gitoperationslib)

A Go library for managing git forges through one set of interfaces. Write your code once against `gitoperationslib.Client` and run it against **Forgejo** (the primary backend), **GitHub** or **Bitbucket Cloud**. Anything a forge cannot do is reported with a typed error. The library never skips part of a request without saying so.

**Covered:** repositories, branches and tags, branch and tag protection, releases and assets, pull requests and reviews, issues (with labels, milestones and comments), webhooks (managing them, and receiving and verifying deliveries), reading users, organisations and teams with any token (Forgejo and GitHub), and Forgejo administration (managing users, organisations, teams).

## Documentation

| Document | Contents |
|---|---|
| **[docs/USAGE.md](docs/USAGE.md)** | The full guide: every package, interface, type and option, with examples and per-backend notes |
| [docs/support_grid.md](docs/support_grid.md) | Support level of every feature, per backend |
| [docs/CHANGELOG.md](docs/CHANGELOG.md) | Release history |
| [docs/TODO.md](docs/TODO.md) | Open items, planned work, and the reasons behind each limitation |
| [pkg.go.dev](https://pkg.go.dev/github.com/jeanfrancoisgratton/gitoperationslib) | API reference generated from the source |

## Installation

```sh
go get github.com/jeanfrancoisgratton/gitoperationslib
```

Versions follow [Semantic Versioning](https://semver.org): 1.0.0 is the first stable release, and the public API only breaks with a new major version.

How 1.x grows without breaking anyone:
- No method is ever added to an existing interface: that would break every other implementation of it, mocks included.
- New areas arrive as new optional interfaces, implemented by the backend clients, found with a type assertion and announced by a new `Capability` bit (like `Admin` and `ExternalAssets`).
- Minor versions may add struct fields, methods on concrete types, sentinel errors, constants and backend options.

**Requirements:**
- Go 1.27.2 or later.
- Forgejo 11.0.10 or later. `forgejo.New` checks the server version and refuses older servers.
- GitHub: github.com, or GitHub Enterprise Server.
- Bitbucket: Bitbucket Cloud only. Data Center has a different API.

The module has two third-party dependencies, both used only by the GitHub backend: [`google/go-github`](https://github.com/google/go-github) (v93), and [`golang.org/x/crypto`](https://pkg.go.dev/golang.org/x/crypto), which encrypts Actions secrets the way GitHub requires (libsodium sealed box).

## Quick start

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	gol "github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/forgejo"
)

func main() {
	ctx := context.Background()

	client, err := forgejo.New(ctx, "https://git.example.com", forgejo.Token(os.Getenv("FORGEJO_TOKEN")))
	if err != nil {
		log.Fatal(err)
	}

	repo, err := client.Repos().Create(ctx, gol.CreateRepoOptions{
		Owner:      "acme",
		Name:       "widget",
		Visibility: gol.VisibilityPrivate,
		AutoInit:   true,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("created", repo.FullName, repo.CloneURL)

	// Every page of a list, as a Go iterator.
	for r, err := range gol.All(ctx, gol.ListOptions{PerPage: 50},
		func(ctx context.Context, o gol.ListOptions) (gol.Page[gol.Repo], error) {
			return client.Repos().List(ctx, "acme", o)
		}) {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(r.FullName)
	}
}
```

To use another forge, change only the constructor:

```go
client, err := github.New(os.Getenv("GITHUB_TOKEN"))
client, err := bitbucket.New(bitbucket.APIToken(os.Getenv("BITBUCKET_EMAIL"), os.Getenv("BITBUCKET_TOKEN")))
```

[docs/USAGE.md](docs/USAGE.md) covers every area in detail.

## Architecture

```
gitoperationslib/             interfaces, domain types, errors, pagination  (no backend code)
├── forgejo/                  Forgejo backend      → internal/forgejoapi    (net/http)
├── github/                   GitHub backend       → google/go-github v93 + a small GraphQL client (+ x/crypto for secrets)
├── bitbucket/                Bitbucket backend    → internal/bitbucketapi  (net/http)
├── webhook/                  receiving deliveries (depends on the root package only)
└── internal/
    ├── forgejoapi/           Forgejo transport: base URL, auth, pagination, errors, uploads
    ├── bitbucketapi/         Bitbucket transport: REST 2.0, pagination, errors
    ├── partial/              helpers for the Directory info calls (paging, tolerated refusals)
    └── contracttest/         one integration suite every backend must pass
```

| Package | Import path | Role |
|---|---|---|
| root | `github.com/jeanfrancoisgratton/gitoperationslib` | `Client` and the area interfaces, domain types, errors, `Page`/`All`, `Ptr` |
| `forgejo` | `…/gitoperationslib/forgejo` | Forgejo backend; implements `Client`, `Admin`, `Account`, `CI` and `ExternalAssets` |
| `github` | `…/gitoperationslib/github` | GitHub backend; implements `Client`, `Account` and `CI` |
| `bitbucket` | `…/gitoperationslib/bitbucket` | Bitbucket Cloud backend; implements `Client`, `Account` and `CI` |
| `webhook` | `…/gitoperationslib/webhook` | Verifies, identifies and decodes webhook deliveries from all three forges |

How a call flows:

1. A constructor (`forgejo.New`, `github.New`, `bitbucket.New`) returns a concrete `*Client` that satisfies `gitoperationslib.Client`.
2. `Client` hands out one interface per area: `Repos()`, `Branches()`, `Tags()`, `BranchProtections()`, `TagProtections()`, `Releases()`, `PullRequests()`, `Issues()`, `Labels()`, `Milestones()`, `Webhooks()` and `Directory()` (users, organisations, teams).
3. The backend converts the library's option structs into the forge's request and calls its API. It then converts the response back into the library's types, so no forge-specific type crosses the boundary.
4. Error responses become a `*gitoperationslib.APIError`. Requests the forge cannot honour fail with an error matching `gitoperationslib.ErrNotSupported`.

The `webhook` package depends only on the root package, so a service that only receives deliveries does not pull in any backend.

## Design choices

- **One interface, several forges.** The domain types model what the forges share. Features only some forges have are separate, optional interfaces (`Admin`, `ExternalAssets`), so the shared model is not bent around one vendor. Check for them with `Capabilities()` or a type assertion.
- **Explicit refusals.** When a forge cannot do something (a merge method, an option, a whole area), the call returns `ErrNotSupported` with a `*NotSupportedError` naming the backend, the operation and the reason. A few calls also return the result along with that error: this happens when the server applied the request but silently ignored part of it, so you can see what actually happened.
- **No third-party types in the public API.** Every exported type belongs to this library, and conversion happens inside each backend. A `go-github` major version bump therefore never breaks callers.
- **Few dependencies.** The Forgejo and Bitbucket backends are small hand-written HTTP clients on `net/http`, not SDKs. The Forgejo SDK was rejected because of its churn, and because its minimum Go version would have become ours (see [DESIGN-NOTES.md](docs/DESIGN-NOTES.md)). Only GitHub uses an SDK (`go-github`, pinned), plus a minimal GraphQL client for what REST cannot do: draft toggling, auto-merge and issue deletion, and `golang.org/x/crypto` to encrypt Actions secrets (the standard library has no XSalsa20-Poly1305).
- **Consistent conventions:**
  - Every call takes a `context.Context`.
  - Lists are paginated the same way everywhere (`ListOptions`, `Page[T]`, `All`).
  - In `Edit…Options`, `nil` means "leave unchanged".
  - Enums are typed (`Visibility`, `MergeMethod`, `PRState`, …), never bare strings or booleans.
  - Webhook event names follow GitHub's and are translated for the other forges.
  - Every public struct has camelCase JSON tags. Fields only some forges report, and zero times, are omitted, and secrets are never serialised. See [USAGE §3.7](docs/USAGE.md#37-json).
- **Safe defaults.** A zero-value `BranchProtection` blocks deletion and force pushes. Deleting, renaming or changing the permissions of an organisation's owner team is refused (`ErrOwnerTeam`), because Forgejo would otherwise let an organisation lose all its owners. Webhook signatures are compared in constant time.
- **Forgejo first.** Forgejo is the reference backend, and its full feature set is exposed, including features GitHub lacks: extra merge methods, file-pattern protection, external release assets, and user, organisation and team administration. The minimum supported version is checked when connecting.
- **Verified against real servers.** One contract suite (`internal/contracttest`) runs against every backend, and it also checks that each backend fails the way its documentation says it should.

## Backend differences

The library adapts where it can, and returns `ErrNotSupported` where it cannot. In summary:

| Area | Forgejo | GitHub | Bitbucket Cloud |
|---|---|---|---|
| Repositories | ✅ | ✅ | ✅ owner = workspace |
| Branches and tags | ✅ | ✅ | ✅ no branch rename |
| Branch protection | ✅ protection rules | ✅ rulesets (free plans: public repositories) | ✅ branch restrictions |
| Tag protection | ✅ | ✅ rulesets | ❌ |
| Releases and assets | ✅ plus external assets | ✅ | ❌ |
| Pull requests and reviews | ✅ | ✅ drafts and auto-merge via GraphQL | ✅ no reopening, no auto-merge |
| Issues, labels, milestones, comments | ✅ | ✅ | ❌ issue tracker retired |
| Webhooks (repository, organisation/workspace) | ✅ | ✅ | ✅ |
| Receiving deliveries (`webhook`) | ✅ | ✅ | ⚠️ not yet checked against live deliveries |
| Reading users, organisations, teams (`Directory`) | ✅ | ✅ | ⚠️ the authenticated user (`Me`) only |
| Managing users, organisations, teams (`Admin`) | ✅ | — | — |

Differences worth knowing before you start:

- **Drafts on Forgejo** are pull requests whose title starts with `WIP:` or `[WIP]`. `Draft: true` adds the prefix, and `Title` comes back with it.
- **GitHub rulesets** stand in for both branch and tag protection. On free plans they only work on public repositories, and only teams (not individual users) can be exempted.
- **Bitbucket** has no public login names. User fields hold the account's **nickname**, and inputs accept a nickname, an account ID or a `{UUID}`.
- **Merge methods:** Forgejo has six, GitHub three (merge, squash, rebase) and Bitbucket five.
- **Webhooks:** Forgejo silently drops event names it does not know, and the library reports them as an error. Only Forgejo has branch filters and authorization headers.

The full feature-by-feature comparison is in **[docs/support_grid.md](docs/support_grid.md)**. Per-call notes are in [docs/USAGE.md](docs/USAGE.md).

## Testing

```sh
go test -race ./...                    # unit tests: no network, no credentials
go test -tags integration ./...        # contract checks against real forges (see below)
```

Integration tests skip themselves unless their environment variables are set. They only create and delete resources named `gitopslib-it-…`.

| Suite | Environment |
|---|---|
| Forgejo | `GITOPSLIB_FORGEJO_URL`, `GITOPSLIB_FORGEJO_TOKEN` (scopes: repository, organization, user, issue — read and write), `GITOPSLIB_FORGEJO_ORG` (a throwaway organisation the token's user owns) |
| GitHub | `GITOPSLIB_GITHUB_TOKEN`, `GITOPSLIB_GITHUB_ORG` (an organisation, or the account's own login) |
| Bitbucket | `GITOPSLIB_BITBUCKET_TOKEN` (Atlassian API token or access token), `GITOPSLIB_BITBUCKET_EMAIL` (required with an API token), `GITOPSLIB_BITBUCKET_WORKSPACE` (optional when the token sees a single workspace) |
| Forgejo admin | a disposable local instance (below); non-loopback URLs are refused |

```sh
scripts/forgejo-testenv.sh start   # Forgejo 15 in Docker on 127.0.0.1:3999, writes .env.testenv
set -a; source .env; source .env.testenv; set +a
go test -tags integration ./...
scripts/forgejo-testenv.sh stop    # removes the container and .env.testenv
```

The GitHub protection checks briefly create a **public** repository, because free plans only allow rulesets on public repositories. Running the GitHub suites many times in a row can trigger GitHub's secondary rate limit on repository creation, which blocks new repositories for up to an hour.

## License

Copyright (C) 2026 Jean-François Gratton

This program is free software: you can redistribute it and/or modify it under the terms of the GNU General Public License as published by the Free Software Foundation, either version 3 of the License, or (at your option) any later version.

This program is distributed in the hope that it will be useful, but WITHOUT ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the [LICENSE](LICENSE) file for details.

SPDX-License-Identifier: GPL-3.0-or-later
