//! Repository permission helpers
//!
//! This module provides functions to query repository permissions
//! using the backend MCP server. This enables dynamic integrity
//! labeling based on actual GitHub permissions.

use crate::labels::{constants::label_constants, MEDIUM_BUFFER_SIZE};
use serde_json::Value;

/// Repository permission level from GitHub
#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord)]
pub enum PermissionLevel {
    None,
    Read,
    Triage,
    Write,
    Maintain,
    Admin,
}

impl PermissionLevel {
    /// Parse permission level from GitHub API string
    pub fn from_str(s: &str) -> Self {
        match s.to_lowercase().as_str() {
            "admin" => PermissionLevel::Admin,
            "maintain" => PermissionLevel::Maintain,
            "write" | "push" => PermissionLevel::Write,
            "triage" => PermissionLevel::Triage,
            "read" | "pull" => PermissionLevel::Read,
            _ => PermissionLevel::None,
        }
    }

    /// Check if this permission level grants maintainer access
    pub fn is_maintainer(&self) -> bool {
        matches!(self, PermissionLevel::Maintain | PermissionLevel::Admin)
    }

    /// Check if this permission level grants contributor (write) access
    pub fn is_contributor(&self) -> bool {
        matches!(
            self,
            PermissionLevel::Write | PermissionLevel::Maintain | PermissionLevel::Admin
        )
    }

    /// Check if this permission level grants read access
    pub fn is_reader(&self) -> bool {
        !matches!(self, PermissionLevel::None)
    }
}

/// Collaborator information from GitHub
#[derive(Debug, Clone)]
pub struct Collaborator {
    pub login: String,
    pub permission: PermissionLevel,
}

/// Repository permission information
#[derive(Debug, Clone, Default)]
pub struct RepoPermissions {
    /// Repository full name (owner/repo)
    pub repo_id: String,
    /// List of collaborators with their permissions
    pub collaborators: Vec<Collaborator>,
    /// Whether permissions were successfully fetched
    pub fetched: bool,
    /// Error message if fetch failed
    pub error: Option<String>,
}

impl RepoPermissions {
    /// Get all maintainers (maintain or admin permission)
    pub fn maintainers(&self) -> impl Iterator<Item = &str> + '_ {
        self.collaborators
            .iter()
            .filter(|c| c.permission.is_maintainer())
            .map(|c| c.login.as_str())
    }

    /// Get all contributors (write, maintain, or admin permission)
    pub fn contributors(&self) -> impl Iterator<Item = &str> + '_ {
        self.collaborators
            .iter()
            .filter(|c| c.permission.is_contributor())
            .map(|c| c.login.as_str())
    }

    /// Check if a user is a maintainer
    pub fn is_maintainer(&self, username: &str) -> bool {
        self.collaborators
            .iter()
            .any(|c| c.login.eq_ignore_ascii_case(username) && c.permission.is_maintainer())
    }

    /// Check if a user is a contributor
    pub fn is_contributor(&self, username: &str) -> bool {
        self.collaborators
            .iter()
            .any(|c| c.login.eq_ignore_ascii_case(username) && c.permission.is_contributor())
    }

    /// Get a user's permission level
    pub fn get_permission(&self, username: &str) -> PermissionLevel {
        self.collaborators
            .iter()
            .find(|c| c.login.eq_ignore_ascii_case(username))
            .map(|c| c.permission.clone())
            .unwrap_or(PermissionLevel::None)
    }
}

