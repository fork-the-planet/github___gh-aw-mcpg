//! Tool-specific label rule application
//!
//! This module contains the `apply_tool_labels` function which applies
//! tool-specific labeling rules based on the tool name and arguments.

use serde_json::Value;

use super::constants::{field_names, scope_names, SENSITIVE_FILE_KEYWORDS, SENSITIVE_FILE_PATTERNS};
use super::helpers::{
    author_association_floor_from_str,
    elevate_via_collaborator_permission, ensure_integrity_baseline,
    extract_number_as_string, extract_repo_info, extract_repo_info_from_search_query,
    format_repo_id, is_any_trusted_actor, is_default_branch_commit_context,
    is_default_branch_ref, max_integrity,
    merged_integrity, policy_private_scope_label, private_user_label, project_github_label,
    reader_integrity, writer_integrity, PolicyContext,
};

fn apply_repo_visibility_secrecy(
    owner: &str,
    repo: &str,
    repo_id: &str,
    current_secrecy: Vec<String>,
    ctx: &PolicyContext,
) -> Vec<String> {
    if owner.is_empty() || repo.is_empty() || repo_id.is_empty() {
        return current_secrecy;
    }

    match super::backend::is_repo_private(owner, repo) {
        Some(true) => policy_private_scope_label(owner, repo, repo_id, ctx),
        Some(false) => vec![],
        None => {
            // Fail secure in runtime when visibility cannot be determined.
            // Keep tests deterministic (backend host calls are unavailable in unit tests).
            if cfg!(test) {
                current_secrecy
            } else {
                policy_private_scope_label(owner, repo, repo_id, ctx)
            }
        }
    }
}

fn private_writer_integrity(
    repo_id: &str,
    repo_private: Option<bool>,
    ctx: &PolicyContext,
) -> Vec<String> {
    if repo_private == Some(true) {
        writer_integrity(repo_id, ctx)
    } else {
        vec![]
    }
}

/// Resolve the effective (owner, repo, repo_id) for a search tool call.
///
/// Extracts the repo scope from the search query first; if the query lacks a
/// `repo:` qualifier, falls back to the `owner`/`repo` fields in `tool_args`.
fn resolve_search_scope(
    tool_args: &Value,
    owner: &str,
    repo: &str,
) -> (String, String, String) {
    let query = tool_args
        .get("query")
        .and_then(|v| v.as_str())
        .unwrap_or("");
    let (q_owner, q_repo, q_repo_id) = extract_repo_info_from_search_query(query);
    if !q_repo_id.is_empty() {
        (q_owner, q_repo, q_repo_id)
    } else if !owner.is_empty() && !repo.is_empty() {
        (owner.to_string(), repo.to_string(), format_repo_id(owner, repo))
    } else {
        (String::new(), String::new(), String::new())
    }
}

/// Compute integrity for a user-authored resource (issue or PR), applying:
///   1. `author_association` floor
///   2. Trusted bot/user elevation to writer level
///   3. Collaborator-permission fallback for org repos
fn resolve_author_integrity(
    owner: &str,
    repo: &str,
    repo_id: &str,
    author_login: Option<&str>,
    author_association: Option<&str>,
    resource_label: &str,
    resource_num: &str,
    base_integrity: Vec<String>,
    ctx: &PolicyContext,
) -> Vec<String> {
    let mut floor = author_association_floor_from_str(repo_id, author_association, ctx);

    if let Some(login) = author_login {
        if is_any_trusted_actor(login, ctx) {
            floor = max_integrity(repo_id, floor, writer_integrity(repo_id, ctx), ctx);
        }
        let resource_id = format!("{}/{}#{}", owner, repo, resource_num);
        floor = elevate_via_collaborator_permission(
            login, repo_id, resource_label, &resource_id, floor, ctx,
        );
    }

    max_integrity(repo_id, base_integrity, floor, ctx)
}

// ============================================================================
// Tool Label Application
// ============================================================================

