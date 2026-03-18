//! Label generation for GitHub resources
//!
//! This module implements the DIFC labeling logic for GitHub resources
//! following the LABELING.md specification.
//!
//! **For detailed documentation on the module structure and design decisions,
//! see [`labels/README.md`](./README.md)**
//!
//! ## Module Organization
//!
//! - `constants` - Label strings, field names, and configuration constants
//! - `helpers` - Common utility functions for labeling logic
//! - `backend` - Backend API calls for contributor verification
//! - `tool_rules` - Tool-specific label rule application
//!
//! ## Integrity Hierarchy
//!
//! Integrity is hierarchical with two levels:
//!   merged > approved > unapproved > (none)
//!
//! Labels:
//! - `merged:<repo>` - Reachable from default branch / merged to project history
//! - `approved:<repo>` - Trusted repository contributor level
//! - `unapproved:<repo>` - Lower trust external contribution level
//! - (none) - Untrusted/external content

use serde_json::Value;

// Sub-modules
pub mod backend;
pub mod constants;
pub mod helpers;
mod response_items;
mod response_paths;
pub mod tool_rules;

// Re-export commonly used items for backward compatibility
pub use constants::MEDIUM_BUFFER_SIZE;

// Re-export helpers - these are part of the public API and used by tests
// The unused_imports warning is suppressed because these are intentionally
// re-exported for external modules and tests, not used within mod.rs itself
#[allow(unused_imports)]
pub use helpers::{
    commit_integrity, ensure_integrity_baseline, extract_items_array, extract_number_as_string,
    extract_repo_from_item, extract_repo_info, extract_repo_info_from_search_query, get_bool_or,
    get_nested_str, get_str_or, is_bot, issue_integrity, limit_items_with_log, make_item_path,
    merged_integrity, none_integrity, pr_integrity, private_scope_label, private_user_label,
    project_github_label, reader_integrity, secret_label, writer_integrity, MinIntegrity,
    PolicyContext, PolicyScopeEntry, ScopeKind,
};

// Re-export response labeling functions (wrappers that pass PolicyContext)
pub fn apply_tool_labels(
    tool_name: &str,
    tool_args: &Value,
    repo_id: &str,
    secrecy: Vec<String>,
    integrity: Vec<String>,
    desc: String,
    ctx: &helpers::PolicyContext,
) -> (Vec<String>, Vec<String>, String) {
    tool_rules::apply_tool_labels(tool_name, tool_args, repo_id, secrecy, integrity, desc, ctx)
}

pub fn label_response_items(
    tool_name: &str,
    tool_args: &Value,
    response: &Value,
    ctx: &helpers::PolicyContext,
) -> Vec<crate::LabeledItem> {
    response_items::label_response_items(tool_name, tool_args, response, ctx)
}

pub fn label_response_paths(
    tool_name: &str,
    tool_args: &Value,
    response: &Value,
    ctx: &helpers::PolicyContext,
) -> Option<response_paths::PathLabelResult> {
    response_paths::label_response_paths(tool_name, tool_args, response, ctx)
}

// ============================================================================
// MCP Response Extraction
// ============================================================================

/// Extract the actual response from MCP wrapper format
/// MCP responses are wrapped in {"content":[{"type":"text","text":"..."}]}
/// where the text field contains stringified JSON
pub(crate) fn extract_mcp_response(response: &Value) -> Value {
    // Log the top-level keys to understand the structure
    if let Some(obj) = response.as_object() {
        let keys: Vec<&str> = obj.keys().map(|s| s.as_str()).collect();
        crate::log_debug(&format!("extract_mcp_response: top-level keys={:?}", keys));
    } else {
        crate::log_debug(&format!(
            "extract_mcp_response: response is not an object, type={}",
            if response.is_array() {
                "array"
            } else if response.is_string() {
                "string"
            } else if response.is_null() {
                "null"
            } else {
                "other"
            }
        ));
    }

    // Try to extract content[0].text and parse it as JSON
    if let Some(content) = response.get("content").and_then(|v| v.as_array()) {
        crate::log_debug(&format!(
            "extract_mcp_response: found content array with {} items",
            content.len()
        ));
        if let Some(first) = content.first() {
            if let Some(text) = first.get("text").and_then(|v| v.as_str()) {
                crate::log_debug(&format!(
                    "extract_mcp_response: found text field, len={}",
                    text.len()
                ));
                // Try to parse the text as JSON
                if let Ok(parsed) = serde_json::from_str::<Value>(text) {
                    crate::log_debug("extract_mcp_response: parsed content[0].text as JSON");
                    return parsed;
                } else {
                    crate::log_debug("extract_mcp_response: failed to parse text as JSON");
                }
            } else {
                crate::log_debug("extract_mcp_response: no text field in content[0]");
            }
        }
    } else {
        crate::log_debug("extract_mcp_response: no content array found");
    }

    // If we can't extract from MCP wrapper, return the original response
    // (it might already be unwrapped or in a different format)
    crate::log_debug("extract_mcp_response: using response as-is");
    response.clone()
}

