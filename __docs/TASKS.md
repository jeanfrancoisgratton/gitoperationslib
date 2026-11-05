# Tasks

Increments of the 1.3.0 work. Item numbers refer to `__docs/TODO.md`.

| # | Items | Work | Status |
|---|---|---|---|
| A | 38, 40, 41, 42 | Audit list endpoints for the missing `Link` header, fix the flaky merge test, and resolve the Forgejo 11 failures (closed by the new 15.0.0 minimum). Stop test repositories from triggering the Mattermost and Jenkins hooks. | Done, committed |
| B | 54 | Look up organisation labels when a name isn't found in the repository, so normal calls cost no extra request. | Done, committed |
| C | 8, 9 | PR diff/patch, files, commits and line comments, plus `A..B`, author filter and commit diffs. | Done, not committed |
| D | 43, 44 | Forgejo git hooks, delivery history and redelivery, `insecure_ssl`, wildcard events and more typed payloads. | To do |
| E | 5 | Collaborators, push mirrors and mirror sync, without deploy keys or migration (`CapMigrate`). | To do |
| F | 6 | GitHub environment secrets, Bitbucket deployment variables and GitHub organisation secrets limited to selected repositories. | To do |

## Outside the increments

| Items | Status |
|---|---|
| 27, 39, 55, 57 | Closed, no increment needed |
| 37 | Open: the upstream report is drafted and needs filing |
| 56 | Deferred |
| 96 | Open: audit the other Bitbucket list calls for page tokens; found during C, not yet scheduled |