/// Apply tool-specific labels based on the tool name and arguments
pub fn apply_tool_labels(
    tool_name: &str,
    tool_args: &Value,
    repo_id: &str,
    mut secrecy: Vec<String>,
    mut integrity: Vec<String>,
    mut desc: String,
    ctx: &PolicyContext,
) -> (Vec<String>, Vec<String>, String) {
    let (owner, repo, _) = extract_repo_info(tool_args);
    let mut baseline_scope = repo_id.to_string();
    let repo_private = if owner.is_empty() || repo.is_empty() {
        None
    } else {
        super::backend::is_repo_private(&owner, &repo)
    };

    match tool_name {
        // === Issues (repo-scoped) ===
        "get_issue" | "issue_read" | "list_issues" => {
            // Issues are user-submitted, low integrity
            // I(issue) = contributor if author is contributor, else untrusted (empty)
            // S(issue) = S(repo) - inherits from repository visibility
            if !owner.is_empty() && !repo.is_empty() {
                if let Some(issue_num) =
                    extract_number_as_string(tool_args, field_names::ISSUE_NUMBER)
                {
                    desc = format!("issue:{}/{}#{}", owner, repo, issue_num);
                }
            }
            secrecy = apply_repo_visibility_secrecy(&owner, &repo, repo_id, secrecy, ctx);
            integrity = private_writer_integrity(repo_id, repo_private, ctx);

            if matches!(tool_name, "get_issue" | "issue_read") {
                if let Some(issue_num) =
                    extract_number_as_string(tool_args, field_names::ISSUE_NUMBER)
                {
                    if let Some(info) =
                        super::backend::get_issue_author_info(&owner, &repo, &issue_num)
                    {
                        integrity = resolve_author_integrity(
                            &owner, &repo, repo_id,
                            info.author_login.as_deref(),
                            info.author_association.as_deref(),
                            "issue_read", &issue_num,
                            integrity, ctx,
                        );
                    }
                }
            }
        }

        // === Issue Pin/Unpin (repo-scoped write) ===
        "pin_issue" | "unpin_issue" => {
            // Pinning/unpinning an issue is a repo-level cosmetic write operation.
            // S = S(repo) — inherits from repository visibility
            // I = writer (requires repo write access to change issue pin state)
            if !owner.is_empty() && !repo.is_empty() {
                if let Some(issue_num) =
                    extract_number_as_string(tool_args, field_names::ISSUE_NUMBER)
                {
                    desc = format!("issue:{}/{}#{}", owner, repo, issue_num);
                }
            }
            secrecy = apply_repo_visibility_secrecy(&owner, &repo, repo_id, secrecy, ctx);
            integrity = writer_integrity(repo_id, ctx);
        }

        // === Blocked repository operations ===
        // Applies repo-visibility secrecy before label_resource enforces the unconditional
        // block via is_blocked_tool(). Covers: irreversible ownership changes
        // (transfer_repository) and unsupported gh-repo operations (archive, unarchive,
        // rename).
        "transfer_repository"
        | "archive_repository"
        | "unarchive_repository"
        | "rename_repository" => {
            secrecy = apply_repo_visibility_secrecy(&owner, &repo, repo_id, secrecy, ctx);
        }

        // Search issues / pull requests: extract repo scope from query or tool_args when available
        "search_issues" | "search_pull_requests" => {
            let (s_owner, s_repo, s_repo_id) = resolve_search_scope(tool_args, &owner, &repo);
            if !s_repo_id.is_empty() {
                desc = format!("{}:{}", tool_name, s_repo_id);
                secrecy =
                    apply_repo_visibility_secrecy(&s_owner, &s_repo, &s_repo_id, secrecy, ctx);
                // Use the search query's repo for privacy check when tool_args lacks owner/repo
                let search_repo_private = repo_private
                    .or_else(|| super::backend::is_repo_private(&s_owner, &s_repo));
                integrity = private_writer_integrity(&s_repo_id, search_repo_private, ctx);
            } else {
                integrity = vec![];
            }
        }

        // === Pull Requests ===
        "get_pull_request" | "pull_request_read" | "list_pull_requests" => {
            // I(PR) = merged if merged; otherwise approved/unapproved/contributor floor by evidence
            // S(PR) = S(repo)
            if !owner.is_empty() && !repo.is_empty() {
                let pr_num = extract_number_as_string(tool_args, field_names::PULL_NUMBER)
                    .or_else(|| extract_number_as_string(tool_args, "pullNumber"));
                if let Some(num) = pr_num {
                    desc = format!("pr:{}/{}#{}", owner, repo, num);
                }
            }
            secrecy = apply_repo_visibility_secrecy(&owner, &repo, repo_id, secrecy, ctx);
            if matches!(tool_name, "get_pull_request" | "pull_request_read") {
                let pull_number = extract_number_as_string(tool_args, field_names::PULL_NUMBER)
                    .or_else(|| extract_number_as_string(tool_args, "pullNumber"));
                if let Some(number) = pull_number {
                    if let Some(facts) =
                        super::backend::get_pull_request_facts(&owner, &repo, &number)
                    {
                        integrity = resolve_author_integrity(
                            &owner, &repo, repo_id,
                            facts.author_login.as_deref(),
                            facts.author_association.as_deref(),
                            "pull_request_read", &number,
                            integrity, ctx,
                        );

                        if repo_private == Some(true) {
                            integrity = max_integrity(
                                repo_id,
                                integrity,
                                writer_integrity(repo_id, ctx),
                                ctx,
                            );
                        } else {
                            match facts.is_forked {
                                Some(true) => {
                                    integrity = max_integrity(
                                        repo_id,
                                        integrity,
                                        reader_integrity(repo_id, ctx),
                                        ctx,
                                    );
                                }
                                Some(false) => {
                                    integrity = max_integrity(
                                        repo_id,
                                        integrity,
                                        writer_integrity(repo_id, ctx),
                                        ctx,
                                    );
                                }
                                None => {}
                            }
                        }

                        if facts.is_merged {
                            integrity = max_integrity(
                                repo_id,
                                integrity,
                                merged_integrity(repo_id, ctx),
                                ctx,
                            );
                        }
                    } else {
                        integrity = private_writer_integrity(repo_id, repo_private, ctx);
                    }
                } else {
                    integrity = private_writer_integrity(repo_id, repo_private, ctx);
                }
            } else {
                // Collection/list calls are coarse; response labeling refines item-by-item.
                integrity = private_writer_integrity(repo_id, repo_private, ctx);
            }
        }

        // === Commits ===
        "get_commit" | "list_commits" => {
            // I(commit) = merged on default branch, approved in private repos, else contributor floor
            // S(commit) = S(repo)
            if !owner.is_empty() && !repo.is_empty() {
                if let Some(sha) = tool_args.get(field_names::SHA).and_then(|v| v.as_str()) {
                    let short_sha = if sha.len() > 8 { &sha[..8] } else { sha };
                    desc = format!("commit:{}/{}@{}", owner, repo, short_sha);
                }
            }
            secrecy = apply_repo_visibility_secrecy(&owner, &repo, repo_id, secrecy, ctx);
            let sha_or_ref = tool_args
                .get(field_names::SHA)
                .and_then(|v| v.as_str())
                .unwrap_or("");
            let is_default_ref = is_default_branch_commit_context(tool_name, sha_or_ref);
            let repo_private_effective = match repo_private {
                Some(value) => value,
                None => !cfg!(test),
            };

            integrity = if repo_private_effective {
                if is_default_ref {
                    merged_integrity(repo_id, ctx)
                } else {
                    writer_integrity(repo_id, ctx)
                }
            } else if is_default_ref {
                merged_integrity(repo_id, ctx)
            } else {
                vec![]
            };
        }

        // === Security: Secret Scanning ===
        "list_secret_scanning_alerts" | "get_secret_scanning_alert" => {
            // S(alert) = private:owner/repo - alerts may contain actual secret values
            // Treated as private regardless of repo visibility (secrets in public repos are still sensitive)
            // I(alert) = approved - automated detection
            secrecy = policy_private_scope_label(&owner, &repo, repo_id, ctx);
            integrity = writer_integrity(repo_id, ctx);
        }

        // === Security: Code Scanning & Dependabot ===
        "list_code_scanning_alerts"
        | "get_code_scanning_alert"
        | "list_dependabot_alerts"
        | "get_dependabot_alert" => {
            // S(alert) = private:repo - security findings are sensitive
            // I(alert) = approved - tool output, not user-controlled
            secrecy = policy_private_scope_label(&owner, &repo, repo_id, ctx);
            integrity = writer_integrity(repo_id, ctx);
        }

        // === Actions: Job Logs ===
        "get_job_logs" => {
            // Job logs may contain CI secrets (e.g. accidentally printed tokens) even in public repos.
            // Always treat as private regardless of repository visibility.
            // S(logs) = private:owner/repo; I(logs) = approved (system-generated output)
            secrecy = policy_private_scope_label(&owner, &repo, repo_id, ctx);
            integrity = writer_integrity(repo_id, ctx);
        }

        // === Actions: Workflow/Artifact Metadata and Artifact Downloads ===
        "actions_get" => {
            let method = tool_args.get("method").and_then(|v| v.as_str()).unwrap_or("");
            if method == "download_workflow_run_artifact" {
                // Artifact downloads may contain sensitive data or accidentally-included secrets.
                // Always treat as private regardless of repository visibility.
                // S(artifact) = private:owner/repo; I(artifact) = approved
                secrecy = policy_private_scope_label(&owner, &repo, repo_id, ctx);
            } else {
                secrecy = apply_repo_visibility_secrecy(&owner, &repo, repo_id, secrecy, ctx);
            }
            integrity = writer_integrity(repo_id, ctx);
        }

        // === Repo-scoped resources: visibility-inherited secrecy, approved integrity ===
        // S = inherits from repo visibility; I = approved (writer-level)
        "actions_list"
        | "get_discussion"
        | "get_discussion_comments"
        | "get_label"
        | "get_repository"
        | "get_repository_tree"
        | "get_tag"
        | "list_branches"
        | "list_discussion_categories"
        | "list_discussions"
        | "list_label"
        | "list_releases"
        | "get_latest_release"
        | "get_release_by_tag"
        | "list_tags" => {
            secrecy = apply_repo_visibility_secrecy(&owner, &repo, repo_id, secrecy, ctx);
            integrity = writer_integrity(repo_id, ctx);
        }

        // === Content Access ===
        "get_file_contents" => {
            secrecy = apply_repo_visibility_secrecy(&owner, &repo, repo_id, secrecy, ctx);
            // File secrecy based on path patterns
            if let Some(path) = tool_args.get("path").and_then(|v| v.as_str()) {
                secrecy = check_file_secrecy(path, secrecy, &owner, &repo, repo_id, ctx);
            }
            let branch_ref = tool_args.get("ref").and_then(|v| v.as_str()).unwrap_or("");
            integrity = if is_default_branch_ref(branch_ref) {
                merged_integrity(repo_id, ctx)
            } else {
                writer_integrity(repo_id, ctx)
            };
        }

        // === Code Search ===
        "search_code" => {
            // Code search can expose private code
            // S(code) = inherits from repo secrecy
            // I(code) = approved - code from repository
            let (s_owner, s_repo, s_repo_id) = resolve_search_scope(tool_args, &owner, &repo);
            if !s_repo_id.is_empty() {
                baseline_scope = s_repo_id.clone();
                desc = format!("search_code:{}", s_repo_id);
                secrecy =
                    apply_repo_visibility_secrecy(&s_owner, &s_repo, &s_repo_id, secrecy, ctx);
                integrity = writer_integrity(&s_repo_id, ctx);
            } else {
                secrecy =
                    apply_repo_visibility_secrecy(&owner, &repo, repo_id, secrecy, ctx);
                integrity = writer_integrity(repo_id, ctx);
            }
        }

        // === Repository Metadata ===
        "search_repositories" => {
            // Repository metadata has approved-level integrity
            // Secrecy will be determined per-item based on private flag
            integrity = writer_integrity(repo_id, ctx);
        }

        // === Issue Types ===
        "list_issue_types" => {
            // Org-level issue types
            // S = inherits from org
            // I = project:org - maintained by org admins
            integrity = project_github_label(ctx);
        }

        // === User Search ===
        "search_users" => {
            // Public user profiles
            // S = public (empty)
            // I = project:github - GitHub's data
            secrecy = vec![];
            integrity = project_github_label(ctx);
        }

        // === GitHub Projects (org-scoped) ===
        // Canonical names (projects_list, projects_get) plus deprecated aliases
        "list_projects" | "get_project" | "list_project_fields" | "list_project_items"
        | "projects_list" | "projects_get" => {
            // Projects are org-scoped; creating/managing projects requires org membership.
            // I = approved:<owner> — equivalent to MEMBER author_association
            // S = empty by default (public project); per-item secrecy for items is refined in
            //     label_response_paths for list_project_items
            if !owner.is_empty() {
                baseline_scope = owner.clone();
                integrity = writer_integrity(&baseline_scope, ctx);
            }
        }

        // === Gists (user-scoped) ===
        "list_gists" | "get_gist" | "create_gist" | "update_gist" => {
            // Gists are user content; secrecy depends on public/secret flag.
            // Resource-level: conservative labeling; response labeling refines per-item.
            // S = private:user (conservative — some gists may be secret)
            // I = unapproved (user content, no repo-level trust signal)
            secrecy = private_user_label();
            baseline_scope = scope_names::USER.to_string();
            integrity = reader_integrity(scope_names::USER, ctx);
        }

        // === Notifications (user-scoped, private) ===
        "list_notifications" | "get_notification_details" => {
            // Notifications are private to the authenticated user.
            // S = private:user
            // I = none (notifications reference external content of unknown trust)
            secrecy = private_user_label();
            integrity = vec![];
        }

        // === Notification management (account-scoped writes) ===
        "dismiss_notification"
        | "mark_all_notifications_read"
        | "manage_notification_subscription"
        | "manage_repository_notification_subscription" => {
            // These operations change notification/subscription state and return minimal metadata.
            // S = public (empty); I = project:github
            secrecy = vec![];
            baseline_scope = scope_names::GITHUB.to_string();
            integrity = project_github_label(ctx);
        }

        // === Private GitHub-controlled metadata (user-associated): PII/org-structure sensitive ===
        "get_me"
        | "get_teams"
        | "get_team_members"
        | "list_starred_repositories"
        | "get_copilot_space"
        | "list_copilot_spaces" => {
            // User profile, org team membership, starred repos, and Copilot Spaces are all
            // GitHub-controlled metadata that may contain PII or reveal internal org structure.
            // S = private:user
            // I = project:github (GitHub-controlled metadata)
            secrecy = private_user_label();
            baseline_scope = scope_names::GITHUB.to_string();
            integrity = project_github_label(ctx);
        }

        // === Public GitHub-controlled metadata: org profiles, advisories, docs ===
        "search_orgs"
        | "list_global_security_advisories"
        | "get_global_security_advisory"
        | "github_support_docs_search" => {
            // Public organization profiles, global CVE advisories, and GitHub docs contain no
            // private data but are curated/controlled by GitHub.
            // S = public (empty)
            // I = project:github (GitHub-controlled metadata)
            secrecy = vec![];
            baseline_scope = scope_names::GITHUB.to_string();
            integrity = project_github_label(ctx);
        }

        // === Security Advisories (repository/org-scoped) ===
        "list_repository_security_advisories" | "list_org_repository_security_advisories" => {
            // Repository/org security advisories may include draft advisories
            // with non-public vulnerability details.
            // S = private:repo — may contain embargoed vulnerability info
            // I = approved — maintained by repo security contacts
            secrecy = policy_private_scope_label(&owner, &repo, repo_id, ctx);
            integrity = writer_integrity(repo_id, ctx);
        }

        // === Issue/PR write operations (repo-scoped) ===
        "create_issue" | "issue_write" | "sub_issue_write" | "add_issue_comment"
        | "create_pull_request" | "create_pull_request_with_copilot"
        | "update_pull_request" | "merge_pull_request"
        | "pull_request_review_write" | "add_comment_to_pending_review"
        | "add_reply_to_pull_request_comment" => {
            // Write operations that return the created/modified resource.
            // S = S(repo) — response contains repo-scoped content
            // I = writer (agent-authored content)
            secrecy = apply_repo_visibility_secrecy(&owner, &repo, repo_id, secrecy, ctx);
            integrity = writer_integrity(repo_id, ctx);
        }

        // === Granular repo-scoped write operations ===
        // Covers granular issue mutation tools (including custom field mutations),
        // sub-issue management, granular PR mutation tools, and PR review tools.
        // All follow: S = S(repo), I = writer.
        "update_issue_assignees"
        | "update_issue_body"
        | "update_issue_labels"
        | "update_issue_milestone"
        | "update_issue_state"
        | "update_issue_title"
        | "update_issue_type"
        | "set_issue_fields"
        | "add_sub_issue"
        | "remove_sub_issue"
        | "reprioritize_sub_issue"
        | "update_pull_request_body"
        | "update_pull_request_draft_state"
        | "update_pull_request_state"
        | "update_pull_request_title"
        | "add_pull_request_review_comment"
        | "create_pull_request_review"
        | "delete_pending_pull_request_review"
        | "request_pull_request_reviewers"
        | "resolve_review_thread"
        | "submit_pending_pull_request_review"
        | "unresolve_review_thread" => {
            secrecy = apply_repo_visibility_secrecy(&owner, &repo, repo_id, secrecy, ctx);
            integrity = writer_integrity(repo_id, ctx);
        }

        // === Repo content and structure write operations ===
        "create_or_update_file" | "push_files" | "delete_file" | "create_branch"
        | "update_pull_request_branch" => {
            // Write operations that modify repo content/structure.
            // S = S(repo) — response references repo-scoped content
            // I = writer (agent-authored content)
            secrecy = apply_repo_visibility_secrecy(&owner, &repo, repo_id, secrecy, ctx);
            integrity = writer_integrity(repo_id, ctx);
        }

        // === Repository creation/fork (user/org-scoped writes) ===
        "create_repository" | "fork_repository" => {
            // Creating/forking repositories is account-scoped and does not return repo content.
            // S = public (empty); I = writer(github)
            secrecy = vec![];
            baseline_scope = scope_names::GITHUB.to_string();
            integrity = writer_integrity(scope_names::GITHUB, ctx);
        }

        // === Projects write operations (org-scoped) ===
        "projects_write"
        // Deprecated aliases that map to projects_write
        | "add_project_item" | "update_project_item" | "delete_project_item" => {
            // Projects are org-scoped; write responses carry the same labels as reads.
            // I = approved:<owner>
            if !owner.is_empty() {
                baseline_scope = owner.clone();
                integrity = writer_integrity(&baseline_scope, ctx);
            }
        }

        // === Label write operations (repo-scoped) ===
        "label_write" => {
            // Label creates/updates/deletes return repo-scoped content.
            // S = S(repo); I = writer
            secrecy = apply_repo_visibility_secrecy(&owner, &repo, repo_id, secrecy, ctx);
            integrity = writer_integrity(repo_id, ctx);
        }

        // === Actions: Workflow run triggers ===
        "actions_run_trigger"
        // Deprecated aliases that map to actions_run_trigger
        | "run_workflow" | "delete_workflow_run_logs" => {
            // Triggering a workflow run returns repo-scoped metadata.
            // S = S(repo); I = writer
            secrecy = apply_repo_visibility_secrecy(&owner, &repo, repo_id, secrecy, ctx);
            integrity = writer_integrity(repo_id, ctx);
        }

        // === Actions: Workflow run cancel/rerun ===
        "cancel_workflow_run"
        | "force_cancel_workflow_run"
        | "rerun_workflow_run"
        | "rerun_failed_jobs"
        | "rerun_workflow_job" => {
            // These modify workflow run state; repo-scoped write.
            // S = S(repo); I = writer
            secrecy = apply_repo_visibility_secrecy(&owner, &repo, repo_id, secrecy, ctx);
            integrity = writer_integrity(repo_id, ctx);
        }

        // === Copilot coding-agent task (blocked: unsupported agent operation) ===
        "create_agent_task" => {
            // Creates a Copilot coding-agent job that modifies repo branches and opens a PR.
            // Blocked via is_blocked_tool(); secrecy applied so the resource is correctly
            // classified before the integrity override in label_resource.
            // S = S(repo); I = blocked (override applied in label_resource)
            secrecy = apply_repo_visibility_secrecy(&owner, &repo, repo_id, secrecy, ctx);
        }

        // === Copilot agent operations (repo-scoped) ===
        "assign_copilot_to_issue" | "request_copilot_review" => {
            // Copilot assignment/review requests return repo-scoped content.
            // S = S(repo); I = writer
            secrecy = apply_repo_visibility_secrecy(&owner, &repo, repo_id, secrecy, ctx);
            integrity = writer_integrity(repo_id, ctx);
        }

        // === Repository settings edit (can change visibility) ===
        "edit_repository" => {
            // Can change repo visibility, security settings, default branch.
            // S = S(repo); I = writer (requires admin access)
            secrecy = apply_repo_visibility_secrecy(&owner, &repo, repo_id, secrecy, ctx);
            integrity = writer_integrity(repo_id, ctx);
        }

        // === PR revert (creates revert branch + PR) ===
        "revert_pull_request" => {
            // Creates a new branch + PR reverting a merged PR.
            // S = S(repo); I = writer
            secrecy = apply_repo_visibility_secrecy(&owner, &repo, repo_id, secrecy, ctx);
            integrity = writer_integrity(repo_id, ctx);
        }

        // === Deploy key management (SSH key with optional write access) ===
        "add_deploy_key" | "delete_deploy_key" => {
            // Manages SSH deploy keys — `add_deploy_key` may grant persistent write access.
            // S = at least private; scope is policy-dependent (may be unscoped, owner-scoped, or repo-scoped)
            // I = writer (requires admin access)
            secrecy = policy_private_scope_label(&owner, &repo, repo_id, ctx);
            integrity = writer_integrity(repo_id, ctx);
        }

        // === Dynamic toolset enablement (capability expansion) ===
        "enable_toolset" => {
            // Enabling a toolset expands the agent's runtime capability set.
            // Requires writer-level integrity to prevent low-trust agents from
            // self-escalating by enabling additional tool groups.
            // S = public (empty — no repository-scoped data); I = writer (github)
            baseline_scope = scope_names::GITHUB.to_string();
            integrity = writer_integrity(scope_names::GITHUB, ctx);
        }

        // === Star/unstar operations (public metadata) ===
        "star_repository" | "unstar_repository" => {
            // Starring is a public action; response is minimal metadata.
            // S = public (empty); I = project:github
            secrecy = vec![];
            baseline_scope = scope_names::GITHUB.to_string();
            integrity = project_github_label(ctx);
        }

        // === Issue/PR comment editing/deletion (pre-emptive) ===
        "update_issue_comment" | "delete_issue_comment" => {
            // Editing or deleting an issue/PR comment is a repo-scoped write.
            // S = S(repo); I = writer
            secrecy = apply_repo_visibility_secrecy(&owner, &repo, repo_id, secrecy, ctx);
            integrity = writer_integrity(repo_id, ctx);
        }

        // === Release management (pre-emptive) ===
        "create_release" | "edit_release" | "delete_release" => {
            // Release operations are repo-scoped writes.
            // S = S(repo); I = writer
            secrecy = apply_repo_visibility_secrecy(&owner, &repo, repo_id, secrecy, ctx);
            integrity = writer_integrity(repo_id, ctx);
        }

        // === Gist deletion (pre-emptive) ===
        "delete_gist" => {
            // Gist deletion is a write on user-scoped content.
            // Conservatively treat gists as private/user-scoped, consistent with
            // other gist operations that may target secret gists.
            // S = private_user; I = writer(user)
            secrecy = private_user_label();
            baseline_scope = scope_names::USER.to_string();
            integrity = writer_integrity(scope_names::USER, ctx);
        }

        _ => {
            // Default: inherit provided labels
        }
    }

    (
        secrecy,
        ensure_integrity_baseline(&baseline_scope, integrity, ctx),
        desc,
    )
}