/// Fetch repository permissions using the backend MCP server
///
/// This calls the GitHub MCP server to get collaborator information.
/// Note: This requires the token to have appropriate permissions.
///
/// # Arguments
/// * `owner` - Repository owner
/// * `repo` - Repository name
///
/// # Returns
/// * `RepoPermissions` with collaborator information, or error details
pub fn fetch_repo_permissions(owner: &str, repo: &str) -> RepoPermissions {
    let repo_id = format!("{}/{}", owner, repo);

    if owner.is_empty() || repo.is_empty() {
        return RepoPermissions {
            repo_id,
            fetched: false,
            error: Some("Missing owner or repo".to_string()),
            ..Default::default()
        };
    }

    // Prepare the backend call arguments
    // Note: The GitHub MCP server doesn't have a direct "list_collaborators" tool,
    // so we use get_repository to check permissions for the authenticated user,
    // or we could iterate through known users.
    //
    // For a more complete implementation, you would need:
    // 1. A list_collaborators tool in the MCP server, or
    // 2. Use the GitHub REST API directly via a custom tool

    let args = serde_json::json!({
        "owner": owner,
        "repo": repo
    });

    let args_str = args.to_string();
    let mut result_buffer = vec![0u8; MEDIUM_BUFFER_SIZE];

    // Try to get repository info which includes permission for current user
    match crate::invoke_backend("get_repository", &args_str, &mut result_buffer) {
        Ok(len) => {
            if len == 0 {
                return RepoPermissions {
                    repo_id,
                    fetched: false,
                    error: Some("Empty response from backend".to_string()),
                    ..Default::default()
                };
            }

            // Parse the response
            let response_str = match std::str::from_utf8(&result_buffer[..len]) {
                Ok(s) => s,
                Err(e) => {
                    return RepoPermissions {
                        repo_id,
                        fetched: false,
                        error: Some(format!("Invalid UTF-8 response: {}", e)),
                        ..Default::default()
                    };
                }
            };

            match serde_json::from_str::<Value>(response_str) {
                Ok(repo_data) => parse_repo_permissions(&repo_id, &repo_data),
                Err(e) => RepoPermissions {
                    repo_id,
                    fetched: false,
                    error: Some(format!("Failed to parse response: {}", e)),
                    ..Default::default()
                },
            }
        }
        Err(code) => RepoPermissions {
            repo_id,
            fetched: false,
            error: Some(format!("Backend call failed with code {}", code)),
            ..Default::default()
        },
    }
}

/// Parse repository data to extract permission information
fn parse_repo_permissions(repo_id: &str, repo_data: &Value) -> RepoPermissions {
    let mut permissions = RepoPermissions {
        repo_id: repo_id.to_string(),
        fetched: true,
        ..Default::default()
    };

    // Extract permissions object if present
    // GitHub API returns: { "permissions": { "admin": true, "maintain": false, ... } }
    if let Some(perms) = repo_data.get("permissions").and_then(|p| p.as_object()) {
        // Determine the highest permission level for the authenticated user
        let level = if perms
            .get("admin")
            .and_then(|v| v.as_bool())
            .unwrap_or(false)
        {
            PermissionLevel::Admin
        } else if perms
            .get("maintain")
            .and_then(|v| v.as_bool())
            .unwrap_or(false)
        {
            PermissionLevel::Maintain
        } else if perms.get("push").and_then(|v| v.as_bool()).unwrap_or(false) {
            PermissionLevel::Write
        } else if perms
            .get("triage")
            .and_then(|v| v.as_bool())
            .unwrap_or(false)
        {
            PermissionLevel::Triage
        } else if perms.get("pull").and_then(|v| v.as_bool()).unwrap_or(false) {
            PermissionLevel::Read
        } else {
            PermissionLevel::None
        };

        // Get the owner's login - they are always admin
        if let Some(owner) = repo_data
            .get("owner")
            .and_then(|o| o.get("login"))
            .and_then(|l| l.as_str())
        {
            permissions.collaborators.push(Collaborator {
                login: owner.to_string(),
                permission: PermissionLevel::Admin,
            });
        }

        // Note: The current user's permission is implicit from the token used
        // We can't get a full list of collaborators without the list_collaborators API

        crate::log_debug(&format!(
            "Parsed repo permissions for {}: authenticated user has {:?} access",
            repo_id, level
        ));
    }

    permissions
}

/// Determine integrity tags for an author based on repository permissions
///
/// This is a helper that returns appropriate integrity tags based on
/// whether the author is a contributor or external user.
///
/// # Design Note
/// Returns `Vec<String>` rather than an iterator because it creates owned
/// String data that must be allocated. The Vec is immediately consumed in
/// all usage sites. See the comment in labels.rs for more details on when
/// to use Vec vs Iterator.
///
/// # Arguments
/// * `author` - Username of the content author
/// * `owner` - Repository owner
/// * `repo` - Repository name
///
/// # Returns
/// * Vector of integrity tag strings (0-2 items)
pub fn get_author_integrity_tags(author: &str, owner: &str, repo: &str) -> Vec<String> {
    if author.is_empty() || owner.is_empty() || repo.is_empty() {
        return vec![];
    }

    let repo_id = format!("{}/{}", owner, repo);

    // Check if author is the repo owner (always has highest integrity)
    if author.eq_ignore_ascii_case(owner) {
        return vec![
            format!("{}{}", label_constants::READER_PREFIX, repo_id),
            format!("{}{}", label_constants::WRITER_PREFIX, repo_id),
        ];
    }

    // For non-owners, we would need to fetch permissions
    // This is expensive, so we return a conservative default
    // In a production system, you might cache these lookups

    // Default: assume contributor level for any authenticated user
    // This is conservative - actual permission check would be more accurate
    vec![format!("{}{}", label_constants::READER_PREFIX, repo_id)]
}

