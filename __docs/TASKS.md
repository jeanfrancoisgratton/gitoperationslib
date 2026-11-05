# Tasks

Increments of the 1.3.0 work. Item numbers refer to `__docs/TODO.md`.

| # | Items | Work | Status |
|---|---|---|---|
| A | 38, 40, 41, 42 | Audit list endpoints for the missing `Link` header, fix the flaky merge test, and resolve the Forgejo 11 failures (closed by the new 15.0.0 minimum). Stop test repositories from triggering the Mattermost and Jenkins hooks. | Done, committed |
| B | 54 | Look up organisation labels when a name isn't found in the repository, so normal calls cost no extra request. | Done, committed |
| C | 8, 9 | PR diff/patch, files, commits and line comments, plus `A..B`, author filter and commit diffs. | Done, committed |
| D | 43, 44 | Forgejo git hooks, delivery history and redelivery, `insecure_ssl`, wildcard events and more typed payloads. | Done, committed |
| E | 5 | Collaborators, push mirrors and mirror sync, without deploy keys or migration (`CapMigrate`). | Done, committed |
| F | 6 | GitHub environment secrets, Bitbucket deployment variables and GitHub organisation secrets limited to selected repositories. | Done, committed |

## Outside the increments

| Items | Status |
|---|---|
| 27, 39, 55, 57 | Closed, no increment needed |
| 37, 97 | Library side done, not committed. The reports are in `__docs/UPSTREAM-REPORTS.md`, ready to file |
| 56 | Deferred |
| 100–110 | Open: what Forgejo gained after 15.0.2, see `__docs/post-v15.md` |
| 96, 98 | Done, not committed |
| 99 | Open: finish checking D and F on GitHub once it allows creating repositories again; `OrgSecrets` also needs an organisation to test in |