/// Check if a file path contains sensitive patterns.
/// If sensitive, returns a private-scoped secrecy label for the given owner/repo
/// regardless of the repository's public/private visibility — sensitive files
/// (credentials, keys, workflow definitions) should always be restricted.
/// Otherwise returns `default_secrecy` unchanged.
fn check_file_secrecy(
    path: &str,
    default_secrecy: Vec<String>,
    owner: &str,
    repo: &str,
    repo_id: &str,
    ctx: &PolicyContext,
) -> Vec<String> {
    let path_lower = path.to_lowercase();
    let segments: Vec<&str> = path_lower.split('/').collect();

    // Check for sensitive file extensions/names
    for pattern in SENSITIVE_FILE_PATTERNS {
        if path_lower.ends_with(pattern) || segments.iter().any(|seg| seg.starts_with(*pattern)) {
            return policy_private_scope_label(owner, repo, repo_id, ctx);
        }
    }

    // Get filename
    let filename = segments.last().copied().unwrap_or(path_lower.as_str());

    // Check for sensitive keywords in filename
    for keyword in SENSITIVE_FILE_KEYWORDS {
        if filename.contains(keyword) {
            return policy_private_scope_label(owner, repo, repo_id, ctx);
        }
    }

    // Workflow files may contain secrets
    if path_lower.starts_with(".github/workflows/") {
        return policy_private_scope_label(owner, repo, repo_id, ctx);
    }

    default_secrecy
}

