# GitHub Secrecy Tag Specification

## Secrecy Levels

This specification defines secrecy labels for GitHub objects using a simple confidentiality model:

- `[]` (public / unrestricted)
- `private:<owner>`
- `private:<owner>/<repo>`

For private repository objects, secrecy expansion is explicit:

- private-repo object emits `["private:<owner>", "private:<owner>/<repo>"]`

For public repository objects:

- public-repo object emits `[]`

This ensures owner-level and repo-level confidentiality are both represented.

---

## Scope Hierarchy

Secrecy scope has two practical levels from broadest to narrowest:

1. `<owner>`
2. `<owner>/<repo>`

Examples:

- `private:octo-org` applies to private data scoped to owner `octo-org`.
- `private:octo-org/example-repo` applies to private data scoped to that repository.

Containment semantics:

- `<owner>` is broader than `<owner>/<repo>`.
- `<owner>/<repo>` is the most specific repository scope.

Private repository data should include both labels as an explicit hierarchy expansion.

---

## Core Semantics

Secrecy assignment is derived from repository visibility:

- Public repository => `[]`
- Private repository => `["private:<owner>", "private:<owner>/<repo>"]`

### Flow Rule

Secrecy enforces confidentiality:

- A subject may read data only if subject secrecy clearance is a superset of data secrecy labels.
- A subject may write data only if resulting flow does not reduce confidentiality guarantees.

### Non-Downgrade Rule

Secrecy should be monotonic in derived outputs:

- Do not remove private secrecy labels once private scope is established for an object.
- Item-level response labeling may refine secrecy per item, but must not downgrade private items to public.

---

## Resource Label Rules (`label_resource`)

Resource labels are coarse pre-check labels by tool call.

| Tool / Resource Type | Private Repo | Public Repo |
|---|---|---|
| Repo-scoped read tools (`get_issue`, `list_issues`, `get_pull_request`, `list_pull_requests`, `get_commit`, `list_commits`, `get_file_contents`, `list_branches`, `list_tags`, `get_tag`, `list_releases`, `get_latest_release`, `get_release_by_tag`, `get_label`, `list_label`, `actions_get`, `actions_list`, `search_code`, `get_repository`, `get_repository_tree`, `list_discussions`, `get_discussion`, `get_discussion_comments`, `list_discussion_categories`) | `private:<owner>`, `private:<owner>/<repo>` | `[]` |
| Job logs (`get_job_logs`) | `secret` | `secret` |
| Sensitive file content (`get_file_contents` with sensitive paths) | `secret` | `secret` |
| Secret scanning alerts (`list_secret_scanning_alerts`, `get_secret_scanning_alert`) | `secret` | `secret` |
| Code scanning & Dependabot alerts (`list_code_scanning_alerts`, `get_code_scanning_alert`, `list_dependabot_alerts`, `get_dependabot_alert`) | `private:<owner>`, `private:<owner>/<repo>` | `private:<owner>`, `private:<owner>/<repo>` |
| Repo/org security advisories (`list_repository_security_advisories`, `list_org_repository_security_advisories`) | `private:<owner>`, `private:<owner>/<repo>` | `private:<owner>`, `private:<owner>/<repo>` |
| Artifact downloads (`actions_get` with method `download_workflow_run_artifact`) | `secret` | `secret` |
| User-scoped tools (`get_me`, `get_teams`, `get_team_members`, `list_starred_repositories`) | `private:user` | `private:user` |
| Gist tools (`list_gists`, `get_gist`) | `private:user` (conservative; response refines per-item) | `private:user` (conservative; response refines per-item) |
| Notification tools (`list_notifications`, `get_notification_details`) | `private:user` | `private:user` |
| Cross-repo search tools (`search_issues`, `search_pull_requests`, `search_repositories`, `search_users`, `search_orgs`) | coarse `[]` (response items refine) | coarse `[]` (response items refine) |
| Global security advisories (`list_global_security_advisories`, `get_global_security_advisory`) | `[]` (public CVE data) | `[]` (public CVE data) |
| Project tools (`projects_list`, `projects_get`, `list_projects`, `get_project`, `list_project_fields`, `list_project_items`) | `[]` (response items refine per-item) | `[]` (response items refine per-item) |

Notes:

- Resource labels are intentionally coarse for search/list APIs where results may span mixed visibility.
- Response labeling is authoritative when item-level visibility is available.

---

## Response Label Rules (`label_response`)

Response labels are fine-grained per item and should be treated as authoritative.

| Response Object Type | Private Repo | Public Repo |
|---|---|---|
| Repository item (`search_repositories`, `get_repository`) | `private:<owner>`, `private:<owner>/<repo>` | `[]` |
| Issue item (`list_issues`, `search_issues`, `get_issue`) | `private:<owner>`, `private:<owner>/<repo>` | `[]` |
| Pull request item (`list_pull_requests`, `search_pull_requests`, `get_pull_request`) | `private:<owner>`, `private:<owner>/<repo>` | `[]` |
| Commit item (`list_commits`, `get_commit`) | `private:<owner>`, `private:<owner>/<repo>` | `[]` |
| File content item (`get_file_contents`) | `private:<owner>`, `private:<owner>/<repo>` | `[]` |
| Branch/tag/release metadata item | `private:<owner>`, `private:<owner>/<repo>` | `[]` |
| GitHub Actions workflow/artifact metadata | `private:<owner>`, `private:<owner>/<repo>` | `[]` |
| Job logs (`get_job_logs`) | `secret` | `secret` |
| Security alert item | `private:<owner>`, `private:<owner>/<repo>` (or stricter tool-specific secrecy where configured) | `[]` (or stricter tool-specific secrecy where configured) |
| Global security advisory | `[]` (public CVE data) | `[]` (public CVE data) |
| Repo/org security advisory | `private:<owner>`, `private:<owner>/<repo>` | `private:<owner>`, `private:<owner>/<repo>` |
| Discussion item (`list_discussions`, `get_discussion`, `get_discussion_comments`) | `private:<owner>`, `private:<owner>/<repo>` | `[]` |
| Discussion category metadata (`list_discussion_categories`) | `private:<owner>`, `private:<owner>/<repo>` | `[]` |
| Gist item (`list_gists`, `get_gist`) | `private:user` (secret gists) / `[]` (public gists) | `private:user` (secret gists) / `[]` (public gists) |
| Notification item (`list_notifications`, `get_notification_details`) | `private:user` | `private:user` |
| Project item (`projects_list`, `list_project_items`) | per-item from referenced repo | per-item from referenced repo |
| User/org metadata (`get_me`, `get_teams`, `get_team_members`, `list_starred_repositories`, `search_orgs`) | `private:user` (user-scoped) / `[]` (org search) | `private:user` / `[]` |

---

## Visibility Determination

Visibility is determined from repository metadata (`private` boolean or equivalent backend metadata).

- `private = true` => apply private secrecy expansion
- `private = false` => apply `[]`
- unknown visibility => fail-secure behavior may conservatively treat as private until resolved

---

## Migration Requirement

For secrecy, standardize on these tags:

- `private:<owner>`
- `private:<owner>/<repo>`
- `[]` for public

For private repository objects, emit both owner and repo secrecy tags together.
