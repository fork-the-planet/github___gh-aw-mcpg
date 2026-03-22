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
    blocked_integrity, commit_integrity, ensure_integrity_baseline, extract_items_array,
    extract_number_as_string, extract_repo_from_item, extract_repo_info,
    extract_repo_info_from_search_query, get_bool_or, get_nested_str, get_str_or,
    is_blocked_user, is_bot, issue_integrity, limit_items_with_log, make_item_path, merged_integrity,
    none_integrity, pr_integrity, private_scope_label, private_user_label, project_github_label,
    reader_integrity, secret_label, writer_integrity, MinIntegrity, PolicyContext, PolicyScopeEntry,
    ScopeKind,
};
#[cfg(test)]
pub use helpers::has_approval_label;

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
            ..Default::default()
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
            ..Default::default()
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
            ..Default::default()
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
            ..Default::default()
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

        // Private repo issues get approved integrity
        let bot_issue = json!({
            "user": {"login": "dependabot[bot]"}
        });
        assert_eq!(
            issue_integrity(&bot_issue, repo, true, &ctx),
            writer_integrity(repo, &ctx)
        );

        // Public repo issues get none baseline integrity
        let owner_issue = json!({
            "user": {"login": "github"}
        });
        assert_eq!(
            issue_integrity(&owner_issue, repo, false, &ctx),
            none_integrity(repo, &ctx)
        );

        // Test empty owner/repo
        let issue = json!({
            "user": {"login": "someone"}
        });
        assert_eq!(
            issue_integrity(&issue, "", false, &ctx),
            none_integrity("", &ctx)
        );

        // Public issue with OWNER association retains approved floor
        let owner_assoc_issue = json!({"author_association": "OWNER"});
        assert_eq!(
            issue_integrity(&owner_assoc_issue, repo, false, &ctx),
            writer_integrity(repo, &ctx)
        );

        // Public issue with CONTRIBUTOR association gets unapproved floor
        let contributor_assoc_issue = json!({"author_association": "CONTRIBUTOR"});
        assert_eq!(
            issue_integrity(&contributor_assoc_issue, repo, false, &ctx),
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
    fn test_trusted_first_party_bot_detection() {
        use super::helpers::is_trusted_first_party_bot;

        // Trusted first-party bots
        assert!(is_trusted_first_party_bot("dependabot[bot]"));
        assert!(is_trusted_first_party_bot("github-actions[bot]"));
        assert!(is_trusted_first_party_bot("github-merge-queue[bot]"));
        assert!(is_trusted_first_party_bot("copilot"));

        // Case-insensitive
        assert!(is_trusted_first_party_bot("Dependabot[bot]"));
        assert!(is_trusted_first_party_bot("GitHub-Actions[bot]"));
        assert!(is_trusted_first_party_bot("Copilot"));

        // Not trusted (third-party bots)
        assert!(!is_trusted_first_party_bot("renovate[bot]"));
        assert!(!is_trusted_first_party_bot("renovate-bot"));
        assert!(!is_trusted_first_party_bot("codecov[bot]"));
        assert!(!is_trusted_first_party_bot("snyk-bot"));

        // Not bots
        assert!(!is_trusted_first_party_bot("octocat"));
        assert!(!is_trusted_first_party_bot("dependabot"));
        assert!(is_trusted_first_party_bot("github-actions"));
        assert!(is_trusted_first_party_bot("app/github-actions"));
        assert!(!is_trusted_first_party_bot(""));
    }

    #[test]
    fn test_trusted_bot_issue_integrity_public_repo() {
        let ctx = default_ctx();
        let repo = "github/copilot";

        // Trusted bot issue on public repo gets approved (writer) integrity
        // even though author_association is NONE
        let dependabot_issue = json!({
            "user": {"login": "dependabot[bot]"},
            "author_association": "NONE"
        });
        assert_eq!(
            issue_integrity(&dependabot_issue, repo, false, &ctx),
            writer_integrity(repo, &ctx)
        );

        let actions_issue = json!({
            "user": {"login": "github-actions[bot]"},
            "author_association": "NONE"
        });
        assert_eq!(
            issue_integrity(&actions_issue, repo, false, &ctx),
            writer_integrity(repo, &ctx)
        );

        let merge_queue_issue = json!({
            "user": {"login": "github-merge-queue[bot]"}
        });
        assert_eq!(
            issue_integrity(&merge_queue_issue, repo, false, &ctx),
            writer_integrity(repo, &ctx)
        );

        let copilot_issue = json!({
            "user": {"login": "copilot"},
            "author_association": "NONE"
        });
        assert_eq!(
            issue_integrity(&copilot_issue, repo, false, &ctx),
            writer_integrity(repo, &ctx)
        );

        // github-actions without [bot] suffix (as returned by some APIs)
        let actions_no_bot_issue = json!({
            "user": {"login": "github-actions"},
            "author_association": "NONE"
        });
        assert_eq!(
            issue_integrity(&actions_no_bot_issue, repo, false, &ctx),
            writer_integrity(repo, &ctx)
        );

        // Non-trusted bot still gets none integrity on public repo
        let renovate_issue = json!({
            "user": {"login": "renovate[bot]"},
            "author_association": "NONE"
        });
        assert_eq!(
            issue_integrity(&renovate_issue, repo, false, &ctx),
            none_integrity(repo, &ctx)
        );
    }

    #[test]
    fn test_trusted_bot_pr_integrity_public_repo() {
        let ctx = default_ctx();
        let repo = "github/copilot";

        // Trusted bot open PR on public repo gets approved (writer) integrity
        let dependabot_pr = json!({
            "user": {"login": "dependabot[bot]"},
            "author_association": "NONE",
            "merged": false
        });
        assert_eq!(
            pr_integrity(&dependabot_pr, repo, false, Some(false), &ctx),
            writer_integrity(repo, &ctx)
        );

        // Trusted bot forked PR still gets at least approved from bot status
        let actions_forked_pr = json!({
            "user": {"login": "github-actions[bot]"},
            "author_association": "NONE",
            "merged": false
        });
        assert_eq!(
            pr_integrity(&actions_forked_pr, repo, false, Some(true), &ctx),
            writer_integrity(repo, &ctx)
        );

        // Trusted bot merged PR gets merged integrity
        let copilot_merged_pr = json!({
            "user": {"login": "copilot"},
            "author_association": "NONE",
            "merged_at": "2024-01-15T10:00:00Z"
        });
        assert_eq!(
            pr_integrity(&copilot_merged_pr, repo, false, Some(false), &ctx),
            merged_integrity(repo, &ctx)
        );

        // Non-trusted bot open forked PR gets reader integrity only
        let renovate_pr = json!({
            "user": {"login": "renovate[bot]"},
            "author_association": "NONE",
            "merged": false
        });
        assert_eq!(
            pr_integrity(&renovate_pr, repo, false, Some(true), &ctx),
            reader_integrity(repo, &ctx)
        );
    }

    #[test]
    fn test_trusted_bot_commit_integrity() {
        let ctx = default_ctx();
        let repo = "github/copilot";

        // Trusted bot commit on public non-default branch gets approved
        let bot_commit = json!({
            "author": {"login": "github-actions[bot]"},
            "author_association": "NONE"
        });
        assert_eq!(
            commit_integrity(&bot_commit, repo, false, false, &ctx),
            writer_integrity(repo, &ctx)
        );

        // Trusted bot commit on default branch gets merged
        assert_eq!(
            commit_integrity(&bot_commit, repo, false, true, &ctx),
            merged_integrity(repo, &ctx)
        );
    }

    #[test]
    fn test_configured_trusted_bot_detection() {
        use super::helpers::is_configured_trusted_bot;

        let ctx_with_bots = PolicyContext {
            trusted_bots: vec!["copilot-swe-agent[bot]".to_string(), "my-org-bot".to_string()],
            ..Default::default()
        };

        // Configured bots are detected
        assert!(is_configured_trusted_bot("copilot-swe-agent[bot]", &ctx_with_bots));
        assert!(is_configured_trusted_bot("my-org-bot", &ctx_with_bots));

        // Case-insensitive
        assert!(is_configured_trusted_bot("Copilot-SWE-Agent[bot]", &ctx_with_bots));
        assert!(is_configured_trusted_bot("MY-ORG-BOT", &ctx_with_bots));

        // Bots not in the list are not detected
        assert!(!is_configured_trusted_bot("other-bot[bot]", &ctx_with_bots));
        assert!(!is_configured_trusted_bot("dependabot[bot]", &ctx_with_bots));

        // Empty context has no configured trusted bots
        let empty_ctx = default_ctx();
        assert!(!is_configured_trusted_bot("copilot-swe-agent[bot]", &empty_ctx));
        assert!(!is_configured_trusted_bot("", &empty_ctx));
    }

    #[test]
    fn test_configured_trusted_bot_issue_integrity() {
        let repo = "github/copilot";

        let ctx_with_bots = PolicyContext {
            trusted_bots: vec!["copilot-swe-agent[bot]".to_string()],
            ..Default::default()
        };

        // A configured trusted bot issue gets approved (writer) integrity even with NONE association
        let configured_bot_issue = json!({
            "user": {"login": "copilot-swe-agent[bot]"},
            "author_association": "NONE"
        });
        assert_eq!(
            issue_integrity(&configured_bot_issue, repo, false, &ctx_with_bots),
            writer_integrity(repo, &ctx_with_bots)
        );

        // Case-insensitive match
        let upper_bot_issue = json!({
            "user": {"login": "COPILOT-SWE-AGENT[BOT]"},
            "author_association": "NONE"
        });
        assert_eq!(
            issue_integrity(&upper_bot_issue, repo, false, &ctx_with_bots),
            writer_integrity(repo, &ctx_with_bots)
        );

        // Without trusted_bots in context, the same bot gets none integrity
        let ctx_without_bots = default_ctx();
        assert_eq!(
            issue_integrity(&configured_bot_issue, repo, false, &ctx_without_bots),
            none_integrity(repo, &ctx_without_bots)
        );
    }

    #[test]
    fn test_configured_trusted_bot_pr_integrity() {
        let repo = "github/copilot";

        let ctx_with_bots = PolicyContext {
            trusted_bots: vec!["copilot-swe-agent[bot]".to_string()],
            ..Default::default()
        };

        // A configured trusted bot PR gets approved (writer) integrity even with NONE association
        let configured_bot_pr = json!({
            "user": {"login": "copilot-swe-agent[bot]"},
            "author_association": "NONE"
        });
        assert_eq!(
            pr_integrity(&configured_bot_pr, repo, false, None, &ctx_with_bots),
            writer_integrity(repo, &ctx_with_bots)
        );

        // Without trusted_bots, the same bot gets none integrity
        let ctx_without_bots = default_ctx();
        assert_eq!(
            pr_integrity(&configured_bot_pr, repo, false, None, &ctx_without_bots),
            none_integrity(repo, &ctx_without_bots)
        );
    }

    #[test]
    fn test_configured_trusted_bot_combined_with_builtin() {
        let repo = "github/copilot";

        let ctx_with_bots = PolicyContext {
            trusted_bots: vec!["my-custom-bot[bot]".to_string()],
            ..Default::default()
        };

        // Built-in bot (dependabot) still gets writer integrity
        let builtin_bot_issue = json!({
            "user": {"login": "dependabot[bot]"},
            "author_association": "NONE"
        });
        assert_eq!(
            issue_integrity(&builtin_bot_issue, repo, false, &ctx_with_bots),
            writer_integrity(repo, &ctx_with_bots)
        );

        // Configured bot also gets writer integrity
        let configured_bot_issue = json!({
            "user": {"login": "my-custom-bot[bot]"},
            "author_association": "NONE"
        });
        assert_eq!(
            issue_integrity(&configured_bot_issue, repo, false, &ctx_with_bots),
            writer_integrity(repo, &ctx_with_bots)
        );

        // Unknown bot still gets none integrity
        let unknown_bot_issue = json!({
            "user": {"login": "unknown-bot[bot]"},
            "author_association": "NONE"
        });
        assert_eq!(
            issue_integrity(&unknown_bot_issue, repo, false, &ctx_with_bots),
            none_integrity(repo, &ctx_with_bots)
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
            ..Default::default()
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

    // =========================================================================
    // blocked-users tests
    // =========================================================================

    fn ctx_with_blocked_users(blocked: Vec<&str>) -> PolicyContext {
        PolicyContext {
            blocked_users: blocked.into_iter().map(|s| s.to_string()).collect(),
            ..Default::default()
        }
    }

    fn ctx_with_approval_labels(labels: Vec<&str>) -> PolicyContext {
        PolicyContext {
            approval_labels: labels.into_iter().map(|s| s.to_string()).collect(),
            ..Default::default()
        }
    }

    #[test]
    fn test_is_blocked_user_empty_ctx() {
        let ctx = default_ctx();
        assert!(!is_blocked_user("evil-bot", &ctx));
        assert!(!is_blocked_user("", &ctx));
    }

    #[test]
    fn test_is_blocked_user_case_insensitive() {
        let ctx = ctx_with_blocked_users(vec!["evil-bot", "untrusted-fork"]);
        assert!(is_blocked_user("evil-bot", &ctx));
        assert!(is_blocked_user("Evil-Bot", &ctx));
        assert!(is_blocked_user("EVIL-BOT", &ctx));
        assert!(is_blocked_user("untrusted-fork", &ctx));
        assert!(!is_blocked_user("trusted-user", &ctx));
    }

    #[test]
    fn test_blocked_integrity_returns_blocked_tag() {
        let ctx = default_ctx();
        let scope = "github/copilot";
        let labels = blocked_integrity(scope, &ctx);
        assert_eq!(labels.len(), 1);
        assert_eq!(labels[0], format!("{}{}",
            label_constants::BLOCKED_PREFIX, scope));
    }

    #[test]
    fn test_pr_integrity_blocked_user_overrides_everything() {
        let repo = "github/copilot";
        let ctx = ctx_with_blocked_users(vec!["evil-bot"]);

        // Blocked user PR — even if it has trusted association, it's blocked
        let pr = json!({
            "user": {"login": "evil-bot"},
            "author_association": "OWNER",
            "merged_at": "2024-01-15T10:00:00Z"
        });
        assert_eq!(
            pr_integrity(&pr, repo, false, Some(false), &ctx),
            blocked_integrity(repo, &ctx)
        );

        // Non-blocked user PR still works normally
        let normal_pr = json!({
            "user": {"login": "trusted-user"},
            "author_association": "OWNER",
            "merged": false
        });
        assert_eq!(
            pr_integrity(&normal_pr, repo, false, Some(false), &ctx),
            writer_integrity(repo, &ctx)
        );
    }

    #[test]
    fn test_issue_integrity_blocked_user() {
        let repo = "github/copilot";
        let ctx = ctx_with_blocked_users(vec!["bad-actor"]);

        let issue = json!({
            "user": {"login": "bad-actor"},
            "author_association": "OWNER"
        });
        assert_eq!(
            issue_integrity(&issue, repo, false, &ctx),
            blocked_integrity(repo, &ctx)
        );

        // Private repo also blocked if user is in blocked list
        assert_eq!(
            issue_integrity(&issue, repo, true, &ctx),
            blocked_integrity(repo, &ctx)
        );
    }

    #[test]
    fn test_commit_integrity_blocked_user() {
        let repo = "github/copilot";
        let ctx = ctx_with_blocked_users(vec!["bad-actor"]);

        // Commit via author.login
        let commit = json!({
            "author": {"login": "bad-actor"},
            "author_association": "OWNER"
        });
        assert_eq!(
            commit_integrity(&commit, repo, false, false, &ctx),
            blocked_integrity(repo, &ctx)
        );

        // Even default branch commits from blocked users are blocked
        assert_eq!(
            commit_integrity(&commit, repo, false, true, &ctx),
            blocked_integrity(repo, &ctx)
        );
    }

    #[test]
    fn test_blocked_user_not_promoted_by_approval_label() {
        let repo = "github/copilot";
        let ctx = PolicyContext {
            blocked_users: vec!["evil-bot".to_string()],
            approval_labels: vec!["approved".to_string()],
            ..Default::default()
        };

        // Even with an approval label, a blocked user's PR remains blocked
        let pr = json!({
            "user": {"login": "evil-bot"},
            "author_association": "NONE",
            "merged": false,
            "labels": [{"name": "approved"}]
        });
        assert_eq!(
            pr_integrity(&pr, repo, false, Some(false), &ctx),
            blocked_integrity(repo, &ctx)
        );
    }

    // =========================================================================
    // approval-labels tests
    // =========================================================================

    #[test]
    fn test_has_approval_label_empty_ctx() {
        let ctx = default_ctx();
        let item = json!({"labels": [{"name": "approved"}]});
        assert!(!has_approval_label(&item, &ctx));
    }

    #[test]
    fn test_has_approval_label_no_match() {
        let ctx = ctx_with_approval_labels(vec!["human-reviewed"]);
        let item = json!({"labels": [{"name": "needs-work"}]});
        assert!(!has_approval_label(&item, &ctx));
    }

    #[test]
    fn test_has_approval_label_matching() {
        let ctx = ctx_with_approval_labels(vec!["approved", "human-reviewed"]);

        let item_approved = json!({"labels": [{"name": "approved"}]});
        assert!(has_approval_label(&item_approved, &ctx));

        let item_reviewed = json!({"labels": [{"name": "human-reviewed"}, {"name": "bug"}]});
        assert!(has_approval_label(&item_reviewed, &ctx));
    }

    #[test]
    fn test_has_approval_label_case_insensitive() {
        let ctx = ctx_with_approval_labels(vec!["Approved"]);
        let item = json!({"labels": [{"name": "approved"}]});
        assert!(has_approval_label(&item, &ctx));

        let item2 = json!({"labels": [{"name": "APPROVED"}]});
        assert!(has_approval_label(&item2, &ctx));
    }

    #[test]
    fn test_has_approval_label_missing_labels_field() {
        let ctx = ctx_with_approval_labels(vec!["approved"]);
        let item = json!({"title": "some issue"});
        assert!(!has_approval_label(&item, &ctx));
    }

    #[test]
    fn test_pr_integrity_approval_label_promotes_to_approved() {
        let repo = "github/copilot";
        let ctx = ctx_with_approval_labels(vec!["approved"]);

        // Public forked PR normally gets unapproved (reader) integrity
        let forked_pr = json!({
            "user": {"login": "external"},
            "author_association": "NONE",
            "merged": false,
            "labels": [{"name": "approved"}]
        });
        // With approval label, should be promoted to at least writer (approved)
        assert_eq!(
            pr_integrity(&forked_pr, repo, false, Some(true), &ctx),
            writer_integrity(repo, &ctx)
        );
    }

    #[test]
    fn test_pr_integrity_approval_label_does_not_downgrade_merged() {
        let repo = "github/copilot";
        let ctx = ctx_with_approval_labels(vec!["approved"]);

        // Merged PR already at merged-level — approval label should not downgrade it
        let merged_pr = json!({
            "user": {"login": "external"},
            "author_association": "NONE",
            "merged_at": "2024-01-15T10:00:00Z",
            "labels": [{"name": "approved"}]
        });
        assert_eq!(
            pr_integrity(&merged_pr, repo, false, Some(false), &ctx),
            merged_integrity(repo, &ctx)
        );
    }

    #[test]
    fn test_issue_integrity_approval_label_promotes() {
        let repo = "github/copilot";
        let ctx = ctx_with_approval_labels(vec!["safe-to-process"]);

        // Public repo issue normally gets none integrity
        let issue = json!({
            "user": {"login": "external"},
            "author_association": "NONE",
            "labels": [{"name": "safe-to-process"}]
        });
        assert_eq!(
            issue_integrity(&issue, repo, false, &ctx),
            writer_integrity(repo, &ctx)
        );
    }

    #[test]
    fn test_issue_integrity_already_at_writer_not_affected_by_label() {
        let repo = "github/copilot";
        let ctx = ctx_with_approval_labels(vec!["approved"]);

        // Private repo issue already gets writer integrity — approval label does not change it
        let issue = json!({
            "user": {"login": "member"},
            "author_association": "MEMBER",
            "labels": [{"name": "approved"}]
        });
        // Private repo → writer_integrity; approval label → max(writer, writer) = writer
        assert_eq!(
            issue_integrity(&issue, repo, true, &ctx),
            writer_integrity(repo, &ctx)
        );
    }

    // =========================================================================
    // Tests for 22 new tools added in feat/guard-tool-coverage
    //
    // Each tool is tested for:
    //   - apply_tool_labels (label_resource): correct secrecy and integrity
    //   - label_response_items / label_response_paths where applicable
    //
    // Note on repo_id selection: ensure_integrity_baseline inside apply_tool_labels
    // uses baseline_scope = repo_id. For tools that set integrity scoped to a
    // different scope (e.g., "user", "github"), the repo_id must match that scope
    // to avoid the baseline downgrading labels to none.
    // =========================================================================

    // -------------------------------------------------------------------------
    // Actions: get_job_logs
    // -------------------------------------------------------------------------

    #[test]
    fn test_apply_tool_labels_get_job_logs() {
        let ctx = default_ctx();
        let tool_args = json!({
            "owner": "github",
            "repo": "copilot",
            "job_id": 12345
        });

        let (secrecy, integrity, _desc) = apply_tool_labels(
            "get_job_logs",
            &tool_args,
            "github/copilot",
            vec![],
            vec![],
            String::new(),
            &ctx,
        );

        assert_eq!(secrecy, secret_label(), "get_job_logs must carry secret secrecy (logs may leak tokens)");
        assert_eq!(integrity, writer_integrity("github/copilot", &ctx), "get_job_logs must have approved integrity (system-generated output)");
    }

    // -------------------------------------------------------------------------
    // Context: get_me
    // -------------------------------------------------------------------------

    #[test]
    fn test_apply_tool_labels_get_me() {
        let ctx = default_ctx();
        let tool_args = json!({});

        let (secrecy, integrity, _desc) = apply_tool_labels(
            "get_me",
            &tool_args,
            "",
            vec![],
            vec![],
            String::new(),
            &ctx,
        );

        assert_eq!(secrecy, private_user_label(), "get_me must carry private:user secrecy (PII)");
        assert_eq!(integrity, project_github_label(&ctx), "get_me must have project:github integrity (GitHub-controlled)");
    }

    // -------------------------------------------------------------------------
    // Context: get_teams
    // -------------------------------------------------------------------------

    #[test]
    fn test_apply_tool_labels_get_teams() {
        let ctx = default_ctx();
        let tool_args = json!({ "owner": "github" });

        let (secrecy, integrity, _desc) = apply_tool_labels(
            "get_teams",
            &tool_args,
            "",
            vec![],
            vec![],
            String::new(),
            &ctx,
        );

        assert_eq!(secrecy, private_user_label(), "get_teams must carry private:user secrecy (org structure is sensitive)");
        assert_eq!(integrity, project_github_label(&ctx), "get_teams must have project:github integrity");
    }

    // -------------------------------------------------------------------------
    // Context: get_team_members
    // -------------------------------------------------------------------------

    #[test]
    fn test_apply_tool_labels_get_team_members() {
        let ctx = default_ctx();
        let tool_args = json!({ "owner": "github", "team_slug": "engineering" });

        let (secrecy, integrity, _desc) = apply_tool_labels(
            "get_team_members",
            &tool_args,
            "",
            vec![],
            vec![],
            String::new(),
            &ctx,
        );

        assert_eq!(secrecy, private_user_label(), "get_team_members must carry private:user secrecy");
        assert_eq!(integrity, project_github_label(&ctx), "get_team_members must have project:github integrity");
    }

    // -------------------------------------------------------------------------
    // Discussions: list_discussions
    // -------------------------------------------------------------------------

    #[test]
    fn test_apply_tool_labels_list_discussions_secrecy_inherits_repo_visibility() {
        let ctx = default_ctx();
        let tool_args = json!({ "owner": "github", "repo": "copilot" });

        let (secrecy, integrity, _desc) = apply_tool_labels(
            "list_discussions",
            &tool_args,
            "github/copilot",
            vec![],
            vec![],
            String::new(),
            &ctx,
        );

        // In test mode backend returns None → secrecy stays [] (public assumption)
        assert_eq!(secrecy, vec![] as Vec<String>, "list_discussions secrecy inherits repo visibility");
        // writer_integrity is used regardless of repo visibility — approved at resource level
        assert_eq!(integrity, writer_integrity("github/copilot", &ctx), "list_discussions integrity is approved at resource level");
    }

    // -------------------------------------------------------------------------
    // Discussions: get_discussion
    // -------------------------------------------------------------------------

    #[test]
    fn test_apply_tool_labels_get_discussion_secrecy_inherits_repo_visibility() {
        let ctx = default_ctx();
        let tool_args = json!({
            "owner": "github",
            "repo": "copilot",
            "discussion_number": 42
        });

        let (secrecy, integrity, _desc) = apply_tool_labels(
            "get_discussion",
            &tool_args,
            "github/copilot",
            vec![],
            vec![],
            String::new(),
            &ctx,
        );

        assert_eq!(secrecy, vec![] as Vec<String>);
        assert_eq!(integrity, writer_integrity("github/copilot", &ctx));
    }

    // -------------------------------------------------------------------------
    // Discussions: get_discussion_comments
    // -------------------------------------------------------------------------

    #[test]
    fn test_apply_tool_labels_get_discussion_comments_secrecy_inherits_repo_visibility() {
        let ctx = default_ctx();
        let tool_args = json!({
            "owner": "github",
            "repo": "copilot",
            "discussion_number": 42
        });

        let (secrecy, integrity, _desc) = apply_tool_labels(
            "get_discussion_comments",
            &tool_args,
            "github/copilot",
            vec![],
            vec![],
            String::new(),
            &ctx,
        );

        assert_eq!(secrecy, vec![] as Vec<String>);
        assert_eq!(integrity, writer_integrity("github/copilot", &ctx));
    }

    // -------------------------------------------------------------------------
    // Discussions: list_discussion_categories
    // -------------------------------------------------------------------------

    #[test]
    fn test_apply_tool_labels_list_discussion_categories_approved_integrity() {
        let ctx = default_ctx();
        let tool_args = json!({ "owner": "github", "repo": "copilot" });

        let (secrecy, integrity, _desc) = apply_tool_labels(
            "list_discussion_categories",
            &tool_args,
            "github/copilot",
            vec![],
            vec![],
            String::new(),
            &ctx,
        );

        assert_eq!(secrecy, vec![] as Vec<String>, "list_discussion_categories secrecy inherits repo visibility");
        assert_eq!(integrity, writer_integrity("github/copilot", &ctx), "list_discussion_categories must have approved integrity (maintainer-managed)");
    }

    // -------------------------------------------------------------------------
    // Gists: list_gists
    // -------------------------------------------------------------------------

    #[test]
    fn test_apply_tool_labels_list_gists() {
        let ctx = default_ctx();
        let tool_args = json!({ "username": "octocat" });

        let (secrecy, integrity, _desc) = apply_tool_labels(
            "list_gists",
            &tool_args,
            "",
            vec![],
            vec![],
            String::new(),
            &ctx,
        );

        assert_eq!(secrecy, private_user_label(), "list_gists must carry private:user secrecy (mix of public/secret gists)");
        assert_eq!(integrity, reader_integrity("user", &ctx), "list_gists must have reader (unapproved) integrity (user content)");
    }

    // -------------------------------------------------------------------------
    // Gists: get_gist
    // -------------------------------------------------------------------------

    #[test]
    fn test_apply_tool_labels_get_gist() {
        let ctx = default_ctx();
        let tool_args = json!({ "gist_id": "abc123def456" });

        let (secrecy, integrity, _desc) = apply_tool_labels(
            "get_gist",
            &tool_args,
            "",
            vec![],
            vec![],
            String::new(),
            &ctx,
        );

        assert_eq!(secrecy, private_user_label(), "get_gist must carry private:user secrecy");
        assert_eq!(integrity, reader_integrity("user", &ctx), "get_gist must have reader integrity");
    }

    // -------------------------------------------------------------------------
    // Git: get_repository_tree
    // -------------------------------------------------------------------------

    #[test]
    fn test_apply_tool_labels_get_repository_tree_approved_integrity() {
        let ctx = default_ctx();
        let tool_args = json!({
            "owner": "github",
            "repo": "copilot",
            "tree_sha": "main"
        });

        let (secrecy, integrity, _desc) = apply_tool_labels(
            "get_repository_tree",
            &tool_args,
            "github/copilot",
            vec![],
            vec![],
            String::new(),
            &ctx,
        );

        assert_eq!(secrecy, vec![] as Vec<String>, "get_repository_tree secrecy inherits repo visibility");
        assert_eq!(integrity, writer_integrity("github/copilot", &ctx), "get_repository_tree must have approved integrity (repo metadata)");
    }

    // -------------------------------------------------------------------------
    // Labels: list_label
    // -------------------------------------------------------------------------

    #[test]
    fn test_apply_tool_labels_list_label_approved_integrity() {
        let ctx = default_ctx();
        let tool_args = json!({ "owner": "github", "repo": "copilot" });

        let (secrecy, integrity, _desc) = apply_tool_labels(
            "list_label",
            &tool_args,
            "github/copilot",
            vec![],
            vec![],
            String::new(),
            &ctx,
        );

        assert_eq!(secrecy, vec![] as Vec<String>, "list_label secrecy inherits repo visibility");
        assert_eq!(integrity, writer_integrity("github/copilot", &ctx), "list_label must have approved integrity (maintainer-managed metadata)");
    }

    // -------------------------------------------------------------------------
    // Notifications: list_notifications
    // -------------------------------------------------------------------------

    #[test]
    fn test_apply_tool_labels_list_notifications() {
        let ctx = default_ctx();
        let tool_args = json!({});

        let (secrecy, integrity, _desc) = apply_tool_labels(
            "list_notifications",
            &tool_args,
            "",
            vec![],
            vec![],
            String::new(),
            &ctx,
        );

        assert_eq!(secrecy, private_user_label(), "list_notifications must carry private:user secrecy");
        // integrity = vec![] in rule → ensure_integrity_baseline("", [], ctx) = none_integrity("", ctx) = ["none"]
        assert_eq!(integrity, none_integrity("", &ctx), "list_notifications must have none-level integrity (references external content of unknown trust)");
    }

    // -------------------------------------------------------------------------
    // Notifications: get_notification_details
    // -------------------------------------------------------------------------

    #[test]
    fn test_apply_tool_labels_get_notification_details() {
        let ctx = default_ctx();
        let tool_args = json!({ "thread_id": "12345" });

        let (secrecy, integrity, _desc) = apply_tool_labels(
            "get_notification_details",
            &tool_args,
            "",
            vec![],
            vec![],
            String::new(),
            &ctx,
        );

        assert_eq!(secrecy, private_user_label(), "get_notification_details must carry private:user secrecy");
        assert_eq!(integrity, none_integrity("", &ctx), "get_notification_details must have none-level integrity");
    }

    // -------------------------------------------------------------------------
    // Projects: projects_list (new canonical name for list_project_items)
    // -------------------------------------------------------------------------

    #[test]
    fn test_apply_tool_labels_projects_list_owner_scoped_integrity() {
        let ctx = default_ctx();
        let tool_args = json!({ "owner": "github" });

        // projects_list sets baseline_scope = owner = "github"
        let (_secrecy, integrity, _desc) = apply_tool_labels(
            "projects_list",
            &tool_args,
            "",
            vec![],
            vec![],
            String::new(),
            &ctx,
        );

        assert_eq!(integrity, writer_integrity("github", &ctx), "projects_list must have approved:owner integrity");
    }

    #[test]
    fn test_apply_tool_labels_projects_list_with_owner_scoped_ctx() {
        let ctx = owner_scoped_ctx("github");
        let tool_args = json!({ "owner": "github" });

        let (_secrecy, integrity, _desc) = apply_tool_labels(
            "projects_list",
            &tool_args,
            "",
            vec![],
            vec![],
            String::new(),
            &ctx,
        );

        assert!(
            integrity.contains(&"approved:github".to_string()),
            "projects_list with scoped ctx must have 'approved:github', got: {:?}",
            integrity
        );
    }

    // -------------------------------------------------------------------------
    // Projects: projects_get (new canonical name for get_project)
    // -------------------------------------------------------------------------

    #[test]
    fn test_apply_tool_labels_projects_get_owner_scoped_integrity() {
        let ctx = owner_scoped_ctx("myorg");
        let tool_args = json!({ "owner": "myorg", "project_number": 5 });

        let (_secrecy, integrity, _desc) = apply_tool_labels(
            "projects_get",
            &tool_args,
            "",
            vec![],
            vec![],
            String::new(),
            &ctx,
        );

        assert!(
            integrity.contains(&"approved:myorg".to_string()),
            "projects_get must have 'approved:myorg' integrity, got: {:?}",
            integrity
        );
    }

    // -------------------------------------------------------------------------
    // Repos: list_starred_repositories
    // -------------------------------------------------------------------------

    #[test]
    fn test_apply_tool_labels_list_starred_repositories() {
        let ctx = default_ctx();
        let tool_args = json!({ "username": "octocat" });

        let (secrecy, integrity, _desc) = apply_tool_labels(
            "list_starred_repositories",
            &tool_args,
            "",
            vec![],
            vec![],
            String::new(),
            &ctx,
        );

        assert_eq!(secrecy, private_user_label(), "list_starred_repositories must carry private:user secrecy (personal preferences)");
        assert_eq!(integrity, project_github_label(&ctx), "list_starred_repositories must have project:github integrity (GitHub-controlled metadata)");
    }

    // -------------------------------------------------------------------------
    // Search: search_orgs
    // -------------------------------------------------------------------------

    #[test]
    fn test_apply_tool_labels_search_orgs() {
        let ctx = default_ctx();
        let tool_args = json!({ "query": "github" });

        let (secrecy, integrity, _desc) = apply_tool_labels(
            "search_orgs",
            &tool_args,
            "",
            vec![],
            vec![],
            String::new(),
            &ctx,
        );

        assert_eq!(secrecy, vec![] as Vec<String>, "search_orgs must have public (empty) secrecy");
        assert_eq!(integrity, project_github_label(&ctx), "search_orgs must have project:github integrity");
    }

    // -------------------------------------------------------------------------
    // Security Advisories: list_global_security_advisories
    // -------------------------------------------------------------------------

    #[test]
    fn test_apply_tool_labels_list_global_security_advisories() {
        let ctx = default_ctx();
        let tool_args = json!({ "severity": "critical" });

        let (secrecy, integrity, _desc) = apply_tool_labels(
            "list_global_security_advisories",
            &tool_args,
            "",
            vec![],
            vec![],
            String::new(),
            &ctx,
        );

        assert_eq!(secrecy, vec![] as Vec<String>, "global advisories are public CVE data — empty secrecy");
        assert_eq!(integrity, project_github_label(&ctx), "global advisories curated by GitHub security team — project:github integrity");
    }

    // -------------------------------------------------------------------------
    // Security Advisories: get_global_security_advisory
    // -------------------------------------------------------------------------

    #[test]
    fn test_apply_tool_labels_get_global_security_advisory() {
        let ctx = default_ctx();
        let tool_args = json!({ "ghsa_id": "GHSA-xxxx-yyyy-zzzz" });

        let (secrecy, integrity, _desc) = apply_tool_labels(
            "get_global_security_advisory",
            &tool_args,
            "",
            vec![],
            vec![],
            String::new(),
            &ctx,
        );

        assert_eq!(secrecy, vec![] as Vec<String>);
        assert_eq!(integrity, project_github_label(&ctx));
    }

    // -------------------------------------------------------------------------
    // Security Advisories: list_repository_security_advisories
    // -------------------------------------------------------------------------

    #[test]
    fn test_apply_tool_labels_list_repository_security_advisories() {
        let ctx = default_ctx();
        let tool_args = json!({ "owner": "github", "repo": "copilot" });

        let (secrecy, integrity, _desc) = apply_tool_labels(
            "list_repository_security_advisories",
            &tool_args,
            "github/copilot",
            vec![],
            vec![],
            String::new(),
            &ctx,
        );

        assert_eq!(
            secrecy,
            vec!["private:github/copilot".to_string()],
            "repo security advisories may contain embargoed vulnerability info — private:repo secrecy"
        );
        assert_eq!(integrity, writer_integrity("github/copilot", &ctx), "repo security advisories maintained by repo security contacts — approved integrity");
    }

    // -------------------------------------------------------------------------
    // Security Advisories: list_org_repository_security_advisories
    // -------------------------------------------------------------------------

    #[test]
    fn test_apply_tool_labels_list_org_repo_security_advisories() {
        let ctx = default_ctx();
        let tool_args = json!({ "owner": "github", "repo": "copilot" });

        let (secrecy, integrity, _desc) = apply_tool_labels(
            "list_org_repository_security_advisories",
            &tool_args,
            "github/copilot",
            vec![],
            vec![],
            String::new(),
            &ctx,
        );

        assert_eq!(
            secrecy,
            vec!["private:github/copilot".to_string()],
            "org repo security advisories must carry private:repo secrecy"
        );
        assert_eq!(integrity, writer_integrity("github/copilot", &ctx));
    }

    // =========================================================================
    // label_response_items tests for new tools
    // =========================================================================

    // -------------------------------------------------------------------------
    // list_gists — public gist
    // -------------------------------------------------------------------------

    #[test]
    fn test_label_response_items_list_gists_public_gist_empty_secrecy() {
        let ctx = default_ctx();
        let tool_args = json!({ "username": "octocat" });
        let response = json!([
            { "id": "abc123def456", "public": true, "description": "A public gist" }
        ]);

        let items = label_response_items("list_gists", &tool_args, &response, &ctx);

        assert_eq!(items.len(), 1, "should label one gist item");
        let item = &items[0];
        assert_eq!(item.labels.secrecy, vec![] as Vec<String>, "public gist must have empty secrecy");
        assert_eq!(item.labels.integrity, reader_integrity("user", &ctx), "gist must have reader integrity");
        assert_eq!(item.labels.description, "gist:abc123def456");
    }

    // -------------------------------------------------------------------------
    // list_gists — private gist
    // -------------------------------------------------------------------------

    #[test]
    fn test_label_response_items_list_gists_private_gist_private_user_secrecy() {
        let ctx = default_ctx();
        let tool_args = json!({ "username": "octocat" });
        let response = json!([
            { "id": "secret789xyz", "public": false, "description": "A secret gist" }
        ]);

        let items = label_response_items("list_gists", &tool_args, &response, &ctx);

        assert_eq!(items.len(), 1);
        let item = &items[0];
        assert_eq!(item.labels.secrecy, private_user_label(), "secret gist must carry private:user secrecy");
        assert_eq!(item.labels.integrity, reader_integrity("user", &ctx), "secret gist still has reader integrity");
        assert_eq!(item.labels.description, "gist:secret789xyz");
    }

    // -------------------------------------------------------------------------
    // list_gists — mixed public and private
    // -------------------------------------------------------------------------

    #[test]
    fn test_label_response_items_list_gists_mixed_public_and_private() {
        let ctx = default_ctx();
        let tool_args = json!({});
        let response = json!([
            { "id": "pub1", "public": true },
            { "id": "sec2", "public": false },
            { "id": "pub3", "public": true }
        ]);

        let items = label_response_items("list_gists", &tool_args, &response, &ctx);

        assert_eq!(items.len(), 3);
        assert_eq!(items[0].labels.secrecy, vec![] as Vec<String>, "first item is public → empty secrecy");
        assert_eq!(items[1].labels.secrecy, private_user_label(), "second item is private → private:user");
        assert_eq!(items[2].labels.secrecy, vec![] as Vec<String>, "third item is public → empty secrecy");
        // All gists share the same reader integrity level
        for item in &items {
            assert_eq!(item.labels.integrity, reader_integrity("user", &ctx));
        }
    }

    // -------------------------------------------------------------------------
    // get_gist — public gist
    // -------------------------------------------------------------------------

    #[test]
    fn test_label_response_items_get_gist_public_secrecy_reader_integrity() {
        let ctx = default_ctx();
        let tool_args = json!({ "gist_id": "abc123def456" });
        let response = json!({ "id": "abc123def456", "public": true });

        let items = label_response_items("get_gist", &tool_args, &response, &ctx);

        assert_eq!(items.len(), 1, "single-object response must produce one labeled item");
        assert_eq!(items[0].labels.secrecy, vec![] as Vec<String>);
        assert_eq!(items[0].labels.integrity, reader_integrity("user", &ctx));
    }

    // -------------------------------------------------------------------------
    // get_gist — private (secret) gist
    // -------------------------------------------------------------------------

    #[test]
    fn test_label_response_items_get_gist_private() {
        let ctx = default_ctx();
        let tool_args = json!({ "gist_id": "secret789xyz" });
        let response = json!({ "id": "secret789xyz", "public": false });

        let items = label_response_items("get_gist", &tool_args, &response, &ctx);

        assert_eq!(items.len(), 1);
        assert_eq!(items[0].labels.secrecy, private_user_label());
        assert_eq!(items[0].labels.integrity, reader_integrity("user", &ctx));
    }

    // -------------------------------------------------------------------------
    // list_notifications — response items
    // -------------------------------------------------------------------------

    #[test]
    fn test_label_response_items_list_notifications_private_user_secrecy() {
        let ctx = default_ctx();
        let tool_args = json!({});
        let response = json!([
            {
                "id": "n1",
                "subject": { "title": "Fix login bug", "type": "Issue" },
                "reason": "mention"
            },
            {
                "id": "n2",
                "subject": { "title": "Add feature X", "type": "PullRequest" },
                "reason": "review_requested"
            }
        ]);

        let items = label_response_items("list_notifications", &tool_args, &response, &ctx);

        assert_eq!(items.len(), 2, "should label both notification items");
        for item in &items {
            assert_eq!(item.labels.secrecy, private_user_label(), "notifications are always private:user");
            // none_integrity("", ctx) = ["none"]
            assert_eq!(item.labels.integrity, none_integrity("", &ctx), "notifications carry no trust — none integrity");
        }
        assert_eq!(items[0].labels.description, "notification:n1");
        assert_eq!(items[1].labels.description, "notification:n2");
    }

    // -------------------------------------------------------------------------
    // get_notification_details — response items (MCP-wrapped single object)
    // -------------------------------------------------------------------------

    #[test]
    fn test_label_response_items_get_notification_details_mcp_wrapped() {
        let ctx = default_ctx();
        let tool_args = json!({ "thread_id": "12345" });
        // get_notification_details returns an array response in the items handler
        let inner = json!([{"id": "12345", "subject": {"title": "Security alert", "type": "RepositoryVulnerabilityAlert"}}]).to_string();
        let response = json!({
            "content": [{ "type": "text", "text": inner }]
        });

        let items = label_response_items("get_notification_details", &tool_args, &response, &ctx);

        assert_eq!(items.len(), 1);
        assert_eq!(items[0].labels.secrecy, private_user_label());
        assert_eq!(items[0].labels.integrity, none_integrity("", &ctx));
        assert_eq!(items[0].labels.description, "notification:12345");
    }

    // =========================================================================
    // label_response_paths tests for new tools
    // =========================================================================

    // -------------------------------------------------------------------------
    // list_gists — path labels with public gist
    // -------------------------------------------------------------------------

    #[test]
    fn test_label_response_paths_list_gists_public_gist_empty_secrecy() {
        let ctx = default_ctx();
        let tool_args = json!({});
        let response = json!([
            { "id": "pub1", "public": true }
        ]);

        let result = label_response_paths("list_gists", &tool_args, &response, &ctx)
            .expect("list_gists should produce path labels");

        assert_eq!(result.labeled_paths.len(), 1);
        assert_eq!(result.labeled_paths[0].path, "/0");
        assert_eq!(result.labeled_paths[0].labels.secrecy, vec![] as Vec<String>, "public gist path must have empty secrecy");
        assert_eq!(result.labeled_paths[0].labels.integrity, reader_integrity("user", &ctx));
        assert_eq!(result.labeled_paths[0].labels.description, "gist:pub1");
    }

    // -------------------------------------------------------------------------
    // list_gists — path labels with private gist
    // -------------------------------------------------------------------------

    #[test]
    fn test_label_response_paths_list_gists_private_gist_private_user_secrecy() {
        let ctx = default_ctx();
        let tool_args = json!({});
        let response = json!([
            { "id": "sec1", "public": false }
        ]);

        let result = label_response_paths("list_gists", &tool_args, &response, &ctx)
            .expect("list_gists should produce path labels");

        assert_eq!(result.labeled_paths.len(), 1);
        assert_eq!(result.labeled_paths[0].labels.secrecy, private_user_label(), "private gist path must carry private:user secrecy");
        assert_eq!(result.labeled_paths[0].labels.integrity, reader_integrity("user", &ctx));
    }

    // -------------------------------------------------------------------------
    // list_gists — path labels mixed public/private
    // -------------------------------------------------------------------------

    #[test]
    fn test_label_response_paths_list_gists_mixed_visibility() {
        let ctx = default_ctx();
        let tool_args = json!({});
        let response = json!([
            { "id": "pub1", "public": true },
            { "id": "sec2", "public": false }
        ]);

        let result = label_response_paths("list_gists", &tool_args, &response, &ctx)
            .expect("list_gists should produce path labels");

        assert_eq!(result.labeled_paths.len(), 2);
        assert_eq!(result.labeled_paths[0].labels.secrecy, vec![] as Vec<String>);
        assert_eq!(result.labeled_paths[1].labels.secrecy, private_user_label());
        // Default labels for the collection use conservative reader integrity
        let default_labels = result.default_labels.as_ref().expect("should have default labels");
        assert_eq!(default_labels.secrecy, vec![] as Vec<String>);
        assert_eq!(default_labels.integrity, reader_integrity("user", &ctx));
    }

    // -------------------------------------------------------------------------
    // list_notifications — path labels
    // -------------------------------------------------------------------------

    #[test]
    fn test_label_response_paths_list_notifications_private_empty_integrity() {
        let ctx = default_ctx();
        let tool_args = json!({});
        let response = json!([
            { "id": "n1", "reason": "mention" },
            { "id": "n2", "reason": "review_requested" }
        ]);

        let result = label_response_paths("list_notifications", &tool_args, &response, &ctx)
            .expect("list_notifications should produce path labels");

        assert_eq!(result.labeled_paths.len(), 2);
        for entry in &result.labeled_paths {
            assert_eq!(entry.labels.secrecy, private_user_label(), "all notification paths must be private:user");
            // response_paths.rs uses vec![] directly (not none_integrity)
            assert_eq!(entry.labels.integrity, vec![] as Vec<String>, "notification paths carry no integrity tags");
        }
        assert_eq!(result.labeled_paths[0].path, "/0");
        assert_eq!(result.labeled_paths[1].path, "/1");

        let default_labels = result.default_labels.as_ref().expect("should have default labels");
        assert_eq!(default_labels.secrecy, private_user_label());
        assert_eq!(default_labels.integrity, vec![] as Vec<String>);
    }

    // -------------------------------------------------------------------------
    // projects_list — path labels (new canonical name for list_project_items)
    // -------------------------------------------------------------------------

    #[test]
    fn test_label_response_paths_projects_list_issue_item() {
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

        let result = label_response_paths("projects_list", &tool_args, &response, &ctx)
            .expect("projects_list should produce path labels");

        assert_eq!(result.labeled_paths.len(), 1);
        let entry = &result.labeled_paths[0];
        assert_eq!(entry.labels.description, "project-item:issue");
        assert!(
            entry.labels.integrity.iter().any(|l| l.starts_with("approved:")),
            "MEMBER association must yield approved-level integrity, got: {:?}",
            entry.labels.integrity
        );
    }

    #[test]
    fn test_label_response_paths_projects_list_draft_issue_item() {
        let ctx = owner_scoped_ctx("github");
        let tool_args = json!({ "owner": "github", "project_number": 1 });
        let response = json!({
            "items": [
                {
                    "type": "DRAFT_ISSUE",
                    "creator": { "login": "some-admin" }
                }
            ]
        });

        let result = label_response_paths("projects_list", &tool_args, &response, &ctx)
            .expect("projects_list should produce path labels for DRAFT_ISSUE");

        assert_eq!(result.labeled_paths.len(), 1);
        let entry = &result.labeled_paths[0];
        assert_eq!(entry.labels.description, "project-item:draft_issue");
        assert_eq!(entry.labels.secrecy, vec![] as Vec<String>, "draft issues have no repo — empty secrecy");
        assert!(
            entry.labels.integrity.contains(&"approved:github".to_string()),
            "DRAFT_ISSUE must have approved:github integrity, got: {:?}",
            entry.labels.integrity
        );
    }

    #[test]
    fn test_label_response_paths_projects_list_pull_request_item() {
        let ctx = default_ctx();
        let tool_args = json!({ "owner": "github", "project_number": 2 });
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

        let result = label_response_paths("projects_list", &tool_args, &response, &ctx)
            .expect("projects_list should produce path labels for PULL_REQUEST");

        assert_eq!(result.labeled_paths.len(), 1);
        let entry = &result.labeled_paths[0];
        assert_eq!(entry.labels.description, "project-item:pull_request");
        assert!(
            entry.labels.integrity.iter().any(|l| l.starts_with("unapproved:")),
            "CONTRIBUTOR association must yield unapproved-level integrity, got: {:?}",
            entry.labels.integrity
        );
    }
}