#[cfg(test)]
mod tests {
    use super::*;
    use super::super::helpers::PolicyContext;

    fn default_ctx() -> PolicyContext {
        PolicyContext::default()
    }

    fn private_label(owner: &str, repo: &str) -> Vec<String> {
        vec![format!("private:{}/{}", owner, repo)]
    }

    #[test]
    fn check_file_secrecy_env_file_triggers_private() {
        let ctx = default_ctx();
        let result = check_file_secrecy(".env", vec![], "octocat", "hello-world", "octocat/hello-world", &ctx);
        assert_eq!(result, private_label("octocat", "hello-world"));
    }

    #[test]
    fn check_file_secrecy_dotenv_extension_triggers_private() {
        let ctx = default_ctx();
        let result = check_file_secrecy("deploy/config.env", vec![], "octocat", "hello-world", "octocat/hello-world", &ctx);
        assert_eq!(result, private_label("octocat", "hello-world"));
    }

    #[test]
    fn check_file_secrecy_pem_file_triggers_private() {
        let ctx = default_ctx();
        let result = check_file_secrecy("certs/server.pem", vec![], "octocat", "hello-world", "octocat/hello-world", &ctx);
        assert_eq!(result, private_label("octocat", "hello-world"));
    }

    #[test]
    fn check_file_secrecy_rsa_key_triggers_private() {
        let ctx = default_ctx();
        let result = check_file_secrecy(".ssh/id_rsa", vec![], "octocat", "hello-world", "octocat/hello-world", &ctx);
        assert_eq!(result, private_label("octocat", "hello-world"));
    }

