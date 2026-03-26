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
    "add_comment_to_pending_review",
    "add_reply_to_pull_request_comment",
    "request_copilot_review",
    "add_issue_comment",
    "assign_copilot_to_issue",
    "run_workflow",
    "rerun_workflow_run",
    "rerun_failed_jobs",
    "cancel_workflow_run",
    "delete_workflow_run_logs",
    "actions_run_trigger",
    "create_gist",
    "dismiss_notification",
    "mark_all_notifications_read",
    "manage_notification_subscription",
    "manage_repository_notification_subscription",
    "add_project_item",
    "delete_project_item",
    "projects_write",
    "star_repository",
    "unstar_repository",
    "label_write",
    "create_issue",
    // Dynamically enables additional toolsets, expanding the agent's capability set
    "enable_toolset",
    // Pre-emptive entries for anticipated future MCP tools (no equivalent tool today)
    "archive_repository", // gh repo archive
    "transfer_issue",     // gh issue transfer
    "enable_workflow",    // gh workflow enable
    "disable_workflow",   // gh workflow disable
    "set_secret",         // gh secret set
    "set_variable",         // gh variable set
    "upload_release_asset", // gh release upload
    "sync_fork",            // gh repo sync
];

/// Read-write operations that both read and modify data
pub const READ_WRITE_OPERATIONS: &[&str] = &[
    "merge_pull_request",
    "update_pull_request",
    "update_pull_request_branch",
    "pull_request_review_write",
    "issue_write",
    "sub_issue_write",
    "update_gist",
    "update_project_item",
    "create_pull_request_with_copilot",
    "update_issue",
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

/// Check if a tool is an update operation
pub fn is_update_operation(tool_name: &str) -> bool {
    tool_name.starts_with("update_")
}

/// Check if a tool is a create operation
pub fn is_create_operation(tool_name: &str) -> bool {
    tool_name.starts_with("create_")
}

/// Check if a tool is a lock operation
pub fn is_lock_operation(tool_name: &str) -> bool {
    tool_name.starts_with("lock_")
}

/// Check if a tool is an unlock operation
pub fn is_unlock_operation(tool_name: &str) -> bool {
    tool_name.starts_with("unlock_")
}