// ============================================================================
// Tests
// ============================================================================

#[cfg(test)]
mod tests {
    use super::*;
    use crate::labels::constants::label_constants;
    use serde_json::json;

    fn default_ctx() -> PolicyContext {
        PolicyContext::default()
    }

    #[test]
    fn test_reader_integrity() {
        let ctx = default_ctx();
        let scope = "github/copilot";
        assert_eq!(
            reader_integrity(scope, &ctx),
            vec![
                format!("{}{}", label_constants::NONE_PREFIX, scope),
                format!("{}{}", label_constants::READER_PREFIX, scope)
            ]
        );
    }

    #[test]
    fn test_writer_integrity() {
        let ctx = default_ctx();
        let scope = "github/copilot";
        let labels = writer_integrity(scope, &ctx);
        assert_eq!(labels.len(), 3);
        assert!(labels.contains(&format!("{}{}", label_constants::NONE_PREFIX, scope)));
        assert!(labels.contains(&format!("{}{}", label_constants::READER_PREFIX, scope)));
        assert!(labels.contains(&format!("{}{}", label_constants::WRITER_PREFIX, scope)));
    }

    #[test]
    fn test_merged_integrity() {
        let ctx = default_ctx();
        let scope = "github/copilot";
        let labels = merged_integrity(scope, &ctx);
        assert_eq!(labels.len(), 4);
        assert!(labels.contains(&format!("{}{}", label_constants::NONE_PREFIX, scope)));
        assert!(labels.contains(&format!("{}{}", label_constants::READER_PREFIX, scope)));
        assert!(labels.contains(&format!("{}{}", label_constants::WRITER_PREFIX, scope)));
        assert!(labels.contains(&format!("{}{}", label_constants::MERGED_PREFIX, scope)));
    }

    #[test]
    fn test_empty_scope_integrity() {
        let ctx = default_ctx();
        assert_eq!(
            reader_integrity("", &ctx),
            vec![
                label_constants::NONE.to_string(),
                label_constants::READER_BASE.to_string()
            ]
        );
        assert_eq!(
            writer_integrity("", &ctx),
            vec![
                label_constants::NONE.to_string(),
                label_constants::READER_BASE.to_string(),
                label_constants::WRITER_BASE.to_string()
            ]
        );
    }

    #[test]
    fn test_label_helpers() {
        let ctx = default_ctx();
        assert_eq!(secret_label(), vec!["secret".to_string()]);
        assert_eq!(private_user_label(), vec!["private:user".to_string()]);
        assert_eq!(
            project_github_label(&ctx),
            vec![
                format!("{}github", label_constants::NONE_PREFIX),
                format!("{}github", label_constants::READER_PREFIX),
                format!("{}github", label_constants::WRITER_PREFIX)
            ]
        );
        assert_eq!(
            private_scope_label("repo"),
            vec!["private:repo".to_string()]
        );
        assert_eq!(private_scope_label(""), Vec::<String>::new());
    }

    #[test]
    fn test_label_helpers_return_owned_data() {
        let label1 = secret_label();
        let label2 = secret_label();
        // Each call returns independent data
        assert_eq!(label1, label2);
        assert_ne!(label1.as_ptr(), label2.as_ptr());
    }

    #[test]
    fn test_repo_prefix_scope_integrity_label_shaping() {
        let ctx = PolicyContext {
            scopes: vec![PolicyScopeEntry {
                scope_kind: ScopeKind::RepoPrefix,
                scope_owner: Some("lpcox".to_string()),
                scope_repo: Some("github-".to_string()),
                scope_label: "lpcox/github-*".to_string(),
            }],
        };

        let in_scope = writer_integrity("lpcox/github-guard", &ctx);
        assert_eq!(
            in_scope,
            vec![
                "none:lpcox/github-*".to_string(),
                "unapproved:lpcox/github-*".to_string(),
                "approved:lpcox/github-*".to_string(),
            ]
        );

        let out_of_scope = writer_integrity("lpcox/website", &ctx);
        assert_eq!(
            out_of_scope,
            vec![
                "none:lpcox/website".to_string(),
                "unapproved:lpcox/website".to_string(),
                "approved:lpcox/website".to_string(),
            ]
        );
    }

