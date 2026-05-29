//! Tool classification for GitHub operations
//!
//! This module provides functions to classify GitHub MCP tools
//! by their operation type (read, write, merge, delete, etc.)

/// Write operations that modify data
pub const WRITE_OPERATIONS: &[&str] = &[
    "create_repository",
    "create_branch",
    "create_or_update_file",
    "push_files",
    "delete_file",
    "fork_repository",
    "create_pull_request",
    "create_pull_request_with_copilot",
    "add_comment_to_pending_review",
    "add_reply_to_pull_request_comment",
    "request_copilot_review",
    "add_issue_comment",
    "assign_copilot_to_issue",
    "actions_run_trigger",
    "create_gist",
    "dismiss_notification",
    "mark_all_notifications_read",
    "manage_notification_subscription",
    "manage_repository_notification_subscription",
    "projects_write",
    "star_repository",
    "unstar_repository",
    "label_write",
    "create_issue",
    // Dynamically enables additional toolsets, expanding the agent's capability set
    "enable_toolset",
    // Pre-emptive entries for anticipated future MCP tools (no equivalent tool today)
    "archive_repository",   // gh repo archive   — blocked: repo settings change unsupported
    "unarchive_repository", // gh repo unarchive — blocked: symmetric to archive_repository
    "rename_repository",    // gh repo rename    — blocked: breaks clone URLs and integrations
    "transfer_issue",       // gh issue transfer
    "transfer_repository",  // gh repo transfer  — blocked: repo ownership transfer is irreversible
    "pin_issue",            // gh issue pin
    "unpin_issue",          // gh issue unpin
    "enable_workflow",    // gh workflow enable
    "disable_workflow",   // gh workflow disable
    "set_secret",         // gh secret set
    "set_variable",         // gh variable set
    "upload_release_asset", // gh release upload
    "sync_fork",            // gh repo sync
    // gh run cancel / force-cancel
    "cancel_workflow_run",       // gh run cancel       — cancels an in-progress workflow run
    "force_cancel_workflow_run", // gh run cancel --force — force-cancels a workflow run
    // gh run rerun
    "rerun_workflow_run",  // gh run rerun        — reruns a completed workflow run
    "rerun_failed_jobs",   // gh run rerun --failed — reruns only failed jobs
    "rerun_workflow_job",  // gh run rerun --job  — reruns a specific job
    // Pre-emptive: gh repo edit (PATCH /repos/{owner}/{repo}) — can change visibility, security settings
    "edit_repository",
    // Pre-emptive: gh pr revert (GraphQL revertPullRequest) — creates revert branch + PR
    "revert_pull_request",
    // Pre-emptive: gh repo deploy-key add/delete — SSH key with optional write access
    "add_deploy_key",
    "delete_deploy_key",
    // Deprecated alias coverage (guard sees alias name before backend resolves it)
    "run_workflow",             // deprecated alias for actions_run_trigger (POST workflow dispatch)
    "delete_workflow_run_logs", // deprecated alias for actions_run_trigger (DELETE run logs)
    "add_project_item",        // deprecated alias for projects_write (addProjectV2ItemById)
    "delete_project_item",     // deprecated alias for projects_write (deleteProjectV2Item)
    // Pre-emptive: issue/PR comment editing/deletion (gh issue/pr comment --edit/--delete)
    "update_issue_comment", // PATCH /repos/.../issues/comments/{id}
    "delete_issue_comment", // DELETE /repos/.../issues/comments/{id}
    // Pre-emptive: release management (gh release create/edit/delete)
    "create_release", // POST /repos/.../releases
    "edit_release",   // PATCH /repos/.../releases/{id}
    "delete_release", // DELETE /repos/.../releases/{id}
    // Pre-emptive: gist deletion (gh gist delete)
    "delete_gist", // DELETE /gists/{gist_id}
    // Discussion comment write (addDiscussionComment / updateDiscussionComment via GraphQL)
    "discussion_comment_write", // creates or edits GitHub Discussion comments

];

