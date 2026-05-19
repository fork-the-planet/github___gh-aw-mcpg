//! Item-based response labeling (legacy format)
//!
//! This module generates item-based labels by cloning JSON data.
//! This is the **legacy** format that's more memory intensive.
//!
//! **Performance Note**: This module clones JSON data by design.
//! For production use with large datasets, prefer `label_response_paths`
//! which uses zero-copy JSON Pointers (RFC 6901).
//!
//! Use path-based labeling (`label_response_paths`) when possible for better
//! performance with large result sets.

use super::constants::{field_names, label_constants, scope_names};
use super::extract_mcp_response;
use super::helpers::*;
use crate::{LabeledItem, ResourceLabels};
use serde_json::Value;

/// Label individual items in a response (fine-grained labeling)
/// This returns labeled items using the legacy format that works with MCP wrappers
/// Format: {"items": [{"data": <item>, "labels": {...}}, ...]}
pub fn label_response_items(
    tool_name: &str,
    tool_args: &Value,
    response: &Value,
    ctx: &PolicyContext,
) -> Vec<LabeledItem> {
    let mut labeled_items = vec![];

    // Skip labeling for error responses (e.g. 404 Not Found).
    // Resource-level labels from tool_rules handle these cases.
    if response.get("isError").and_then(|v| v.as_bool()) == Some(true) {
        crate::log_info("label_response_items: skipping error response (isError=true)");
        return labeled_items;
    }

    // MCP responses are wrapped in {"content":[{"type":"text","text":"..."}]}
    // Extract the actual response from content[0].text if needed
    let actual_response = extract_mcp_response(response);

    match tool_name {
        // === Repository Search - label private repos with approved-level integrity ===
        "search_repositories" => {
            // Response has items array with repositories
            // Each item has a "private" boolean field from the GitHub API
            if let Some(items) = actual_response.get("items").and_then(|v| v.as_array()) {
                crate::log_info(&format!(
                    "label_response: search_repositories found {} items",
                    items.len()
                ));

                // Limit items to prevent WASM memory exhaustion
                let items_to_process = limit_items_with_log(items, "search_repositories");

                let mut private_count = 0;
                for (i, item) in items_to_process.iter().enumerate() {
                    let is_private = get_bool_or(item, field_names::PRIVATE, false);
                    let full_name = get_str_or(item, field_names::FULL_NAME, "unknown");

                    // Repository metadata has approved-level integrity (endorsed by maintainers)
                    let integrity = writer_integrity(full_name, ctx);

                    if is_private {
                        private_count += 1;
                        crate::log_info(&format!("  [{}] {} is PRIVATE", i, full_name));
                        let secrecy = if let Some((owner, repo)) = full_name.split_once('/') {
                            policy_private_scope_label(owner, repo, full_name, ctx)
                        } else {
                            vec![label_constants::PRIVATE_BASE.to_string()]
                        };
                        labeled_items.push(LabeledItem {
                            data: item.clone(),
                            labels: ResourceLabels {
                                description: format!("repo:{}", full_name),
                                secrecy: secrecy.into(),
                                integrity: integrity.into(),
                            },
                        });
                    } else {
                        // Public repos - explicitly label as public (empty secrecy)
                        labeled_items.push(LabeledItem {
                            data: item.clone(),
                            labels: ResourceLabels {
                                description: format!("repo:{}", full_name),
                                secrecy: vec![].into(),
                                integrity: integrity.into(),
                            },
                        });
                    }
                }
                crate::log_info(&format!(
                    "label_response: {} private repos, {} public repos",
                    private_count,
                    items_to_process.len() - private_count
                ));
            } else {
                crate::log_info("label_response: search_repositories - no items array found");
            }
        }

        // === Pull Requests - label by merged state ===
        "list_pull_requests" | "search_pull_requests" | "pull_request_read" | "get_pull_request" => {
            // For pull_request_read sub-methods that return non-PR objects (e.g.
            // get_check_runs, get_files, get_review_comments, get_reviews,
            // get_comments, get_diff, get_status), skip per-item response labeling.
            // The resource-level labels from tool_rules (which call
            // get_pull_request_facts) provide correct PR-scoped integrity.
            let method = tool_args
                .get("method")
                .and_then(|v| v.as_str())
                .unwrap_or("");
            if tool_name == "pull_request_read" && !method.is_empty() && method != "get" {
                // Fall through — use resource-level labels from tool_rules
            } else {
            // Handle array, {items: [...]}, {pull_requests: [...]}, GraphQL nested, GraphQL single, or REST single object.
            // Work directly with &[Value] slices to avoid allocating a Vec<&Value>.
            let single_item_buf;
            let graphql_single_buf;
            let items: &[Value] = if let Some(arr) = actual_response.as_array() {
                arr.as_slice()
            } else if let Some(items_arr) = actual_response.get("items").and_then(|v| v.as_array()) {
                items_arr.as_slice()
            } else if let Some(items_arr) = actual_response.get("pull_requests").and_then(|v| v.as_array()) {
                items_arr.as_slice()
            } else if let Some(nodes) = extract_graphql_nodes(&actual_response) {
                nodes.as_slice()
            } else if let Some(obj) = extract_graphql_single_object(&actual_response) {
                graphql_single_buf = [obj.clone()];
                &graphql_single_buf
            } else if actual_response.is_object() && !is_graphql_wrapper(&actual_response) && !is_search_result_wrapper(&actual_response) && !is_mcp_text_wrapper(&actual_response) {
                single_item_buf = [actual_response.as_ref().clone()];
                &single_item_buf
            } else {
                &[]
            };

            if !items.is_empty() {
                let items_to_process = limit_items_with_log(items, "list_pull_requests");
                let (arg_owner, arg_repo, arg_repo_full) = extract_repo_scope_with_query_fallback(tool_args);
                let default_repo_private = if !arg_owner.is_empty() && !arg_repo.is_empty() {
                    super::backend::is_repo_private(&arg_owner, &arg_repo).unwrap_or(false)
                } else {
                    false
                };
                let secrecy = if tool_name == "list_pull_requests" || tool_name == "pull_request_read" || tool_name == "get_pull_request" {
                    repo_visibility_secrecy(&arg_owner, &arg_repo, &arg_repo_full, ctx)
                } else {
                    vec![]
                };

                for item in items_to_process.iter() {
                    let number = extract_resource_number(item, "pr", &arg_repo_full);

                    // Get repo info from the PR's base or head, with fallback to
                    // extract_repo_from_item (parses repository_url, html_url, etc.)
                    let base_head_repo = item
                        .get("base")
                        .and_then(|b| b.get("repo"))
                        .and_then(|r| r.get(field_names::FULL_NAME))
                        .and_then(|v| v.as_str())
                        .or_else(|| {
                            item.get("head")
                                .and_then(|h| h.get("repo"))
                                .and_then(|r| r.get(field_names::FULL_NAME))
                                .and_then(|v| v.as_str())
                        })
                        .unwrap_or("");
                    let item_repo_fallback = if base_head_repo.is_empty() {
                        extract_repo_from_item(item)
                    } else {
                        String::new()
                    };
                    let repo_full_name = if !base_head_repo.is_empty() {
                        base_head_repo
                    } else if !item_repo_fallback.is_empty() {
                        &item_repo_fallback
                    } else {
                        &arg_repo_full
                    };
                    let repo_private = repo_visibility_private_for_repo_id(repo_full_name)
                        .unwrap_or(default_repo_private);

                    let is_forked = item
                        .get("base")
                        .and_then(|b| b.get("repo"))
                        .and_then(|r| r.get(field_names::FULL_NAME))
                        .and_then(|v| v.as_str())
                        .zip(
                            item.get("head")
                                .and_then(|h| h.get("repo"))
                                .and_then(|r| r.get(field_names::FULL_NAME))
                                .and_then(|v| v.as_str()),
                        )
                        .map(|(base, head)| !base.eq_ignore_ascii_case(head));

                    let integrity =
                        pr_integrity(item, repo_full_name, repo_private, is_forked, ctx);

                    labeled_items.push(LabeledItem {
                        data: item.clone(),
                        labels: ResourceLabels {
                            description: format!("pr:{}#{}", repo_full_name, number),
                            secrecy: if tool_name == "search_pull_requests" {
                                repo_visibility_secrecy_for_repo_id(repo_full_name, ctx)
                            } else {
                                secrecy.clone()
                            }
                            .into(),
                            integrity: integrity.into(),
                        },
                    });
                }
            }
            } // end else (non-sub-method)
        }

        // === Issues - label by author status ===
        "list_issues" | "search_issues" | "get_issue" | "issue_read" => {
            // For issue_read sub-methods that return non-issue objects (e.g.
            // get_comments, get_sub_issues, get_labels), skip per-item labeling.
            // Resource-level labels from tool_rules provide correct issue-scoped integrity.
            let method = tool_args
                .get("method")
                .and_then(|v| v.as_str())
                .unwrap_or("");
            if tool_name == "issue_read" && !method.is_empty() && method != "get" {
                // Fall through — use resource-level labels from tool_rules
            } else {
            // Handle single issue, array of issues, {issues: [...]}, GraphQL nested, or GraphQL single object
            let all_items: Vec<&Value> = if actual_response.is_array() {
                actual_response
                    .as_array()
                    .map(|arr| arr.iter().collect())
                    .unwrap_or_default()
            } else if let Some(items_arr) = actual_response.get("items").and_then(|v| v.as_array()) {
                items_arr.iter().collect()
            } else if let Some(items_arr) = actual_response.get("issues").and_then(|v| v.as_array()) {
                items_arr.iter().collect()
            } else if let Some(nodes) = extract_graphql_nodes(&actual_response) {
                nodes.iter().collect()
            } else if let Some(obj) = extract_graphql_single_object(&actual_response) {
                vec![obj]
            } else if actual_response.is_object() && !is_graphql_wrapper(&actual_response) && !is_search_result_wrapper(&actual_response) && !is_mcp_text_wrapper(&actual_response) {
                vec![actual_response.as_ref()]
            } else {
                Vec::new()
            };

            // Limit items to prevent WASM memory exhaustion
            let items_limited = limit_items_with_log(all_items.as_slice(), "list_issues");

            // Get owner/repo from tool_args for contributor verification
            let (arg_owner, arg_repo, default_repo_full_name) = extract_repo_scope_with_query_fallback(tool_args);
            let default_repo_private = if !arg_owner.is_empty() && !arg_repo.is_empty() {
                super::backend::is_repo_private(&arg_owner, &arg_repo).unwrap_or(false)
            } else {
                false
            };
            let secrecy = if tool_name == "list_issues" || tool_name == "get_issue" || tool_name == "issue_read" {
                repo_visibility_secrecy(&arg_owner, &arg_repo, &default_repo_full_name, ctx)
            } else {
                vec![]
            };

            for item in items_limited.iter().copied() {
                let item_repo = extract_repo_from_item(item);
                let repo_full_name = if item_repo.is_empty() {
                    default_repo_full_name.clone()
                } else {
                    item_repo
                };

                let repo_private = repo_visibility_private_for_repo_id(&repo_full_name)
                    .unwrap_or(default_repo_private);
                let number = extract_resource_number(item, "issue", &repo_full_name);
                let integrity = issue_integrity(
                    item,
                    &repo_full_name,
                    repo_private,
                    ctx,
                );

                labeled_items.push(LabeledItem {
                    data: item.clone(),
                    labels: ResourceLabels {
                        description: format!("issue:{}#{}", repo_full_name, number),
                        secrecy: if tool_name == "search_issues" {
                            repo_visibility_secrecy_for_repo_id(&repo_full_name, ctx)
                        } else {
                            secrecy.clone()
                        }
                        .into(),
                        integrity: integrity.into(),
                    },
                });
            }
            } // end else (non-sub-method)
        }

        // === File Contents - repo-scoped secrecy ===
        "get_file_contents" => {
            let all_items = collect_items_simple(&actual_response);

            let items_limited = limit_items_with_log(all_items.as_slice(), "get_file_contents");
            let (arg_owner, arg_repo, repo_full_name) = extract_repo_info(tool_args);
            let secrecy = repo_visibility_secrecy(&arg_owner, &arg_repo, &repo_full_name, ctx);
            let branch_ref = tool_args.get("ref").and_then(|v| v.as_str()).unwrap_or("");
            let file_integrity = if is_default_branch_ref(branch_ref) {
                merged_integrity(&repo_full_name, ctx)
            } else {
                writer_integrity(&repo_full_name, ctx)
            };

            for &item in items_limited.iter() {
                labeled_items.push(LabeledItem {
                    data: item.clone(),
                    labels: ResourceLabels {
                        description: format!("file:{}", repo_full_name),
                        secrecy: secrecy.clone().into(),
                        integrity: file_integrity.clone().into(),
                    },
                });
            }
        }

        // === Commits - label by branch (default branch = merged) ===
        "list_commits" | "get_commit" => {
            let all_items = collect_items_simple(&actual_response);

            // Limit items to prevent WASM memory exhaustion
            let items_limited = limit_items_with_log(all_items.as_slice(), "list_commits");

            // Get owner/repo from tool_args
            let (arg_owner, arg_repo, repo_full_name) = extract_repo_info(tool_args);
            let arg_branch = tool_args.get("sha").and_then(|v| v.as_str()).unwrap_or("");
            let secrecy = repo_visibility_secrecy(&arg_owner, &arg_repo, &repo_full_name, ctx);
            let repo_private = if !arg_owner.is_empty() && !arg_repo.is_empty() {
                match super::backend::is_repo_private(&arg_owner, &arg_repo) {
                    Some(value) => value,
                    None => !cfg!(test),
                }
            } else {
                false
            };

            // For get_commit, SHA object identifiers are treated as commit-context
            // requests, which should preserve merged-floor consistency with
            // list_commits-derived SHAs.
            let is_default_branch = is_default_branch_commit_context(tool_name, arg_branch);

            for item in items_limited.iter().copied() {
                let sha = item.get("sha").and_then(|v| v.as_str()).unwrap_or("");
                let short_sha = if sha.len() > 8 { &sha[..8] } else { sha };

                let integrity =
                    commit_integrity(item, &repo_full_name, repo_private, is_default_branch, ctx);

                labeled_items.push(LabeledItem {
                    data: item.clone(),
                    labels: ResourceLabels {
                        description: format!("commit:{}@{}", repo_full_name, short_sha),
                        secrecy: secrecy.clone().into(),
                        integrity: integrity.into(),
                    },
                });
            }
        }

        // === Gists - label by visibility ===
        "list_gists" | "get_gist" => {
            let all_items = collect_items_simple(&actual_response);

            // Limit items to prevent WASM memory exhaustion
            let items_limited = limit_items_with_log(all_items.as_slice(), "list_gists");

            let gist_integrity = reader_integrity(scope_names::USER, ctx);
            for item in items_limited.iter().copied() {
                let is_public = get_bool_or(item, "public", true);
                let id = get_str_or(item, "id", "unknown");

                let secrecy = if is_public {
                    vec![]
                } else {
                    private_user_label()
                };

                // Gists have contributor-level integrity (user content)
                labeled_items.push(LabeledItem {
                    data: item.clone(),
                    labels: ResourceLabels {
                        description: format!("gist:{}", id),
                        secrecy: secrecy.into(),
                        integrity: gist_integrity.clone().into(),
                    },
                });
            }
        }

        // === Notifications - all are private ===
        "list_notifications" | "get_notification_details" => {
            let items = actual_response.as_array().or_else(|| response.as_array());

            if let Some(items) = items {
                let notif_secrecy = private_user_label();
                let notif_integrity = none_integrity("", ctx);
                for item in items.iter() {
                    let id = get_str_or(item, "id", "unknown");
                    labeled_items.push(LabeledItem {
                        data: item.clone(),
                        labels: ResourceLabels {
                            description: format!("notification:{}", id),
                            secrecy: notif_secrecy.clone().into(),
                            integrity: notif_integrity.clone().into(),
                        },
                    });
                }
            }
        }

        // === Releases - merged-level integrity (endorsed) ===
        "list_releases" | "get_latest_release" | "get_release_by_tag" => {
            let all_items = collect_items_simple(&actual_response);

            // Limit items to prevent WASM memory exhaustion
            let items_limited = limit_items_with_log(all_items.as_slice(), "list_releases");

            let (arg_owner, arg_repo, repo_full_name) = extract_repo_info(tool_args);
            let secrecy = repo_visibility_secrecy(&arg_owner, &arg_repo, &repo_full_name, ctx);

            let release_integrity = merged_integrity(&repo_full_name, ctx);
            for item in items_limited.iter().copied() {
                let tag = get_str_or(item, "tag_name", "unknown");

                // Releases have merged-level integrity (endorsed by maintainers)
                labeled_items.push(LabeledItem {
                    data: item.clone(),
                    labels: ResourceLabels {
                        description: format!("release:{}@{}", repo_full_name, tag),
                        secrecy: secrecy.clone().into(),
                        integrity: release_integrity.clone().into(),
                    },
                });
            }
        }

        _ => {}
    }

    labeled_items
}