    #[test]
    fn test_repo_prefix_scope_private_label_shaping() {
        let ctx = PolicyContext {
            scopes: vec![PolicyScopeEntry {
                scope_kind: ScopeKind::RepoPrefix,
                scope_owner: Some("lpcox".to_string()),
                scope_repo: Some("github-".to_string()),
                scope_label: "lpcox/github-*".to_string(),
            }],
        };

        assert_eq!(
            super::helpers::policy_private_scope_label(
                "lpcox",
                "github-guard",
                "lpcox/github-guard",
                &ctx
            ),
            vec!["private:lpcox/github-*".to_string()]
        );

        assert_eq!(
            super::helpers::policy_private_scope_label("lpcox", "website", "lpcox/website", &ctx),
            vec!["private:lpcox/website".to_string()]
        );
    }

    #[test]
    fn test_multi_scope_integrity_token_shaping() {
        let ctx = PolicyContext {
            scopes: vec![
                PolicyScopeEntry {
                    scope_kind: ScopeKind::Owner,
                    scope_owner: Some("lpcox".to_string()),
                    scope_repo: None,
                    scope_label: "lpcox".to_string(),
                },
                PolicyScopeEntry {
                    scope_kind: ScopeKind::RepoPrefix,
                    scope_owner: Some("lpcox".to_string()),
                    scope_repo: Some("git".to_string()),
                    scope_label: "lpcox/git*".to_string(),
                },
            ],
        };

        assert_eq!(
            reader_integrity("lpcox/git-helper", &ctx),
            vec![
                "integrity=none;scopes=lpcox,lpcox/git*".to_string(),
                "integrity=unapproved;scopes=lpcox,lpcox/git*".to_string(),
            ]
        );

        assert_eq!(
            reader_integrity("octocat/Hello-World", &ctx),
            vec![
                "none:octocat/Hello-World".to_string(),
                "unapproved:octocat/Hello-World".to_string(),
            ]
        );
    }

    #[test]
    fn test_public_scope_integrity_token_shaping() {
        let ctx = PolicyContext {
            scopes: vec![PolicyScopeEntry {
                scope_kind: ScopeKind::Public,
                scope_owner: None,
                scope_repo: None,
                scope_label: "public".to_string(),
            }],
        };

        assert_eq!(
            writer_integrity("octocat/Hello-World", &ctx),
            vec![
                "none:public".to_string(),
                "unapproved:public".to_string(),
                "approved:public".to_string(),
            ]
        );

        assert_eq!(
            writer_integrity("lpcox/github-guard", &ctx),
            vec![
                "none:public".to_string(),
                "unapproved:public".to_string(),
                "approved:public".to_string(),
            ]
        );
    }

    #[test]
    fn test_extract_items_array_root_array() {
        let response = json!([{"id": 1}, {"id": 2}]);
        let (items, path) = extract_items_array(&response);
        assert!(items.is_some());
        assert_eq!(items.unwrap().len(), 2);
        assert_eq!(path, "");
    }

    #[test]
    fn test_extract_items_array_items_field() {
        let response = json!({"items": [{"id": 1}, {"id": 2}]});
        let (items, path) = extract_items_array(&response);
        assert!(items.is_some());
        assert_eq!(items.unwrap().len(), 2);
        assert_eq!(path, "/items");
    }

    #[test]
    fn test_extract_items_array_issues_field() {
        let response = json!({"issues": [{"id": 1}, {"id": 2}]});
        let (items, path) = extract_items_array(&response);
        assert!(items.is_some());
        assert_eq!(items.unwrap().len(), 2);
        assert_eq!(path, "/issues");
    }

    #[test]
    fn test_extract_items_array_no_array() {
        let response = json!({"data": {"id": 1}});
        let (items, _path) = extract_items_array(&response);
        assert!(items.is_none());
    }

    #[test]
    fn test_extract_items_array_empty_array() {
        let response = json!([]);
        let (items, path) = extract_items_array(&response);
        assert!(items.is_some());
        assert_eq!(items.unwrap().len(), 0);
        assert_eq!(path, "");
    }