/// Read-write operations that both read and modify data
pub const READ_WRITE_OPERATIONS: &[&str] = &[
    "merge_pull_request",
    "update_pull_request",
    "update_pull_request_branch",
    "pull_request_review_write",
    "issue_write",
    "issue_write_ff_remote_mcp_issue_fields", // feature-flag variant of issue_write
    "sub_issue_write",
    "update_gist",
    // Pre-emptive entries for anticipated future MCP tools (no equivalent tool today)
    // gh agent-task create — creates a Copilot coding-agent job (branch + PR); blocked as unsupported
    "create_agent_task",
    // Deprecated alias coverage
    "update_project_item", // deprecated alias for projects_write (updateProjectV2ItemFieldValue)

    // Granular issue update tools (alongside issue_write composite)
    "update_issue_assignees", // PATCH — modifies issue assignees
    "update_issue_body",      // PATCH — modifies issue body
    "update_issue_labels",    // PATCH — modifies issue labels
    "update_issue_milestone", // PATCH — modifies issue milestone
    "update_issue_state",     // PATCH — opens or closes an issue
    "update_issue_title",     // PATCH — modifies issue title
    "update_issue_type",      // PATCH — modifies issue type

    // Issue custom field mutation (field definitions are org-level; target issue is repo-scoped)
    "set_issue_fields", // GraphQL — sets custom field values on a specific repository issue

    // Sub-issue management tools (alongside sub_issue_write composite)
    "add_sub_issue",          // POST  /repos/.../issues/{number}/sub_issues
    "remove_sub_issue",       // DELETE/POST — remove sub-issue link
    "reprioritize_sub_issue", // PATCH — reorder sub-issues

    // PR review tools (alongside pull_request_review_write composite)
    "add_pull_request_review_comment",    // POST /repos/.../pulls/{number}/comments
    "create_pull_request_review",         // POST /repos/.../pulls/{number}/reviews
    "delete_pending_pull_request_review", // DELETE /repos/.../pulls/{number}/reviews/{id}
    "request_pull_request_reviewers",     // POST /repos/.../pulls/{number}/requested_reviewers
    "resolve_review_thread",              // PUT  /graphql — resolveReviewThread
    "submit_pending_pull_request_review", // POST /repos/.../pulls/{number}/reviews/{id}/events
    "unresolve_review_thread",            // PUT  /graphql — unresolveReviewThread

    // Granular PR update tools (alongside update_pull_request composite)
    "update_pull_request_body",        // PATCH — modifies PR body
    "update_pull_request_draft_state", // PATCH — converts to/from draft
    "update_pull_request_state",       // PATCH — opens or closes a PR
    "update_pull_request_title",       // PATCH — modifies PR title
];

/// Check if a tool is a write operation
pub fn is_write_operation(tool_name: &str) -> bool {
    WRITE_OPERATIONS.contains(&tool_name)
        || is_lock_operation(tool_name)
        || is_unlock_operation(tool_name)
}

/// Check if a tool is a read-write operation
pub fn is_read_write_operation(tool_name: &str) -> bool {
    READ_WRITE_OPERATIONS.contains(&tool_name)
}

/// Check if a tool is a merge operation
pub fn is_merge_operation(tool_name: &str) -> bool {
    tool_name.starts_with("merge_")
}

/// Check if a tool is a delete operation
pub fn is_delete_operation(tool_name: &str) -> bool {
    tool_name.starts_with("delete_")
}

/// Check if a tool is a lock operation
pub fn is_lock_operation(tool_name: &str) -> bool {
    tool_name.starts_with("lock_")
}

/// Check if a tool is an unlock operation
pub fn is_unlock_operation(tool_name: &str) -> bool {
    tool_name.starts_with("unlock_")
}

/// Tools that are unconditionally blocked regardless of agent integrity.
///
/// These operations are too dangerous or unsupported to ever permit via an agent.
/// Entries here should also appear in `WRITE_OPERATIONS` or `READ_WRITE_OPERATIONS`
/// so the tool is still subject to all normal write-path checks before being denied.
pub const BLOCKED_TOOLS: &[&str] = &[
    "transfer_repository",  // irreversible ownership transfer
    "archive_repository",   // repo settings change; unsupported
    "unarchive_repository", // symmetric to archive_repository
    "rename_repository",    // breaks clone URLs and integrations
    "create_agent_task",    // unsupported agent-task creation
];