    #[test]
    fn check_file_secrecy_workflow_file_triggers_private() {
        let ctx = default_ctx();
        let result = check_file_secrecy(".github/workflows/ci.yml", vec![], "octocat", "hello-world", "octocat/hello-world", &ctx);
        assert_eq!(result, private_label("octocat", "hello-world"));
    }

    #[test]
    fn check_file_secrecy_secrets_json_triggers_private() {
        let ctx = default_ctx();
        let result = check_file_secrecy("config/secrets.json", vec![], "octocat", "hello-world", "octocat/hello-world", &ctx);
        assert_eq!(result, private_label("octocat", "hello-world"));
    }

    #[test]
    fn check_file_secrecy_password_file_triggers_private() {
        let ctx = default_ctx();
        let result = check_file_secrecy("db_password.txt", vec![], "octocat", "hello-world", "octocat/hello-world", &ctx);
        assert_eq!(result, private_label("octocat", "hello-world"));
    }

    #[test]
    fn check_file_secrecy_token_file_triggers_private() {
        let ctx = default_ctx();
        let result = check_file_secrecy("auth_token", vec![], "octocat", "hello-world", "octocat/hello-world", &ctx);
        assert_eq!(result, private_label("octocat", "hello-world"));
    }

    #[test]
    fn check_file_secrecy_normal_source_file_returns_default() {
        let ctx = default_ctx();
        let default = vec!["private:octocat/hello-world".to_string()];
        let result = check_file_secrecy("src/main.rs", default.clone(), "octocat", "hello-world", "octocat/hello-world", &ctx);
        assert_eq!(result, default);
    }

    #[test]
    fn check_file_secrecy_readme_returns_default() {
        let ctx = default_ctx();
        let default = vec!["private:octocat/hello-world".to_string()];
        let result = check_file_secrecy("README.md", default.clone(), "octocat", "hello-world", "octocat/hello-world", &ctx);
        assert_eq!(result, default);
    }

    #[test]
    fn check_file_secrecy_case_insensitive_env() {
        let ctx = default_ctx();
        // .ENV (uppercase) should still match
        let result = check_file_secrecy("config/.ENV", vec![], "octocat", "hello-world", "octocat/hello-world", &ctx);
        assert_eq!(result, private_label("octocat", "hello-world"));
    }

    #[test]
    fn check_file_secrecy_case_insensitive_keyword() {
        let ctx = default_ctx();
        // SECRET (uppercase) in filename should match keyword check
        let result = check_file_secrecy("MY_SECRET_KEY", vec![], "octocat", "hello-world", "octocat/hello-world", &ctx);
        assert_eq!(result, private_label("octocat", "hello-world"));
    }
}