    #[test]
    fn test_make_item_path() {
        assert_eq!(make_item_path("", 0), "/0");
        assert_eq!(make_item_path("", 5), "/5");
        assert_eq!(make_item_path("/items", 0), "/items/0");
        assert_eq!(make_item_path("/items", 10), "/items/10");
    }

    #[test]
    fn test_extract_number_as_string() {
        let args = json!({
            "str_num": "123",
            "i64_num": 456,
            "u64_num": 789u64,
            "not_a_num": true
        });

        assert_eq!(
            extract_number_as_string(&args, "str_num"),
            Some("123".to_string())
        );
        assert_eq!(
            extract_number_as_string(&args, "i64_num"),
            Some("456".to_string())
        );
        assert_eq!(
            extract_number_as_string(&args, "u64_num"),
            Some("789".to_string())
        );
        assert_eq!(extract_number_as_string(&args, "not_a_num"), None);
        assert_eq!(extract_number_as_string(&args, "missing"), None);
    }

    #[test]
    fn test_apply_tool_labels_get_file_contents() {
        let ctx = default_ctx();
        let tool_args = json!({
            "owner": "github",
            "repo": "copilot",
            "path": "README.md"
        });

        let (secrecy, integrity, desc) = apply_tool_labels(
            "get_file_contents",
            &tool_args,
            "github/copilot",
            vec![],
            vec![],
            String::new(),
            &ctx,
        );

        assert_eq!(secrecy, vec![] as Vec<String>);
        assert_eq!(integrity, merged_integrity("github/copilot", &ctx));
        assert_eq!(desc, "");
    }

    #[test]
    fn test_apply_tool_labels_get_file_contents_secret() {
        let ctx = default_ctx();
        let tool_args = json!({
            "owner": "github",
            "repo": "copilot",
            "path": ".env"
        });

        let (secrecy, integrity, _desc) = apply_tool_labels(
            "get_file_contents",
            &tool_args,
            "github/copilot",
            vec![],
            vec![],
            String::new(),
            &ctx,
        );

        assert_eq!(secrecy, secret_label());
        assert_eq!(integrity, merged_integrity("github/copilot", &ctx));
    }

    #[test]
    fn test_apply_tool_labels_workflow_secret() {
        let ctx = default_ctx();
        let tool_args = json!({
            "owner": "github",
            "repo": "copilot",
            "path": ".github/workflows/ci.yml"
        });

        let (secrecy, integrity, _desc) = apply_tool_labels(
            "get_file_contents",
            &tool_args,
            "github/copilot",
            vec![],
            vec![],
            String::new(),
            &ctx,
        );

        assert_eq!(secrecy, secret_label());
        assert_eq!(integrity, merged_integrity("github/copilot", &ctx));
    }

    #[test]
    fn test_apply_tool_labels_search_code() {
        let ctx = default_ctx();
        let tool_args = json!({
            "query": "auth repo:github/copilot"
        });

        let (secrecy, integrity, _desc) = apply_tool_labels(
            "search_code",
            &tool_args,
            "github/copilot",
            vec![],
            vec![],
            String::new(),
            &ctx,
        );

        assert_eq!(secrecy, vec![] as Vec<String>);
        assert!(
            integrity == writer_integrity("github/copilot", &ctx)
                || integrity == none_integrity("github/copilot", &ctx)
        );
    }

    #[test]
    fn test_issue_desc_number_formatting() {
        let ctx = default_ctx();
        // Test that issue numbers are formatted correctly from different types
        let tool_args_str = json!({
            "owner": "github",
            "repo": "copilot",
            "issue_number": "123"
        });
        let (_s1, _i1, desc1) = apply_tool_labels(
            "get_issue",
            &tool_args_str,
            "github/copilot",
            vec![],
            vec![],
            String::new(),
            &ctx,
        );
        assert_eq!(desc1, "issue:github/copilot#123");

        let tool_args_i64 = json!({
            "owner": "github",
            "repo": "copilot",
            "issue_number": 456
        });
        let (_s2, _i2, desc2) = apply_tool_labels(
            "get_issue",
            &tool_args_i64,
            "github/copilot",
            vec![],
            vec![],
            String::new(),
            &ctx,
        );
        assert_eq!(desc2, "issue:github/copilot#456");
    }