/// Check if a tool is unconditionally blocked (always denied regardless of agent integrity).
///
/// Blocked tools are listed here when the operation is considered too dangerous
/// to ever permit via an agent, even if the agent would otherwise satisfy the
/// integrity requirements for a normal write operation.
///
/// Current entries:
/// - `transfer_repository`: repository ownership transfer is irreversible and
///   must never be performed by an automated agent.
/// - `archive_repository`: archives a repository, restricting contributions; unsupported as an
///   agent operation.
/// - `unarchive_repository`: re-enables contributions to a previously archived repository;
///   symmetric to `archive_repository` and equally unsupported.
/// - `rename_repository`: renames a repository, breaking all clone URLs, webhooks, and external
///   references; unsupported as an agent operation.
/// - `create_agent_task`: creates a Copilot coding-agent job that opens a branch and PR;
///   unsupported as a directly invocable agent operation.
pub fn is_blocked_tool(tool_name: &str) -> bool {
    BLOCKED_TOOLS.contains(&tool_name)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn blocked_tools_are_classified_as_write_or_read_write() {
        for &tool in BLOCKED_TOOLS {
            assert!(
                WRITE_OPERATIONS.contains(&tool) || READ_WRITE_OPERATIONS.contains(&tool),
                "blocked tool `{tool}` must also be classified in WRITE_OPERATIONS or READ_WRITE_OPERATIONS"
            );
        }
    }

    #[test]
    fn test_is_blocked_tool_transfer_repository() {
        assert!(
            is_blocked_tool("transfer_repository"),
            "transfer_repository must be unconditionally blocked"
        );
    }

    #[test]
    fn test_is_blocked_tool_repo_modifying_operations() {
        for op in &["archive_repository", "unarchive_repository", "rename_repository"] {
            assert!(
                is_blocked_tool(op),
                "{} must be unconditionally blocked (modifying gh repo operation)",
                op
            );
        }
    }

    #[test]
    fn test_is_blocked_tool_other_write_ops_not_blocked() {
        // Regular write operations should not be blocked
        for op in &["create_issue", "add_issue_comment", "pin_issue", "unpin_issue"] {
            assert!(
                !is_blocked_tool(op),
                "{} should not be blocked",
                op
            );
        }
    }

    #[test]
    fn test_transfer_repository_is_write_operation() {
        assert!(
            is_write_operation("transfer_repository"),
            "transfer_repository must be classified as a write operation"
        );
    }

    #[test]
    fn test_repo_modifying_operations_are_write_operations() {
        for op in &["archive_repository", "unarchive_repository", "rename_repository"] {
            assert!(
                is_write_operation(op),
                "{} must be classified as a write operation",
                op
            );
        }
    }

    #[test]
    fn test_pin_unpin_issue_are_write_operations() {
        assert!(
            is_write_operation("pin_issue"),
            "pin_issue must be classified as a write operation"
        );
        assert!(
            is_write_operation("unpin_issue"),
            "unpin_issue must be classified as a write operation"
        );
    }

    #[test]
    fn test_workflow_run_cancel_rerun_are_write_operations() {
        for op in &[
            "cancel_workflow_run",
            "force_cancel_workflow_run",
            "rerun_workflow_run",
            "rerun_failed_jobs",
            "rerun_workflow_job",
        ] {
            assert!(
                is_write_operation(op),
                "{} must be classified as a write operation",
                op
            );
        }
    }

    #[test]
    fn test_cli_gap_operations_are_write_operations() {
        for op in &[
            "edit_repository",
            "revert_pull_request",
            "add_deploy_key",
            "delete_deploy_key",
        ] {
            assert!(
                is_write_operation(op),
                "{} must be classified as a write operation",
                op
            );
        }
    }

    #[test]
    fn test_create_agent_task_is_read_write_and_blocked() {
        assert!(
            is_read_write_operation("create_agent_task"),
            "create_agent_task must be classified as a read-write operation"
        );
        assert!(
            is_blocked_tool("create_agent_task"),
            "create_agent_task must be unconditionally blocked (unsupported agent operation)"
        );
        assert!(
            !is_write_operation("create_agent_task"),
            "create_agent_task should not be in WRITE_OPERATIONS (it is in READ_WRITE_OPERATIONS)"
        );
    }

    #[test]
    fn test_deprecated_alias_write_operations() {
        for op in &[
            "run_workflow",
            "delete_workflow_run_logs",
            "add_project_item",
            "delete_project_item",
        ] {
            assert!(
                is_write_operation(op),
                "{} (deprecated alias) must be classified as a write operation",
                op
            );
        }
    }

    #[test]
    fn test_deprecated_alias_read_write_operations() {
        assert!(
            is_read_write_operation("update_project_item"),
            "update_project_item (deprecated alias) must be classified as a read-write operation"
        );
    }

    #[test]
    fn test_preemptive_cli_write_operations() {
        for op in &[
            "update_issue_comment",
            "delete_issue_comment",
            "create_release",
            "edit_release",
            "delete_release",
            "delete_gist",
        ] {
            assert!(
                is_write_operation(op),
                "{} (pre-emptive CLI) must be classified as a write operation",
                op
            );
        }
    }

    #[test]
    fn test_granular_issue_update_tools_are_read_write_operations() {
        for op in &[
            "update_issue_assignees",
            "update_issue_body",
            "update_issue_labels",
            "update_issue_milestone",
            "update_issue_state",
            "update_issue_title",
            "update_issue_type",
        ] {
            assert!(
                is_read_write_operation(op),
                "{} must be classified as a read-write operation",
                op
            );
            assert!(
                !is_write_operation(op),
                "{} should not be in WRITE_OPERATIONS (it is in READ_WRITE_OPERATIONS)",
                op
            );
        }
    }

    #[test]
    fn test_set_issue_fields_is_read_write_operation() {
        let op = "set_issue_fields";
        assert!(
            is_read_write_operation(op),
            "{} must be classified as a read-write operation",
            op
        );
        assert!(
            !is_write_operation(op),
            "{} should not be in WRITE_OPERATIONS (it is in READ_WRITE_OPERATIONS)",
            op
        );
    }

    #[test]
    fn test_sub_issue_management_tools_are_read_write_operations() {
        for op in &["add_sub_issue", "remove_sub_issue", "reprioritize_sub_issue"] {
            assert!(
                is_read_write_operation(op),
                "{} must be classified as a read-write operation",
                op
            );
            assert!(
                !is_write_operation(op),
                "{} should not be in WRITE_OPERATIONS (it is in READ_WRITE_OPERATIONS)",
                op
            );
        }
    }

    #[test]
    fn test_pr_review_tools_are_read_write_operations() {
        for op in &[
            "add_pull_request_review_comment",
            "create_pull_request_review",
            "delete_pending_pull_request_review",
            "request_pull_request_reviewers",
            "resolve_review_thread",
            "submit_pending_pull_request_review",
            "unresolve_review_thread",
        ] {
            assert!(
                is_read_write_operation(op),
                "{} must be classified as a read-write operation",
                op
            );
            assert!(
                !is_write_operation(op),
                "{} should not be in WRITE_OPERATIONS (it is in READ_WRITE_OPERATIONS)",
                op
            );
        }
    }

    #[test]
    fn test_is_merge_operation() {
        assert!(is_merge_operation("merge_pull_request"));
        assert!(is_merge_operation("merge_upstream"));
        assert!(!is_merge_operation("create_pull_request"));
        assert!(!is_merge_operation("update_pull_request"));
        assert!(!is_merge_operation(""));
    }

    #[test]
    fn test_is_delete_operation() {
        assert!(is_delete_operation("delete_file"));
        assert!(is_delete_operation("delete_branch"));
        assert!(is_delete_operation("delete_release"));
        assert!(!is_delete_operation("create_repository"));
        assert!(!is_delete_operation(""));
    }

    #[test]
    fn test_is_lock_operation() {
        assert!(is_lock_operation("lock_issue"));
        assert!(is_lock_operation("lock_pull_request"));
        assert!(!is_lock_operation("unlock_issue"));
        assert!(!is_lock_operation("create_issue"));
        assert!(!is_lock_operation(""));
    }

    #[test]
    fn test_is_unlock_operation() {
        assert!(is_unlock_operation("unlock_issue"));
        assert!(is_unlock_operation("unlock_pull_request"));
        assert!(!is_unlock_operation("lock_issue"));
        assert!(!is_unlock_operation("create_issue"));
        assert!(!is_unlock_operation(""));
    }

    #[test]
    fn test_lock_and_unlock_contribute_to_write_operations() {
        // is_write_operation delegates to is_lock_operation and is_unlock_operation
        assert!(is_write_operation("lock_issue"));
        assert!(is_write_operation("unlock_issue"));
    }

    #[test]
    fn test_discussion_comment_write_is_write_operation() {
        assert!(
            is_write_operation("discussion_comment_write"),
            "discussion_comment_write must be classified as a write operation"
        );
        assert!(
            !is_read_write_operation("discussion_comment_write"),
            "discussion_comment_write should not be in READ_WRITE_OPERATIONS"
        );
    }

    #[test]
    fn test_granular_pr_update_tools_are_read_write_operations() {
        for op in &[
            "update_pull_request_body",
            "update_pull_request_draft_state",
            "update_pull_request_state",
            "update_pull_request_title",
        ] {
            assert!(
                is_read_write_operation(op),
                "{} must be classified as a read-write operation",
                op
            );
            assert!(
                !is_write_operation(op),
                "{} should not be in WRITE_OPERATIONS (it is in READ_WRITE_OPERATIONS)",
                op
            );
        }
    }
}