/// Get integrity tags for a bot account
///
/// Bots typically have approved-level integrity for their automated actions.
///
/// # Design Note
/// Returns `Vec<String>` rather than an iterator because it creates owned
/// String data. See get_author_integrity_tags() and labels.rs for details
/// on Vec vs Iterator design decisions.
///
/// # Returns
/// * Vector of integrity tag strings (1-2 items)
pub fn get_bot_integrity_tags(bot_name: &str, owner: &str, repo: &str) -> Vec<String> {
    let repo_id = format!("{}/{}", owner, repo);

    // Well-known GitHub bots have approved-level integrity
    let lower = bot_name.to_lowercase();
    if lower.contains("dependabot")
        || lower.contains("renovate")
        || lower.contains("github-actions")
        || lower.contains("copilot")
    {
        vec![
            format!("{}{}", label_constants::READER_PREFIX, repo_id),
            format!("{}{}", label_constants::WRITER_PREFIX, repo_id),
        ]
    } else {
        // Unknown bots get contributor level
        vec![format!("{}{}", label_constants::READER_PREFIX, repo_id)]
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::labels::is_bot;

    #[test]
    fn test_permission_level_parsing() {
        assert_eq!(PermissionLevel::from_str("admin"), PermissionLevel::Admin);
        assert_eq!(PermissionLevel::from_str("ADMIN"), PermissionLevel::Admin);
        assert_eq!(
            PermissionLevel::from_str("maintain"),
            PermissionLevel::Maintain
        );
        assert_eq!(PermissionLevel::from_str("write"), PermissionLevel::Write);
        assert_eq!(PermissionLevel::from_str("push"), PermissionLevel::Write);
        assert_eq!(PermissionLevel::from_str("read"), PermissionLevel::Read);
        assert_eq!(PermissionLevel::from_str("pull"), PermissionLevel::Read);
        assert_eq!(PermissionLevel::from_str("unknown"), PermissionLevel::None);
    }

    #[test]
    fn test_permission_level_checks() {
        assert!(PermissionLevel::Admin.is_maintainer());
        assert!(PermissionLevel::Maintain.is_maintainer());
        assert!(!PermissionLevel::Write.is_maintainer());

        assert!(PermissionLevel::Admin.is_contributor());
        assert!(PermissionLevel::Write.is_contributor());
        assert!(!PermissionLevel::Read.is_contributor());
    }

    #[test]
    fn test_bot_detection() {
        // Test the canonical function from labels module
        assert!(is_bot("dependabot[bot]"));
        assert!(is_bot("renovate-bot"));
        assert!(is_bot("github-actions"));
        assert!(!is_bot("octocat"));
    }

    #[test]
    fn test_maintainers_and_contributors_iterators() {
        // Create test permissions with various collaborators
        let perms = RepoPermissions {
            repo_id: "test/repo".to_string(),
            collaborators: vec![
                Collaborator {
                    login: "admin_user".to_string(),
                    permission: PermissionLevel::Admin,
                },
                Collaborator {
                    login: "maintainer_user".to_string(),
                    permission: PermissionLevel::Maintain,
                },
                Collaborator {
                    login: "writer_user".to_string(),
                    permission: PermissionLevel::Write,
                },
                Collaborator {
                    login: "reader_user".to_string(),
                    permission: PermissionLevel::Read,
                },
            ],
            fetched: true,
            error: None,
        };

        // Test maintainers() returns an iterator
        let maintainers: Vec<&str> = perms.maintainers().collect();
        assert_eq!(maintainers.len(), 2);
        assert!(maintainers.contains(&"admin_user"));
        assert!(maintainers.contains(&"maintainer_user"));

        // Test contributors() returns an iterator
        let contributors: Vec<&str> = perms.contributors().collect();
        assert_eq!(contributors.len(), 3);
        assert!(contributors.contains(&"admin_user"));
        assert!(contributors.contains(&"maintainer_user"));
        assert!(contributors.contains(&"writer_user"));
        assert!(!contributors.contains(&"reader_user"));

        // Test that iterators can be used without collecting
        assert_eq!(perms.maintainers().count(), 2);
        assert_eq!(perms.contributors().count(), 3);

        // Test that iterators can be chained
        let first_maintainer = perms.maintainers().next();
        assert!(first_maintainer.is_some());
    }
}