    #[test]
    fn test_apply_tool_labels_list_issues_repo_scoped_integrity() {
        let ctx = default_ctx();
        let tool_args = json!({
            "owner": "github",
            "repo": "copilot",
            "perPage": 5
        });

        let (_secrecy, integrity, _desc) = apply_tool_labels(
            "list_issues",
            &tool_args,
            "github/copilot",
            vec![],
            vec![],
            String::new(),
            &ctx,
        );

        assert!(
            integrity == writer_integrity("github/copilot", &ctx)
                || integrity == none_integrity("github/copilot", &ctx)
        );
    }

    #[test]
    fn test_apply_tool_labels_list_pull_requests_repo_scoped_integrity() {
        let ctx = default_ctx();
        let tool_args = json!({
            "owner": "github",
            "repo": "copilot",
            "state": "all"
        });

        let (_secrecy, integrity, _desc) = apply_tool_labels(
            "list_pull_requests",
            &tool_args,
            "github/copilot",
            vec![],
            vec![],
            String::new(),
            &ctx,
        );

        assert!(
            integrity == writer_integrity("github/copilot", &ctx)
                || integrity == none_integrity("github/copilot", &ctx)
        );
    }

    #[test]
    fn test_get_commit_sha_uses_commit_context_integrity() {
        let ctx = default_ctx();
        let tool_args = json!({
            "owner": "lpcox",
            "repo": "github-guard",
            "sha": "a590b228c2e258907f503759c31c75bbfcd78a36"
        });

        let (_secrecy, integrity, _desc) = apply_tool_labels(
            "get_commit",
            &tool_args,
            "lpcox/github-guard",
            vec![],
            vec![],
            String::new(),
            &ctx,
        );

        assert_eq!(integrity, merged_integrity("lpcox/github-guard", &ctx));
    }

    #[test]
    fn test_get_commit_response_sha_preserves_merged_floor() {
        let ctx = default_ctx();
        let tool_args = json!({
            "owner": "lpcox",
            "repo": "github-guard",
            "sha": "a590b228c2e258907f503759c31c75bbfcd78a36"
        });

        let response = json!({
            "content": [{
                "type": "text",
                "text": "{\"sha\":\"a590b228c2e258907f503759c31c75bbfcd78a36\",\"author_association\":\"CONTRIBUTOR\"}"
            }]
        });

        let labeled = label_response_items("get_commit", &tool_args, &response, &ctx);
        assert_eq!(labeled.len(), 1);
        assert!(labeled[0]
            .labels
            .integrity
            .contains(&"merged:lpcox/github-guard".to_string()));
    }

    #[test]
    fn test_default_branch_commit_context_helper() {
        assert!(super::helpers::is_default_branch_commit_context(
            "get_commit",
            "a590b228c2e258907f503759c31c75bbfcd78a36"
        ));
        assert!(!super::helpers::is_default_branch_commit_context(
            "list_commits",
            "a590b228c2e258907f503759c31c75bbfcd78a36"
        ));
    }

    #[test]
    fn test_pr_integrity() {
        let ctx = default_ctx();
        let repo = "github/copilot";

        // Merged PR gets merged integrity
        let merged_pr = json!({
            "merged_at": "2024-01-15T10:00:00Z",
            "user": {"login": "contributor"}
        });
        assert_eq!(
            pr_integrity(&merged_pr, repo, false, Some(false), &ctx),
            merged_integrity(repo, &ctx)
        );

        // Public forked PR gets unapproved integrity
        let forked_pr = json!({
            "merged": false,
            "user": {"login": "external"}
        });
        assert_eq!(
            pr_integrity(&forked_pr, repo, false, Some(true), &ctx),
            reader_integrity(repo, &ctx)
        );

        // Public direct PR gets approved integrity
        let direct_pr = json!({
            "merged": false,
            "user": {"login": "contributor"}
        });
        assert_eq!(
            pr_integrity(&direct_pr, repo, false, Some(false), &ctx),
            writer_integrity(repo, &ctx)
        );

        // Private repo PR gets approved integrity
        let private_pr = json!({
            "user": {"login": "someone"}
        });
        assert_eq!(
            pr_integrity(&private_pr, repo, true, Some(true), &ctx),
            writer_integrity(repo, &ctx)
        );
    }

