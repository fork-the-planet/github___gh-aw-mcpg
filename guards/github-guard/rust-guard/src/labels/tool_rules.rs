//! Tool-specific label rule application
//!
//! This module contains the `apply_tool_labels` function which applies
//! tool-specific labeling rules based on the tool name and arguments.

use serde_json::Value;

use super::constants::{field_names, SENSITIVE_FILE_KEYWORDS, SENSITIVE_FILE_PATTERNS};
use super::helpers::{
    author_association_floor_from_str, ensure_integrity_baseline, extract_number_as_string,
    extract_repo_info, extract_repo_info_from_search_query, is_default_branch_commit_context,
    is_default_branch_ref, max_integrity, merged_integrity, policy_private_scope_label,
    project_github_label, reader_integrity, secret_label, writer_integrity, PolicyContext,
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
                    let floor = author_association_floor_from_str(
                        repo_id,
                        super::backend::get_issue_author_association(&owner, &repo, &issue_num)
                            .as_deref(),
                        ctx,
                    );
                    integrity = max_integrity(repo_id, integrity, floor, ctx);
                }
            }
        }

        // Search is cross-repo / unknown scope until response labeling
        "search_issues" => {
            integrity = vec![];
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
                        integrity = author_association_floor_from_str(
                            repo_id,
                            facts.author_association.as_deref(),
                            ctx,
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

        // Search is cross-repo / unknown scope until response labeling
        "search_pull_requests" => {
            integrity = vec![];
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
            // S(alert) = secret - alerts reference actual secrets
            // I(alert) = approved - automated detection
            secrecy = secret_label();
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

        // === GitHub Actions ===
        "actions_get" | "actions_list" => {
            // S(workflow/artifact) = inherits from repo secrecy
            // I(workflow/artifact) = approved - maintained by repo team
            secrecy = apply_repo_visibility_secrecy(&owner, &repo, repo_id, secrecy, ctx);
            integrity = writer_integrity(repo_id, ctx);

            // Additional secrecy checks for workflow files
            if tool_name == "actions_get"
                && tool_args.get("method")
                    == Some(&Value::String("download_workflow_run_artifact".to_string()))
            {
                // Artifacts may contain secrets
                secrecy = secret_label();
            }
        }

        // === Content Access ===
        "get_file_contents" => {
            secrecy = apply_repo_visibility_secrecy(&owner, &repo, repo_id, secrecy, ctx);
            // File secrecy based on path patterns
            if let Some(path) = tool_args.get("path").and_then(|v| v.as_str()) {
                secrecy = check_file_secrecy(path, secrecy);
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
            let mut scoped_owner = owner.clone();
            let mut scoped_repo = repo.clone();
            let mut scoped_repo_id = repo_id.to_string();

            if (scoped_owner.is_empty() || scoped_repo.is_empty() || scoped_repo_id.is_empty())
                && tool_args.get("query").and_then(|v| v.as_str()).is_some()
            {
                let query = tool_args
                    .get("query")
                    .and_then(|v| v.as_str())
                    .unwrap_or("");
                let (q_owner, q_repo, q_repo_id) = extract_repo_info_from_search_query(query);
                if !q_repo_id.is_empty() {
                    scoped_owner = q_owner;
                    scoped_repo = q_repo;
                    scoped_repo_id = q_repo_id;
                    baseline_scope = scoped_repo_id.clone();
                    desc = format!("search_code:{}", scoped_repo_id);
                }
            }

            secrecy = apply_repo_visibility_secrecy(
                &scoped_owner,
                &scoped_repo,
                &scoped_repo_id,
                secrecy,
                ctx,
            );
            integrity = writer_integrity(&scoped_repo_id, ctx);
        }

        // === Repository Metadata ===
        "search_repositories" => {
            // Repository metadata has approved-level integrity
            // Secrecy will be determined per-item based on private flag
            integrity = writer_integrity(repo_id, ctx);
        }

        "get_repository" => {
            // Single repository metadata should inherit repository visibility.
            secrecy = apply_repo_visibility_secrecy(&owner, &repo, repo_id, secrecy, ctx);
            integrity = writer_integrity(repo_id, ctx);
        }

        // === Branches & Tags ===
        "list_branches" | "list_tags" | "get_tag" => {
            // Branch/tag metadata from repo
            // S = inherits from repo
            // I = approved (maintained by repo team)
            secrecy = apply_repo_visibility_secrecy(&owner, &repo, repo_id, secrecy, ctx);
            integrity = writer_integrity(repo_id, ctx);
        }

        // === Releases ===
        "list_releases" | "get_latest_release" | "get_release_by_tag" => {
            // Release metadata
            // S = inherits from repo (releases can be private)
            // I = project (created by maintainers)
            secrecy = apply_repo_visibility_secrecy(&owner, &repo, repo_id, secrecy, ctx);
            integrity = writer_integrity(repo_id, ctx);
        }

        // === Labels ===
        "get_label" => {
            // Label metadata
            // S = inherits from repo
            // I = project (managed by maintainers)
            secrecy = apply_repo_visibility_secrecy(&owner, &repo, repo_id, secrecy, ctx);
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
        "list_projects" | "get_project" | "list_project_fields" | "list_project_items" => {
            // Projects are org-scoped; creating/managing projects requires org membership.
            // I = approved:<owner> — equivalent to MEMBER author_association
            // S = empty by default (public project); per-item secrecy for items is refined in
            //     label_response_paths for list_project_items
            if !owner.is_empty() {
                baseline_scope = owner.clone();
                integrity = writer_integrity(&baseline_scope, ctx);
            }
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

/// Check if a file path contains sensitive patterns
/// Returns secret label if sensitive, otherwise returns the default secrecy
fn check_file_secrecy(path: &str, default_secrecy: Vec<String>) -> Vec<String> {
    let path_lower = path.to_lowercase();

    // Check for sensitive file extensions/names
    for pattern in SENSITIVE_FILE_PATTERNS {
        if path_lower.ends_with(pattern) || path_lower.contains(&format!("/{}", pattern)) {
            return secret_label();
        }
    }

    // Get filename
    let filename = path_lower.rsplit('/').next().unwrap_or(&path_lower);

    // Check for sensitive keywords in filename
    for keyword in SENSITIVE_FILE_KEYWORDS {
        if filename.contains(keyword) {
            return secret_label();
        }
    }

    // Workflow files may contain secrets
    if path_lower.starts_with(".github/workflows/") {
        return secret_label();
    }

    default_secrecy
}
