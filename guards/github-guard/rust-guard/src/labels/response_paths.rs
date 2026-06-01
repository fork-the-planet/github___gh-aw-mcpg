//! Path-based response labeling
//!
//! This module generates path-based labels using RFC 6901 JSON Pointers.
//! This is the **preferred** format for response labeling as it avoids
//! cloning JSON objects and is more memory efficient.
//!
//! Returns JSON paths like `/items/0`, `/items/1` pointing to labeled objects
//! in the response, rather than cloning the entire data.

use super::constants::{field_names, label_constants, scope_names};
use super::extract_mcp_response;
use super::helpers::*;
use serde_json::Value;
use std::borrow::Cow;

/// Path-based label for response items (RFC 6901 JSON Pointer)
#[derive(Debug, Clone)]
pub struct PathLabelEntry {
    pub path: String,
    pub labels: crate::ResourceLabels,
}

/// Result of path-based labeling
#[derive(Debug)]
pub struct PathLabelResult {
    pub labeled_paths: Vec<PathLabelEntry>,
    pub default_labels: Option<crate::ResourceLabels>,
    pub items_path: Option<&'static str>,
}

/// Generate path-based labels for collection responses (preferred format per GUARD_RESPONSE_LABELING.md)
/// Returns None if the response is not a collection or should use resource labels
/// Returns Some(PathLabelResult) with JSON Pointer paths for collection items
pub fn label_response_paths(
    tool_name: &str,
    tool_args: &Value,
    response: &Value,
    ctx: &PolicyContext,
) -> Option<PathLabelResult> {
    // Skip labeling for error responses (e.g. 404 Not Found).
    // Resource-level labels from tool_rules handle these cases.
    if response.get("isError").and_then(|v| v.as_bool()) == Some(true) {
        crate::log_info("label_response_paths: skipping error response (isError=true)");
        return None;
    }

    // MCP responses are wrapped in {"content":[{"type":"text","text":"..."}]}
    let actual_response = extract_mcp_response(response);

    match tool_name {
        // === Repository Search - label by private/public ===
        "search_repositories" => {
            let (items_opt, items_key) =
                if let Some(arr) = actual_response.get("items").and_then(|v| v.as_array()) {
                    (Some(arr), "items")
                } else if let Some(arr) = actual_response
                    .get("repositories")
                    .and_then(|v| v.as_array())
                {
                    (Some(arr), "repositories")
                } else {
                    (None, "items")
                };
            if let Some(items) = items_opt {
                // Empty search results are server metadata — let lib.rs fallback handle
                if items.is_empty() && is_search_result_wrapper(&actual_response) {
                    return None;
                }
                crate::log_info(&format!(
                    "label_response_paths: search_repositories found {} items",
                    items.len()
                ));

                let limited_items = limit_items_with_log(items, "search_repositories");
                let mut labeled_paths = Vec::with_capacity(limited_items.len());

                for (i, item) in limited_items.iter().enumerate() {
                    let is_private = get_bool_or(item, field_names::PRIVATE, false);
                    let full_name = get_str_or(item, field_names::FULL_NAME, "unknown");
                    let integrity = writer_integrity(full_name, ctx);

                    let secrecy = if is_private {
                        if let Some((owner, repo)) = full_name.split_once('/') {
                            policy_private_scope_label(owner, repo, full_name, ctx)
                        } else {
                            vec![label_constants::PRIVATE_BASE.to_string()]
                        }
                    } else {
                        vec![]
                    };

                    labeled_paths.push(PathLabelEntry {
                        path: format!("/{}/{}", items_key, i),
                        labels: crate::ResourceLabels {
                            description: format!("repo:{}", full_name),
                            secrecy: secrecy.into(),
                            integrity: integrity.into(),
                        },
                    });
                }

                return Some(PathLabelResult {
                    labeled_paths,
                    default_labels: Some(crate::ResourceLabels {
                        description: "repository".to_string(),
                        secrecy: vec![].into(),
                        integrity: none_integrity("", ctx).into(),
                    }),
                    items_path: Some(match items_key {
                        "repositories" => "/repositories",
                        _ => "/items",
                    }),
                });
            }
        }

        // === Pull Requests - label by merged state ===
        "list_pull_requests"
        | "search_pull_requests"
        | "pull_request_read"
        | "get_pull_request" => {
            // Skip per-item labeling for pull_request_read sub-methods that return
            // non-PR objects (e.g. get_check_runs, get_files, get_reviews).
            // Resource-level labels from tool_rules provide correct PR integrity.
            let method = tool_args
                .get("method")
                .and_then(|v| v.as_str())
                .unwrap_or("");
            if tool_name == "pull_request_read" && !method.is_empty() && method != "get" {
                // Fall through — use resource-level labels
            } else {
                let (items, items_path) = extract_items_array(&actual_response);

                if let Some(items) = items {
                    // Empty search results are server metadata — let lib.rs handle
                    // them with properly-scoped writer_integrity via the metadata fallback.
                    if items.is_empty() && is_search_result_wrapper(&actual_response) {
                        return None;
                    }
                    // Try tool_args first, fall back to extracting from first item
                    let (arg_owner, arg_repo, arg_repo_full) =
                        extract_repo_scope_with_query_fallback(tool_args);
                    let default_repo_private = if !arg_owner.is_empty() && !arg_repo.is_empty() {
                        super::backend::is_repo_private(&arg_owner, &arg_repo).unwrap_or(false)
                    } else {
                        false
                    };
                    let default_repo = if !arg_repo_full.is_empty() {
                        arg_repo_full
                    } else if let Some(first) = items.first() {
                        extract_repo_from_item(first)
                    } else {
                        String::new()
                    };
                    let default_secrecy = if tool_name == "list_pull_requests"
                        || tool_name == "pull_request_read"
                        || tool_name == "get_pull_request"
                    {
                        repo_visibility_secrecy(&arg_owner, &arg_repo, &default_repo, ctx)
                    } else {
                        vec![]
                    };
                    let default_secrecy_shared: crate::SharedLabels = default_secrecy.into();

                    let limited_items = limit_items_with_log(items, "list_pull_requests");
                    let mut labeled_paths = Vec::with_capacity(limited_items.len());

                    for (i, item) in limited_items.iter().enumerate() {
                        // Extract repo from each item (may differ for search results)
                        let item_repo = extract_repo_from_item(item);
                        let repo_for_labels = if item_repo.is_empty() {
                            &default_repo
                        } else {
                            &item_repo
                        };

                        let base_repo = item
                            .get("base")
                            .and_then(|b| b.get("repo"))
                            .and_then(|r| r.get(field_names::FULL_NAME))
                            .and_then(|v| v.as_str())
                            .unwrap_or("");
                        let head_repo = item
                            .get("head")
                            .and_then(|h| h.get("repo"))
                            .and_then(|r| r.get(field_names::FULL_NAME))
                            .and_then(|v| v.as_str())
                            .unwrap_or("");
                        let is_forked = if !base_repo.is_empty() && !head_repo.is_empty() {
                            Some(!base_repo.eq_ignore_ascii_case(head_repo))
                        } else {
                            None
                        };

                        let item_repo_private =
                            repo_visibility_private_for_repo_id(repo_for_labels)
                                .unwrap_or(default_repo_private);

                        let pr_number = extract_resource_number(item, "pr", repo_for_labels);
                        let integrity =
                            pr_integrity(item, repo_for_labels, item_repo_private, is_forked, ctx);
                        let path = make_item_path(&items_path, i);

                        labeled_paths.push(PathLabelEntry {
                            path,
                            labels: crate::ResourceLabels {
                                description: format!("pr:{}#{}", repo_for_labels, pr_number),
                                secrecy: if tool_name == "search_pull_requests" {
                                    repo_visibility_secrecy_for_repo_id(repo_for_labels, ctx).into()
                                } else {
                                    default_secrecy_shared.clone()
                                },
                                integrity: integrity.into(),
                            },
                        });
                    }

                    return Some(PathLabelResult {
                        labeled_paths,
                        default_labels: Some(crate::ResourceLabels {
                            description: "pull_request".to_string(),
                            secrecy: default_secrecy_shared.clone(),
                            integrity: if default_repo_private {
                                writer_integrity(&default_repo, ctx)
                            } else {
                                none_integrity(&default_repo, ctx)
                            }
                            .into(),
                        }),
                        items_path: if items_path.is_empty() {
                            None
                        } else {
                            Some(items_path)
                        },
                    });
                }
            } // end else (non-sub-method)
        }

        // === Issues - label by author contributor status ===
        "list_issues" | "search_issues" | "issue_read" | "get_issue" => {
            // Skip per-item labeling for issue_read sub-methods (get_comments,
            // get_sub_issues, get_labels). Resource-level labels from tool_rules apply.
            let method = tool_args
                .get("method")
                .and_then(|v| v.as_str())
                .unwrap_or("");
            if tool_name == "issue_read" && !method.is_empty() && method != "get" {
                // Fall through — use resource-level labels
            } else {
                let (items, items_path) = extract_items_array(&actual_response);

                if let Some(items) = items {
                    // Empty search results are server metadata — let lib.rs handle
                    // them with properly-scoped writer_integrity via the metadata fallback.
                    if items.is_empty() && is_search_result_wrapper(&actual_response) {
                        return None;
                    }
                    // Try tool_args first, fall back to extracting from first item
                    let (arg_owner, arg_repo, arg_repo_full) =
                        extract_repo_scope_with_query_fallback(tool_args);
                    let default_repo_private = if !arg_owner.is_empty() && !arg_repo.is_empty() {
                        super::backend::is_repo_private(&arg_owner, &arg_repo).unwrap_or(false)
                    } else {
                        false
                    };
                    let default_repo = if !arg_repo_full.is_empty() {
                        arg_repo_full
                    } else if let Some(first) = items.first() {
                        extract_repo_from_item(first)
                    } else {
                        String::new()
                    };
                    let default_secrecy = if tool_name == "list_issues"
                        || tool_name == "issue_read"
                        || tool_name == "get_issue"
                    {
                        repo_visibility_secrecy(&arg_owner, &arg_repo, &default_repo, ctx)
                    } else {
                        vec![]
                    };
                    let default_secrecy_shared: crate::SharedLabels = default_secrecy.into();

                    let limited_items = limit_items_with_log(items, "list_issues");
                    let mut labeled_paths = Vec::with_capacity(limited_items.len());

                    for (i, item) in limited_items.iter().enumerate() {
                        // Extract repo from each item (may differ for search results)
                        let item_repo = extract_repo_from_item(item);
                        let repo_for_labels = if item_repo.is_empty() {
                            &default_repo
                        } else {
                            &item_repo
                        };

                        let item_repo_private =
                            repo_visibility_private_for_repo_id(repo_for_labels)
                                .unwrap_or(default_repo_private);

                        let issue_number = extract_resource_number(item, "issue", repo_for_labels);
                        let integrity =
                            issue_integrity(item, repo_for_labels, item_repo_private, ctx);
                        let path = make_item_path(&items_path, i);

                        labeled_paths.push(PathLabelEntry {
                            path,
                            labels: crate::ResourceLabels {
                                description: format!("issue:{}#{}", repo_for_labels, issue_number),
                                secrecy: if tool_name == "search_issues" {
                                    repo_visibility_secrecy_for_repo_id(repo_for_labels, ctx).into()
                                } else {
                                    default_secrecy_shared.clone()
                                },
                                integrity: integrity.into(),
                            },
                        });
                    }

                    return Some(PathLabelResult {
                        labeled_paths,
                        default_labels: Some(crate::ResourceLabels {
                            description: "issue".to_string(),
                            secrecy: default_secrecy_shared.clone(),
                            integrity: if default_repo_private {
                                writer_integrity(&default_repo, ctx)
                            } else {
                                none_integrity(&default_repo, ctx)
                            }
                            .into(),
                        }),
                        items_path: if items_path.is_empty() {
                            None
                        } else {
                            Some(items_path)
                        },
                    });
                }
            } // end else (non-sub-method)
        }

        // === Commits - label by branch ===
        "list_commits" => {
            let items = actual_response.as_array();

            if let Some(items) = items {
                // Try tool_args first, fall back to extracting from first item
                let (arg_owner, arg_repo, arg_repo_full) = extract_repo_info(tool_args);
                let sha = tool_args.get("sha").and_then(|v| v.as_str()).unwrap_or("");
                let default_repo = if !arg_repo_full.is_empty() {
                    arg_repo_full
                } else if let Some(first) = items.first() {
                    extract_repo_from_item(first)
                } else {
                    String::new()
                };
                let default_secrecy =
                    repo_visibility_secrecy(&arg_owner, &arg_repo, &default_repo, ctx);
                let repo_private = if !arg_owner.is_empty() && !arg_repo.is_empty() {
                    match super::backend::is_repo_private(&arg_owner, &arg_repo) {
                        Some(value) => value,
                        None => !cfg!(test),
                    }
                } else {
                    false
                };

                // Commits on default branch (main/master) get merged-level integrity
                let is_default_branch = is_default_branch_ref(sha);
                let default_secrecy_shared: crate::SharedLabels = default_secrecy.clone().into();

                let limited_items = limit_items_with_log(items, "list_commits");
                let mut labeled_paths = Vec::with_capacity(limited_items.len());

                for (i, item) in limited_items.iter().enumerate() {
                    // Extract repo from each item
                    let item_repo = extract_repo_from_item(item);
                    let repo_for_labels = if item_repo.is_empty() {
                        &default_repo
                    } else {
                        &item_repo
                    };

                    let commit_sha = get_str_or(item, "sha", "unknown");
                    let short_sha = &commit_sha[..std::cmp::min(7, commit_sha.len())];

                    let integrity = commit_integrity(
                        item,
                        repo_for_labels,
                        repo_private,
                        is_default_branch,
                        ctx,
                    );

                    labeled_paths.push(PathLabelEntry {
                        path: format!("/{}", i),
                        labels: crate::ResourceLabels {
                            description: format!("commit:{}@{}", repo_for_labels, short_sha),
                            secrecy: default_secrecy_shared.clone(),
                            integrity: integrity.into(),
                        },
                    });
                }

                return Some(PathLabelResult {
                    labeled_paths,
                    default_labels: Some(crate::ResourceLabels {
                        description: "commit".to_string(),
                        secrecy: default_secrecy.into(),
                        integrity: if is_default_branch {
                            merged_integrity(&default_repo, ctx)
                        } else if repo_private {
                            writer_integrity(&default_repo, ctx)
                        } else {
                            vec![]
                        }
                        .into(),
                    }),
                    items_path: None, // Root array
                });
            }
        }

        // === File Contents - repo-scoped secrecy ===
        "get_file_contents" => {
            let (arg_owner, arg_repo, arg_repo_full) = extract_repo_info(tool_args);
            let secrecy = repo_visibility_secrecy(&arg_owner, &arg_repo, &arg_repo_full, ctx);
            let branch_ref = tool_args.get("ref").and_then(|v| v.as_str()).unwrap_or("");
            let file_integrity = if is_default_branch_ref(branch_ref) {
                merged_integrity(&arg_repo_full, ctx)
            } else {
                writer_integrity(&arg_repo_full, ctx)
            };
            let secrecy_shared: crate::SharedLabels = secrecy.clone().into();
            let file_integrity_shared: crate::SharedLabels = file_integrity.clone().into();

            if let Some(items) = actual_response.as_array() {
                let limited_items = limit_items_with_log(items, "get_file_contents");
                let mut labeled_paths = Vec::with_capacity(limited_items.len());

                for (i, _item) in limited_items.iter().enumerate() {
                    labeled_paths.push(PathLabelEntry {
                        path: format!("/{}", i),
                        labels: crate::ResourceLabels {
                            description: format!("file:{}", arg_repo_full),
                            secrecy: secrecy_shared.clone(),
                            integrity: file_integrity_shared.clone(),
                        },
                    });
                }

                return Some(PathLabelResult {
                    labeled_paths,
                    default_labels: Some(crate::ResourceLabels {
                        description: "file_contents".to_string(),
                        secrecy: secrecy.into(),
                        integrity: file_integrity.into(),
                    }),
                    items_path: None,
                });
            }
        }

        // === Releases - merged-level integrity ===
        "list_releases" => {
            let items = actual_response.as_array();

            if let Some(items) = items {
                // Try tool_args first, fall back to extracting from first item
                let (arg_owner, arg_repo, arg_repo_full) = extract_repo_info(tool_args);
                let default_repo = if !arg_repo_full.is_empty() {
                    arg_repo_full
                } else if let Some(first) = items.first() {
                    extract_repo_from_item(first)
                } else {
                    String::new()
                };
                let default_secrecy =
                    repo_visibility_secrecy(&arg_owner, &arg_repo, &default_repo, ctx);
                let default_secrecy_shared: crate::SharedLabels = default_secrecy.clone().into();

                let limited_items = limit_items_with_log(items, "list_releases");
                let mut labeled_paths = Vec::with_capacity(limited_items.len());

                for (i, item) in limited_items.iter().enumerate() {
                    // Extract repo from each item
                    let item_repo = extract_repo_from_item(item);
                    let repo_for_labels = if item_repo.is_empty() {
                        &default_repo
                    } else {
                        &item_repo
                    };

                    let tag = get_str_or(item, "tag_name", "unknown");

                    labeled_paths.push(PathLabelEntry {
                        path: format!("/{}", i),
                        labels: crate::ResourceLabels {
                            description: format!("release:{}@{}", repo_for_labels, tag),
                            secrecy: default_secrecy_shared.clone(),
                            integrity: merged_integrity(repo_for_labels, ctx).into(),
                        },
                    });
                }

                return Some(PathLabelResult {
                    labeled_paths,
                    default_labels: Some(crate::ResourceLabels {
                        description: "release".to_string(),
                        secrecy: default_secrecy.into(),
                        integrity: merged_integrity(&default_repo, ctx).into(),
                    }),
                    items_path: None, // Root array
                });
            }
        }

        // === Notifications - private ===
        "list_notifications" => {
            let items = actual_response.as_array();

            if let Some(items) = items {
                let limited_items = limit_items_with_log(items, "list_notifications");
                let mut labeled_paths = Vec::with_capacity(limited_items.len());
                // Hoist loop-invariant labels: Arc::clone is free.
                let notif_secrecy: crate::SharedLabels = private_user_label().into();
                let empty_integrity: crate::SharedLabels = vec![].into();

                for (i, item) in limited_items.iter().enumerate() {
                    let id = get_str_or(item, "id", "unknown");

                    labeled_paths.push(PathLabelEntry {
                        path: format!("/{}", i),
                        labels: crate::ResourceLabels {
                            description: format!("notification:{}", id),
                            secrecy: notif_secrecy.clone(),
                            integrity: empty_integrity.clone(),
                        },
                    });
                }

                return Some(PathLabelResult {
                    labeled_paths,
                    default_labels: Some(crate::ResourceLabels {
                        description: "notification".to_string(),
                        secrecy: notif_secrecy,
                        integrity: empty_integrity,
                    }),
                    items_path: None, // Root array
                });
            }
        }

        // === Gists - contributor-level ===
        "list_gists" => {
            let items = actual_response.as_array();

            if let Some(items) = items {
                let limited_items = limit_items_with_log(items, "list_gists");
                let mut labeled_paths = Vec::with_capacity(limited_items.len());
                // Hoist loop-invariant labels: Arc::clone is free.
                let gist_integrity: crate::SharedLabels =
                    reader_integrity(scope_names::USER, ctx).into();
                let public_gist_secrecy: crate::SharedLabels = vec![].into();
                let private_gist_secrecy: crate::SharedLabels = private_user_label().into();

                for (i, item) in limited_items.iter().enumerate() {
                    let is_public = get_bool_or(item, "public", true);
                    let id = get_str_or(item, "id", "unknown");

                    labeled_paths.push(PathLabelEntry {
                        path: format!("/{}", i),
                        labels: crate::ResourceLabels {
                            description: format!("gist:{}", id),
                            secrecy: if is_public {
                                public_gist_secrecy.clone()
                            } else {
                                private_gist_secrecy.clone()
                            },
                            integrity: gist_integrity.clone(),
                        },
                    });
                }

                return Some(PathLabelResult {
                    labeled_paths,
                    default_labels: Some(crate::ResourceLabels {
                        description: "gist".to_string(),
                        secrecy: public_gist_secrecy,
                        integrity: gist_integrity,
                    }),
                    items_path: None, // Root array
                });
            }
        }

        // === GitHub Project Items - heterogeneous ISSUE / PULL_REQUEST / DRAFT_ISSUE ===
        // projects_list is the new canonical name (replaces list_project_items)
        "list_project_items" | "projects_list" => {
            let (arg_owner, _, _) = extract_repo_info(tool_args);
            let (items, items_path) = extract_items_array(&actual_response);

            if let Some(items) = items {
                let limited_items = limit_items_with_log(items, "list_project_items");
                let mut labeled_paths = Vec::with_capacity(limited_items.len());

                for (i, item) in limited_items.iter().enumerate() {
                    let item_type = get_str_or(item, "type", "");

                    let (secrecy, integrity) = if matches!(item_type, "ISSUE" | "PULL_REQUEST") {
                        // Issues and PRs carry a `content` sub-object with
                        // `repository_url` (for repo scope) and
                        // `author_association` (for integrity level).
                        let content = item.get("content").unwrap_or(item);
                        let item_repo = extract_repo_from_item(content);
                        let secrecy = if item_repo.is_empty() {
                            // Fail secure: if we cannot determine the repo for this
                            // item, treat it as private within the owner scope rather
                            // than defaulting to public.
                            policy_private_scope_label(&arg_owner, "", "", ctx)
                        } else {
                            repo_visibility_secrecy_for_repo_id(&item_repo, ctx)
                        };
                        let association = get_str_or(content, "author_association", "");
                        let integrity_scope = if item_repo.is_empty() {
                            &arg_owner
                        } else {
                            &item_repo
                        };
                        let integrity = author_association_floor_from_str(
                            integrity_scope,
                            Some(association),
                            ctx,
                        );
                        (secrecy, integrity)
                    } else {
                        // DRAFT_ISSUE or unrecognised type: no underlying repo context.
                        // Use org-scoped approved integrity (adding items to a project
                        // requires org membership, regardless of the creator's identity).
                        let integrity = writer_integrity(&arg_owner, ctx);
                        (vec![], integrity)
                    };

                    labeled_paths.push(PathLabelEntry {
                        path: make_item_path(&items_path, i),
                        labels: crate::ResourceLabels {
                            description: {
                                let type_lower: Cow<'_, str> = match item_type {
                                    "ISSUE" => Cow::Borrowed("issue"),
                                    "PULL_REQUEST" => Cow::Borrowed("pull_request"),
                                    "DRAFT_ISSUE" => Cow::Borrowed("draft_issue"),
                                    other => Cow::Owned(other.to_lowercase()),
                                };
                                format!("project-item:{}", type_lower)
                            },
                            secrecy: secrecy.into(),
                            integrity: integrity.into(),
                        },
                    });
                }

                return Some(PathLabelResult {
                    labeled_paths,
                    default_labels: Some(crate::ResourceLabels {
                        description: "project-item".to_string(),
                        secrecy: vec![].into(),
                        integrity: writer_integrity(&arg_owner, ctx).into(),
                    }),
                    items_path: if items_path.is_empty() {
                        None
                    } else {
                        Some(items_path)
                    },
                });
            }
        }

        _ => {}
    }

    // Not a collection or not supported - return None to use resource labels
    None
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::labels::constants::label_constants;
    use serde_json::json;

    fn ctx() -> PolicyContext {
        PolicyContext::default()
    }

    #[test]
    fn search_repositories_private_gets_secrecy_public_gets_empty() {
        let tool_args = json!({});
        let response = json!({
            "content": [{
                "type": "text",
                "text": serde_json::to_string(&json!({
                    "items": [
                        {"full_name": "octocat/private-repo", "private": true},
                        {"full_name": "octocat/public-repo", "private": false}
                    ]
                }))
                .expect("response should serialize")
            }]
        });

        let result = label_response_paths("search_repositories", &tool_args, &response, &ctx())
            .expect("should produce path labels");

        assert_eq!(result.labeled_paths.len(), 2);

        let private_entry = &result.labeled_paths[0];
        let public_entry = &result.labeled_paths[1];

        assert!(
            !private_entry.labels.secrecy.is_empty(),
            "private repo should have non-empty secrecy"
        );
        assert!(
            public_entry.labels.secrecy.is_empty(),
            "public repo should have empty secrecy"
        );
    }

    #[test]
    fn list_pull_requests_merged_pr_gets_merged_integrity() {
        let tool_args = json!({"owner": "octocat", "repo": "hello-world"});
        let pr = json!({
            "number": 1,
            "merged_at": "2024-01-01T00:00:00Z",
            "base": {"repo": {"full_name": "octocat/hello-world"}},
            "head": {"repo": {"full_name": "octocat/hello-world"}}
        });
        let response = json!({
            "content": [{
                "type": "text",
                "text": serde_json::to_string(&json!([pr])).expect("response should serialize")
            }]
        });

        let result = label_response_paths("list_pull_requests", &tool_args, &response, &ctx())
            .expect("should produce path labels");
        assert_eq!(result.labeled_paths.len(), 1);

        let entry = &result.labeled_paths[0];
        let merged_label = format!("{}octocat/hello-world", label_constants::MERGED_PREFIX);
        assert!(
            entry.labels.integrity.contains(&merged_label),
            "merged PR should have merged integrity; got {:?}",
            entry.labels.integrity
        );
    }

    #[test]
    fn list_pull_requests_item_secrecy_matches_default_labels() {
        let tool_args = json!({"owner": "octocat", "repo": "hello-world"});
        let response = json!({
            "content": [{
                "type": "text",
                "text": serde_json::to_string(&json!([{
                    "number": 1,
                    "base": {"repo": {"full_name": "octocat/hello-world"}},
                    "head": {"repo": {"full_name": "octocat/hello-world"}}
                }]))
                .expect("response should serialize")
            }]
        });

        let result = label_response_paths("list_pull_requests", &tool_args, &response, &ctx())
            .expect("should produce path labels");
        let default_labels = result
            .default_labels
            .as_ref()
            .expect("default_labels should be present");

        assert_eq!(result.labeled_paths.len(), 1);
        assert_eq!(
            result.labeled_paths[0].labels.secrecy,
            default_labels.secrecy
        );
    }

    #[test]
    fn search_issues_uses_repo_qualifier_from_query_scope() {
        let tool_args = json!({"query": "is:issue repo:octocat/hello-world bug"});
        let response = json!({
            "content": [{
                "type": "text",
                "text": serde_json::to_string(&json!({
                    "items": [{"number": 42}]
                }))
                .expect("response should serialize")
            }]
        });

        let result = label_response_paths("search_issues", &tool_args, &response, &ctx())
            .expect("search_issues should produce path labels");

        assert_eq!(result.labeled_paths.len(), 1);
        assert_eq!(
            result.labeled_paths[0].labels.description,
            "issue:octocat/hello-world#42"
        );
    }

    #[test]
    fn list_issues_item_secrecy_matches_default_labels() {
        let tool_args = json!({"owner": "octocat", "repo": "hello-world"});
        let response = json!({
            "content": [{
                "type": "text",
                "text": serde_json::to_string(&json!([{
                    "number": 42,
                    "repository_url": "https://api.github.com/repos/octocat/hello-world"
                }]))
                .expect("response should serialize")
            }]
        });

        let result = label_response_paths("list_issues", &tool_args, &response, &ctx())
            .expect("should produce path labels");
        let default_labels = result
            .default_labels
            .as_ref()
            .expect("default_labels should be present");

        assert_eq!(result.labeled_paths.len(), 1);
        assert_eq!(
            result.labeled_paths[0].labels.secrecy,
            default_labels.secrecy
        );
    }

    #[test]
    fn unknown_tool_returns_none() {
        let result = label_response_paths("unknown_tool", &json!({}), &json!({}), &ctx());
        assert!(
            result.is_none(),
            "unknown tool should produce no path labels"
        );
    }

    // === list_commits tests ===
    // The sha field drives is_default_branch → merged-level integrity, which is a
    // security-relevant decision. A regression here (e.g. treating all commits as
    // default-branch) would over-elevate integrity labels. Both tests are self-contained
    // and require no backend mocking.

    #[test]
    fn list_commits_default_branch_gets_merged_integrity() {
        let tool_args = json!({"owner": "octocat", "repo": "hello-world", "sha": "main"});
        let commit = json!({
            "sha": "abc1234def5678",
            "commit": {"message": "fix: a bug"},
            "author": {"login": "octocat"},
            "author_association": "OWNER"
        });
        let response = json!({
            "content": [{
                "type": "text",
                "text": serde_json::to_string(&json!([commit])).expect("response should serialize")
            }]
        });

        let result = label_response_paths("list_commits", &tool_args, &response, &ctx())
            .expect("list_commits should produce path labels");

        assert_eq!(result.labeled_paths.len(), 1);
        assert_eq!(result.labeled_paths[0].path, "/0");
        assert!(
            result.items_path.is_none(),
            "list_commits root array should have items_path = None, got {:?}",
            result.items_path
        );

        // Default branch (main) → default_labels integrity must include a merged: label
        let default_integrity = &result
            .default_labels
            .as_ref()
            .expect("default_labels should be set")
            .integrity;
        let merged_label = format!("{}octocat/hello-world", label_constants::MERGED_PREFIX);
        assert!(
            default_integrity.contains(&merged_label),
            "default-branch default_labels should have merged-level integrity; got {:?}",
            default_integrity
        );
    }

    #[test]
    fn list_commits_feature_branch_public_repo_has_no_merged_integrity() {
        let tool_args =
            json!({"owner": "octocat", "repo": "hello-world", "sha": "feature/my-branch"});
        let commit = json!({
            "sha": "deadbeef12345678",
            "commit": {"message": "wip: in progress"},
            "author_association": "CONTRIBUTOR"
        });
        let response = json!({
            "content": [{
                "type": "text",
                "text": serde_json::to_string(&json!([commit])).expect("response should serialize")
            }]
        });

        let result = label_response_paths("list_commits", &tool_args, &response, &ctx())
            .expect("list_commits should produce path labels");

        assert_eq!(result.labeled_paths.len(), 1);
        assert!(
            result.items_path.is_none(),
            "list_commits root array should have items_path = None"
        );

        // Non-default branch of public repo (is_repo_private returns None → false in
        // test cfg) → default_labels integrity must NOT contain any merged: label.
        let default_integrity = &result
            .default_labels
            .as_ref()
            .expect("default_labels should be set")
            .integrity;
        assert!(
            !default_integrity
                .iter()
                .any(|l| l.starts_with(label_constants::MERGED_PREFIX)),
            "feature-branch commit on public repo should NOT have merged-level integrity; got {:?}",
            default_integrity
        );
    }
}