    #[test]
    fn test_issue_integrity() {
        let ctx = default_ctx();
        let repo = "github/copilot";
        let owner = "github";
        let repo_name = "copilot";

        // Private repo issues get approved integrity
        let bot_issue = json!({
            "user": {"login": "dependabot[bot]"}
        });
        assert_eq!(
            issue_integrity(&bot_issue, repo, owner, repo_name, true, &ctx),
            writer_integrity(repo, &ctx)
        );

        // Public repo issues get none baseline integrity
        let owner_issue = json!({
            "user": {"login": "github"}
        });
        assert_eq!(
            issue_integrity(&owner_issue, repo, owner, repo_name, false, &ctx),
            none_integrity(repo, &ctx)
        );

        // Test empty owner/repo
        let issue = json!({
            "user": {"login": "someone"}
        });
        assert_eq!(
            issue_integrity(&issue, "", "", "", false, &ctx),
            none_integrity("", &ctx)
        );

        // Public issue with OWNER association retains approved floor
        let owner_assoc_issue = json!({"author_association": "OWNER"});
        assert_eq!(
            issue_integrity(&owner_assoc_issue, repo, owner, repo_name, false, &ctx),
            writer_integrity(repo, &ctx)
        );

        // Public issue with CONTRIBUTOR association gets unapproved floor
        let contributor_assoc_issue = json!({"author_association": "CONTRIBUTOR"});
        assert_eq!(
            issue_integrity(
                &contributor_assoc_issue,
                repo,
                owner,
                repo_name,
                false,
                &ctx
            ),
            reader_integrity(repo, &ctx)
        );
    }

    #[test]
    fn test_pr_integrity_no_downgrade_for_writer_floor() {
        let ctx = default_ctx();
        let repo = "github/copilot";
        let pr = json!({
            "author_association": "OWNER",
            "merged": false
        });

        // Fork lineage cannot downgrade approved floor
        assert_eq!(
            pr_integrity(&pr, repo, false, Some(true), &ctx),
            writer_integrity(repo, &ctx)
        );
    }

    #[test]
    fn test_commit_integrity_floor_and_elevation() {
        let ctx = default_ctx();
        let repo = "github/copilot";

        let contributor_commit = json!({"author_association": "CONTRIBUTOR"});
        assert_eq!(
            commit_integrity(&contributor_commit, repo, false, false, &ctx),
            reader_integrity(repo, &ctx)
        );

        // Default-branch evidence elevates to merged
        assert_eq!(
            commit_integrity(&contributor_commit, repo, false, true, &ctx),
            merged_integrity(repo, &ctx)
        );

        let unknown_commit = json!({"author_association": "NONE"});
        assert_eq!(
            commit_integrity(&unknown_commit, repo, false, false, &ctx),
            none_integrity(repo, &ctx)
        );
        assert_eq!(
            commit_integrity(&unknown_commit, repo, true, false, &ctx),
            writer_integrity(repo, &ctx)
        );
    }

    #[test]
    fn test_get_str_or() {
        let value = json!({"name": "Alice", "count": 42});
        assert_eq!(get_str_or(&value, "name", "default"), "Alice");
        assert_eq!(get_str_or(&value, "missing", "default"), "default");
        // Non-string field returns default
        assert_eq!(get_str_or(&value, "count", "default"), "default");
    }

    #[test]
    fn test_get_nested_str() {
        let value = json!({"user": {"login": "alice"}, "other": 42});
        assert_eq!(get_nested_str(&value, "user", "login"), "alice");
        assert_eq!(get_nested_str(&value, "missing", "login"), "");
        assert_eq!(get_nested_str(&value, "user", "missing"), "");
        // Non-string inner field returns ""
        assert_eq!(get_nested_str(&value, "other", "login"), "");
    }

    #[test]
    fn test_get_bool_or() {
        let value = json!({"flag": true, "other": false, "count": 42});
        assert!(get_bool_or(&value, "flag", false));
        assert!(!get_bool_or(&value, "other", true));
        assert_eq!(get_bool_or(&value, "missing", true), true);
        // Non-bool field returns default
        assert_eq!(get_bool_or(&value, "count", false), false);
    }

    #[test]
    fn test_extract_repo_from_item_repository_url() {
        let item = json!({
            "number": 42,
            "repository_url": "https://api.github.com/repos/lpcox/github-guard"
        });

        assert_eq!(extract_repo_from_item(&item), "lpcox/github-guard");
    }

    #[test]
    fn test_label_response_paths_search_issues_uses_repository_url_scope() {
        let ctx = default_ctx();
        let tool_args = json!({});
        let response = json!({
            "items": [
                {
                    "number": 123,
                    "repository_url": "https://api.github.com/repos/lpcox/github-guard"
                }
            ]
        });

        let labeled = label_response_paths("search_issues", &tool_args, &response, &ctx)
            .expect("search_issues should produce path labels");

        assert_eq!(labeled.labeled_paths.len(), 1);
        assert_eq!(
            labeled.labeled_paths[0].labels.description,
            "issue:lpcox/github-guard#123"
        );
    }

    #[test]
    fn test_limit_items_with_log() {
        let items: Vec<i32> = (0..150).collect();
        // Exceeding limit returns 100 items
        let limited = limit_items_with_log(&items, "test_tool");
        assert_eq!(limited.len(), 100);
        // Under limit returns all items
        let small: Vec<i32> = (0..50).collect();
        let not_limited = limit_items_with_log(&small, "test_tool");
        assert_eq!(not_limited.len(), 50);
        // Exactly at limit
        let exact: Vec<i32> = (0..100).collect();
        let exact_limited = limit_items_with_log(&exact, "test_tool");
        assert_eq!(exact_limited.len(), 100);
    }

    // -------------------------------------------------------------------------
    // GitHub Projects tools — scope alignment (Bug 1) and integrity level (Bug 2)
    // -------------------------------------------------------------------------

    fn owner_scoped_ctx(owner: &str) -> PolicyContext {
        PolicyContext {
            scopes: vec![PolicyScopeEntry {
                scope_kind: ScopeKind::Owner,
                scope_owner: Some(owner.to_string()),
                scope_repo: None,
                scope_label: owner.to_string(),
            }],
        }
    }

    #[test]
    fn test_apply_tool_labels_list_projects_unscoped_ctx() {
        // With default (unscoped) context, owner is still used as scope token.
        let ctx = default_ctx();
        let tool_args = json!({ "owner": "github" });

        let (_secrecy, integrity, _desc) = apply_tool_labels(
            "list_projects",
            &tool_args,
            "",
            vec![],
            vec![],
            String::new(),
            &ctx,
        );

        // Baseline is approved-level writer_integrity("github", &ctx) → "approved:github"
        // (normalize_scope falls through to the raw owner when no policy token)
        assert_eq!(integrity, writer_integrity("github", &ctx));
    }

    #[test]
    fn test_apply_tool_labels_list_projects_owner_scoped_ctx() {
        // With repos: ["github/*"] the scope token is "github".
        // The resource must carry "none:github" not bare "none" (Bug 1 fix).
        let ctx = owner_scoped_ctx("github");
        let tool_args = json!({ "owner": "github" });

        let (_secrecy, integrity, _desc) = apply_tool_labels(
            "list_projects",
            &tool_args,
            "",
            vec![],
            vec![],
            String::new(),
            &ctx,
        );

        // All integrity labels should be scoped to "github"
        assert!(
            integrity.iter().all(|l| l.ends_with(":github") || l.contains("github")),
            "Expected all labels scoped to 'github', got: {:?}",
            integrity
        );
        // Approved level must be present (Bug 2 fix)
        assert!(
            integrity.contains(&"approved:github".to_string()),
            "Expected 'approved:github' in {:?}",
            integrity
        );
    }

    #[test]
    fn test_apply_tool_labels_get_project_integrity() {
        let ctx = owner_scoped_ctx("myorg");
        let tool_args = json!({ "owner": "myorg", "project_number": 1 });

        let (_secrecy, integrity, _desc) = apply_tool_labels(
            "get_project",
            &tool_args,
            "",
            vec![],
            vec![],
            String::new(),
            &ctx,
        );

        assert!(
            integrity.contains(&"approved:myorg".to_string()),
            "Expected 'approved:myorg' in {:?}",
            integrity
        );
    }

    #[test]
    fn test_apply_tool_labels_list_project_fields_integrity() {
        let ctx = owner_scoped_ctx("myorg");
        let tool_args = json!({ "owner": "myorg", "project_number": 1 });

        let (_secrecy, integrity, _desc) = apply_tool_labels(
            "list_project_fields",
            &tool_args,
            "",
            vec![],
            vec![],
            String::new(),
            &ctx,
        );

        assert!(
            integrity.contains(&"approved:myorg".to_string()),
            "Expected 'approved:myorg' in {:?}",
            integrity
        );
    }

    #[test]
    fn test_apply_tool_labels_list_project_items_integrity() {
        let ctx = owner_scoped_ctx("github");
        let tool_args = json!({ "owner": "github", "project_number": 42 });

        let (_secrecy, integrity, _desc) = apply_tool_labels(
            "list_project_items",
            &tool_args,
            "",
            vec![],
            vec![],
            String::new(),
            &ctx,
        );

        assert!(
            integrity.contains(&"approved:github".to_string()),
            "Expected 'approved:github' in {:?}",
            integrity
        );
    }

    #[test]
    fn test_label_response_paths_list_project_items_issue() {
        // ISSUE items: integrity comes from author_association, secrecy from repo visibility
        let ctx = default_ctx();
        let tool_args = json!({ "owner": "github", "project_number": 1 });
        let response = json!({
            "items": [
                {
                    "type": "ISSUE",
                    "content": {
                        "repository_url": "https://api.github.com/repos/github/copilot",
                        "author_association": "MEMBER"
                    }
                }
            ]
        });

        let result = label_response_paths("list_project_items", &tool_args, &response, &ctx)
            .expect("list_project_items should produce path labels");

        assert_eq!(result.labeled_paths.len(), 1);
        let entry = &result.labeled_paths[0];
        assert_eq!(entry.labels.description, "project-item:issue");
        // MEMBER maps to approved integrity
        assert!(
            entry.labels.integrity.iter().any(|l| l.starts_with("approved:")),
            "Expected approved level for MEMBER, got: {:?}",
            entry.labels.integrity
        );
    }

    #[test]
    fn test_label_response_paths_list_project_items_pull_request() {
        let ctx = default_ctx();
        let tool_args = json!({ "owner": "github", "project_number": 1 });
        let response = json!({
            "items": [
                {
                    "type": "PULL_REQUEST",
                    "content": {
                        "repository_url": "https://api.github.com/repos/github/copilot",
                        "author_association": "CONTRIBUTOR"
                    }
                }
            ]
        });

        let result = label_response_paths("list_project_items", &tool_args, &response, &ctx)
            .expect("list_project_items should produce path labels");

        assert_eq!(result.labeled_paths.len(), 1);
        let entry = &result.labeled_paths[0];
        assert_eq!(entry.labels.description, "project-item:pull_request");
        // CONTRIBUTOR maps to unapproved integrity
        assert!(
            entry.labels.integrity.iter().any(|l| l.starts_with("unapproved:")),
            "Expected unapproved level for CONTRIBUTOR, got: {:?}",
            entry.labels.integrity
        );
    }

    #[test]
    fn test_label_response_paths_list_project_items_draft_issue() {
        // DRAFT_ISSUE: no repo context; org-scoped approved integrity
        let ctx = owner_scoped_ctx("github");
        let tool_args = json!({ "owner": "github", "project_number": 1 });
        let response = json!({
            "items": [
                {
                    "type": "DRAFT_ISSUE",
                    "creator": { "login": "some-user" }
                }
            ]
        });

        let result = label_response_paths("list_project_items", &tool_args, &response, &ctx)
            .expect("list_project_items should produce path labels");

        assert_eq!(result.labeled_paths.len(), 1);
        let entry = &result.labeled_paths[0];
        assert_eq!(entry.labels.description, "project-item:draft_issue");
        assert_eq!(entry.labels.secrecy, vec![] as Vec<String>);
        assert!(
            entry.labels.integrity.contains(&"approved:github".to_string()),
            "Expected 'approved:github' for draft issue, got: {:?}",
            entry.labels.integrity
        );
    }

    #[test]
    fn test_label_response_paths_list_project_items_mixed() {
        // Mixed collection: ISSUE + DRAFT_ISSUE
        let ctx = owner_scoped_ctx("github");
        let tool_args = json!({ "owner": "github", "project_number": 1 });
        let response = json!({
            "items": [
                {
                    "type": "ISSUE",
                    "content": {
                        "repository_url": "https://api.github.com/repos/github/copilot",
                        "author_association": "OWNER"
                    }
                },
                {
                    "type": "DRAFT_ISSUE",
                    "creator": { "login": "admin" }
                }
            ]
        });

        let result = label_response_paths("list_project_items", &tool_args, &response, &ctx)
            .expect("list_project_items should produce path labels");

        assert_eq!(result.labeled_paths.len(), 2);
        assert_eq!(result.labeled_paths[0].path, "/items/0");
        assert_eq!(result.labeled_paths[1].path, "/items/1");
        assert_eq!(result.labeled_paths[0].labels.description, "project-item:issue");
        assert_eq!(result.labeled_paths[1].labels.description, "project-item:draft_issue");
    }
}
