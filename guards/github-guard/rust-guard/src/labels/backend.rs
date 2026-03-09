//! Backend API calls for contributor verification
//!
//! This module handles calls to backend services to verify user status
//! and retrieve additional information needed for labeling.

use serde_json::Value;
use std::collections::HashMap;
use std::sync::{Mutex, OnceLock};
use std::thread;
use std::time::Duration;

use super::constants::{MEDIUM_BUFFER_SIZE, SMALL_BUFFER_SIZE};

/// Backend callback signature used for GitHub MCP tool calls.
pub type GithubMcpCallback = fn(&str, &str, &mut [u8]) -> Result<usize, i32>;

fn repo_visibility_cache() -> &'static Mutex<HashMap<String, bool>> {
    static CACHE: OnceLock<Mutex<HashMap<String, bool>>> = OnceLock::new();
    CACHE.get_or_init(|| Mutex::new(HashMap::new()))
}

fn get_cached_repo_visibility(repo_id: &str) -> Option<bool> {
    repo_visibility_cache()
        .lock()
        .ok()
        .and_then(|cache| cache.get(repo_id).copied())
}

fn set_cached_repo_visibility(repo_id: &str, is_private: bool) {
    if let Ok(mut cache) = repo_visibility_cache().lock() {
        cache.insert(repo_id.to_string(), is_private);
    }
}

#[derive(Debug, Clone)]
pub struct PullRequestFacts {
    pub author_association: Option<String>,
    pub is_merged: bool,
    pub is_forked: Option<bool>,
}

/// Check if a user has any merged pull requests in a repository.
///
/// This is used to determine contributor status for off-branch content
/// like issues and comments. A user with merged PRs is considered a
/// verified contributor.
///
/// Uses the search_pull_requests tool with query:
/// `author:<username> repo:<owner>/<repo> is:merged`
///
/// # Arguments
/// * `username` - The GitHub username to check
/// * `owner` - Repository owner
/// * `repo` - Repository name
///
/// # Returns
/// * `Some(count)` - Number of merged PRs (0 if none)
/// * `None` - If the backend call failed
#[allow(dead_code)]
pub fn count_merged_prs(username: &str, owner: &str, repo: &str) -> Option<u32> {
    if username.is_empty() || owner.is_empty() || repo.is_empty() {
        return Some(0);
    }

    // Build the search query
    let query = format!("author:{} repo:{}/{} is:merged", username, owner, repo);
    let args = serde_json::json!({
        "query": query,
        "perPage": 1  // We only need the count, not the actual PRs
    });

    let args_str = args.to_string();
    let mut result_buffer = vec![0u8; SMALL_BUFFER_SIZE];

    crate::log_debug(&format!(
        "Checking merged PRs for {} in {}/{}",
        username, owner, repo
    ));

    match crate::invoke_backend("search_pull_requests", &args_str, &mut result_buffer) {
        Ok(len) => {
            if len == 0 {
                crate::log_debug("Empty response from search_pull_requests");
                return None;
            }

            // Parse the response
            let response_str = match std::str::from_utf8(&result_buffer[..len]) {
                Ok(s) => s,
                Err(_) => return None,
            };

            // Parse JSON and extract total_count
            if let Ok(response) = serde_json::from_str::<Value>(response_str) {
                if let Some(count) = response.get("total_count").and_then(|v| v.as_u64()) {
                    crate::log_debug(&format!(
                        "User {} has {} merged PRs in {}/{}",
                        username, count, owner, repo
                    ));
                    return Some(count as u32);
                }
            }

            None
        }
        Err(code) => {
            crate::log_warn(&format!("Failed to check merged PRs: error code {}", code));
            None
        }
    }
}

/// Check if a user is a verified contributor (has at least one merged PR)
///
/// # Arguments
/// * `username` - The GitHub username to check
/// * `owner` - Repository owner
/// * `repo` - Repository name
///
/// # Returns
/// * `true` if user has at least one merged PR
/// * `false` if user has no merged PRs or check failed
#[allow(dead_code)]
pub fn is_verified_contributor(username: &str, owner: &str, repo: &str) -> bool {
    count_merged_prs(username, owner, repo).unwrap_or(0) > 0
}

/// Check whether a repository is private using the backend MCP server.
///
/// Returns:
/// - `Some(true)` if repository is private
/// - `Some(false)` if repository is public
/// - `None` if visibility could not be determined
#[allow(dead_code)]
pub fn is_repo_private(owner: &str, repo: &str) -> Option<bool> {
    is_repo_private_with_callback(crate::invoke_backend, owner, repo)
}

pub fn is_repo_private_with_callback(
    callback: GithubMcpCallback,
    owner: &str,
    repo: &str,
) -> Option<bool> {
    if owner.is_empty() || repo.is_empty() {
        crate::log_warn("Repo visibility lookup skipped: owner or repo is empty");
        return None;
    }

    let repo_id = format!("{}/{}", owner, repo);

    if let Some(is_private) = get_cached_repo_visibility(&repo_id) {
        crate::log_debug(&format!(
            "Repo visibility lookup cache hit for {}: {}",
            repo_id,
            if is_private { "private" } else { "public" }
        ));
        return Some(is_private);
    }

    // Use exact repository scoping for visibility lookup.
    let query = format!("repo:{}", repo_id);
    let args = serde_json::json!({
        "query": query,
        "perPage": 10
    });

    let args_str = args.to_string();

    crate::log_debug(&format!("Checking repo visibility for {}", repo_id));

    let mut did_retry_after_rate_limit = false;

    for attempt in 0..=1 {
        let mut result_buffer = vec![0u8; MEDIUM_BUFFER_SIZE];

        let len = match callback("search_repositories", &args_str, &mut result_buffer) {
            Ok(len) if len > 0 => len,
            Ok(_) => {
                crate::log_warn(&format!(
                    "Repo visibility lookup result for {}: unknown (empty search response)",
                    repo_id
                ));
                return get_cached_repo_visibility(&repo_id);
            }
            Err(code) => {
                crate::log_warn(&format!(
                    "Repo visibility lookup result for {}: unknown (backend error code {})",
                    repo_id, code
                ));
                return get_cached_repo_visibility(&repo_id);
            }
        };

        let response_str = match std::str::from_utf8(&result_buffer[..len]) {
            Ok(value) => value,
            Err(_) => {
                crate::log_warn(&format!(
                    "Repo visibility lookup result for {}: unknown (invalid UTF-8 response)",
                    repo_id
                ));
                return get_cached_repo_visibility(&repo_id);
            }
        };

        let response = match serde_json::from_str::<Value>(response_str) {
            Ok(value) => value,
            Err(_) => {
                crate::log_warn(&format!(
                    "Repo visibility lookup result for {}: unknown (invalid JSON response)",
                    repo_id
                ));
                return get_cached_repo_visibility(&repo_id);
            }
        };

        if let Some(error_text) = extract_backend_error_text(&response) {
            if is_rate_limit_error(error_text) {
                if let Some(is_private) = get_cached_repo_visibility(&repo_id) {
                    crate::log_warn(&format!(
                        "Repo visibility lookup result for {}: using cached {} after rate-limit error",
                        repo_id,
                        if is_private { "private" } else { "public" }
                    ));
                    return Some(is_private);
                }

                if attempt == 0 {
                    if let Some(wait_secs) = extract_rate_reset_seconds(error_text) {
                        let wait_secs = wait_secs.min(2);
                        if wait_secs > 0 {
                            crate::log_warn(&format!(
                                "Repo visibility lookup rate-limited for {}: waiting {}s and retrying once",
                                repo_id, wait_secs
                            ));
                            thread::sleep(Duration::from_secs(wait_secs));
                        } else {
                            crate::log_warn(&format!(
                                "Repo visibility lookup rate-limited for {}: retrying once immediately",
                                repo_id
                            ));
                        }
                        did_retry_after_rate_limit = true;
                        continue;
                    }
                }

                crate::log_warn(&format!(
                    "Repo visibility lookup result for {}: unknown (rate-limited and no cached visibility)",
                    repo_id
                ));
                return None;
            }
        }

        let result = extract_repo_private_flag(&response, &repo_id);
        match result {
            Some(true) => {
                set_cached_repo_visibility(&repo_id, true);
                crate::log_info(&format!(
                    "Repo visibility lookup result for {}: private",
                    repo_id
                ));
                return Some(true);
            }
            Some(false) => {
                set_cached_repo_visibility(&repo_id, false);
                crate::log_info(&format!(
                    "Repo visibility lookup result for {}: public",
                    repo_id
                ));
                return Some(false);
            }
            None => {
                if did_retry_after_rate_limit {
                    crate::log_warn(&format!(
                        "Repo visibility lookup result for {}: unknown after rate-limit retry (no matching visibility fields found)",
                        repo_id
                    ));
                } else {
                    crate::log_warn(&format!(
                        "Repo visibility lookup result for {}: unknown (no matching visibility fields found)",
                        repo_id
                    ));
                }
                return get_cached_repo_visibility(&repo_id);
            }
        }
    }

    get_cached_repo_visibility(&repo_id)
}

/// Determine whether a pull request is from a fork.
///
/// This helper calls `get_pull_request` through the provided backend callback,
/// extracts `base.repo.full_name` and `head.repo.full_name`, and returns:
/// - `Some(true)` if the PR is from a fork (head repo differs from base repo)
/// - `Some(false)` if the PR is direct (same repository)
/// - `None` if the result cannot be determined
pub fn is_forked_pull_request_with_callback(
    callback: GithubMcpCallback,
    owner: &str,
    repo: &str,
    pull_number: &str,
) -> Option<bool> {
    if owner.is_empty() || repo.is_empty() || pull_number.is_empty() {
        return None;
    }

    let args = serde_json::json!({
        "owner": owner,
        "repo": repo,
        "pull_number": pull_number,
    });

    let args_str = args.to_string();
    let mut result_buffer = vec![0u8; SMALL_BUFFER_SIZE];

    crate::log_debug(&format!(
        "Checking PR origin for {}/{}#{}",
        owner, repo, pull_number
    ));

    let len = match callback("get_pull_request", &args_str, &mut result_buffer) {
        Ok(len) if len > 0 => len,
        Ok(_) => return None,
        Err(code) => {
            crate::log_warn(&format!(
                "Failed to check PR origin for {}/{}#{}: error code {}",
                owner, repo, pull_number, code
            ));
            return None;
        }
    };

    let response_str = std::str::from_utf8(&result_buffer[..len]).ok()?;
    let response = serde_json::from_str::<Value>(response_str).ok()?;
    let pr = extract_mcp_payload_json(&response);

    let base_full_name = pr
        .get("base")
        .and_then(|b| b.get("repo"))
        .and_then(|r| r.get("full_name"))
        .and_then(|v| v.as_str());

    let head_full_name = pr
        .get("head")
        .and_then(|h| h.get("repo"))
        .and_then(|r| r.get("full_name"))
        .and_then(|v| v.as_str());

    match (base_full_name, head_full_name) {
        (Some(base), Some(head)) if !base.is_empty() && !head.is_empty() => {
            Some(!base.eq_ignore_ascii_case(head))
        }
        _ => None,
    }
}

/// Fetch pull request facts used for integrity derivation.
pub fn get_pull_request_facts_with_callback(
    callback: GithubMcpCallback,
    owner: &str,
    repo: &str,
    pull_number: &str,
) -> Option<PullRequestFacts> {
    if owner.is_empty() || repo.is_empty() || pull_number.is_empty() {
        return None;
    }

    let args = serde_json::json!({
        "owner": owner,
        "repo": repo,
        "pull_number": pull_number,
    });

    let args_str = args.to_string();
    let mut result_buffer = vec![0u8; SMALL_BUFFER_SIZE];

    let len = match callback("get_pull_request", &args_str, &mut result_buffer) {
        Ok(len) if len > 0 => len,
        _ => return None,
    };

    let response_str = std::str::from_utf8(&result_buffer[..len]).ok()?;
    let response = serde_json::from_str::<Value>(response_str).ok()?;
    let pr = extract_mcp_payload_json(&response);

    let base_full_name = pr
        .get("base")
        .and_then(|b| b.get("repo"))
        .and_then(|r| r.get("full_name"))
        .and_then(|v| v.as_str());

    let head_full_name = pr
        .get("head")
        .and_then(|h| h.get("repo"))
        .and_then(|r| r.get("full_name"))
        .and_then(|v| v.as_str());

    let is_forked = match (base_full_name, head_full_name) {
        (Some(base), Some(head)) if !base.is_empty() && !head.is_empty() => {
            Some(!base.eq_ignore_ascii_case(head))
        }
        _ => None,
    };

    let is_merged = pr
        .get("merged_at")
        .map(|v| !v.is_null())
        .or_else(|| pr.get("merged").and_then(|v| v.as_bool()))
        .unwrap_or(false);

    let author_association = pr
        .get("author_association")
        .or_else(|| pr.get("authorAssociation"))
        .and_then(|v| v.as_str())
        .map(String::from);

    Some(PullRequestFacts {
        author_association,
        is_merged,
        is_forked,
    })
}

/// Fetch issue author_association value for resource-level initialization.
pub fn get_issue_author_association_with_callback(
    callback: GithubMcpCallback,
    owner: &str,
    repo: &str,
    issue_number: &str,
) -> Option<String> {
    if owner.is_empty() || repo.is_empty() || issue_number.is_empty() {
        return None;
    }

    let args = serde_json::json!({
        "owner": owner,
        "repo": repo,
        "issue_number": issue_number,
    });

    let args_str = args.to_string();
    let mut result_buffer = vec![0u8; SMALL_BUFFER_SIZE];

    let len = match callback("issue_read", &args_str, &mut result_buffer) {
        Ok(len) if len > 0 => len,
        _ => return None,
    };

    let response_str = std::str::from_utf8(&result_buffer[..len]).ok()?;
    let response = serde_json::from_str::<Value>(response_str).ok()?;
    let issue = extract_mcp_payload_json(&response);

    issue
        .get("author_association")
        .or_else(|| issue.get("authorAssociation"))
        .and_then(|v| v.as_str())
        .map(String::from)
}

pub fn get_pull_request_facts(
    owner: &str,
    repo: &str,
    pull_number: &str,
) -> Option<PullRequestFacts> {
    get_pull_request_facts_with_callback(crate::invoke_backend, owner, repo, pull_number)
}

pub fn get_issue_author_association(owner: &str, repo: &str, issue_number: &str) -> Option<String> {
    get_issue_author_association_with_callback(crate::invoke_backend, owner, repo, issue_number)
}

/// Determine whether a pull request is from a fork using the default backend callback.
#[allow(dead_code)]
pub fn is_forked_pull_request(owner: &str, repo: &str, pull_number: &str) -> Option<bool> {
    is_forked_pull_request_with_callback(crate::invoke_backend, owner, repo, pull_number)
}

fn extract_repo_private_flag(response: &Value, repo_id: &str) -> Option<bool> {
    // Direct object response
    if let Some(is_private) = repo_visibility_from_items(response, repo_id) {
        return Some(is_private);
    }

    // Some MCP backends return { content: [{ text: "{...json...}" }] }
    let text_payload = response
        .get("content")
        .and_then(|v| v.as_array())
        .and_then(|arr| arr.first())
        .and_then(|item| item.get("text"))
        .and_then(|v| v.as_str())?;

    let parsed = serde_json::from_str::<Value>(text_payload).ok()?;
    repo_visibility_from_items(&parsed, repo_id)
}

fn extract_mcp_payload_json(response: &Value) -> Value {
    if let Some(content) = response.get("content").and_then(|v| v.as_array()) {
        if let Some(text) = content
            .first()
            .and_then(|item| item.get("text"))
            .and_then(|v| v.as_str())
        {
            if let Ok(parsed) = serde_json::from_str::<Value>(text) {
                return parsed;
            }
        }
    }

    response.clone()
}

fn extract_backend_error_text(response: &Value) -> Option<&str> {
    if response.get("isError").and_then(|v| v.as_bool()) != Some(true) {
        return None;
    }

    response
        .get("content")
        .and_then(|v| v.as_array())
        .and_then(|arr| arr.first())
        .and_then(|item| item.get("text"))
        .and_then(|v| v.as_str())
}

fn is_rate_limit_error(error_text: &str) -> bool {
    let lower = error_text.to_ascii_lowercase();
    lower.contains("rate limit") || lower.contains("secondary rate limit")
}

fn extract_rate_reset_seconds(error_text: &str) -> Option<u64> {
    let marker = "[rate reset in ";
    let start = error_text.find(marker)? + marker.len();
    let rest = &error_text[start..];

    let mut digits = String::new();
    for ch in rest.chars() {
        if ch.is_ascii_digit() {
            digits.push(ch);
            continue;
        }
        break;
    }

    if digits.is_empty() {
        return None;
    }

    digits.parse::<u64>().ok()
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::{AtomicUsize, Ordering};

    fn copy_payload(payload: Value, buffer: &mut [u8]) -> Result<usize, i32> {
        let serialized = payload.to_string();
        let bytes = serialized.as_bytes();
        buffer[..bytes.len()].copy_from_slice(bytes);
        Ok(bytes.len())
    }

    fn direct_pr_callback(_tool: &str, _args: &str, buffer: &mut [u8]) -> Result<usize, i32> {
        let payload = serde_json::json!({
            "base": { "repo": { "full_name": "owner/repo" } },
            "head": { "repo": { "full_name": "owner/repo" } }
        })
        .to_string();
        let bytes = payload.as_bytes();
        buffer[..bytes.len()].copy_from_slice(bytes);
        Ok(bytes.len())
    }

    fn fork_pr_callback(_tool: &str, _args: &str, buffer: &mut [u8]) -> Result<usize, i32> {
        let payload = serde_json::json!({
            "base": { "repo": { "full_name": "owner/repo" } },
            "head": { "repo": { "full_name": "contrib/repo" } }
        })
        .to_string();
        let bytes = payload.as_bytes();
        buffer[..bytes.len()].copy_from_slice(bytes);
        Ok(bytes.len())
    }

    fn wrapped_fork_pr_callback(_tool: &str, _args: &str, buffer: &mut [u8]) -> Result<usize, i32> {
        let inner = serde_json::json!({
            "base": { "repo": { "full_name": "owner/repo" } },
            "head": { "repo": { "full_name": "fork/repo" } }
        })
        .to_string();
        let payload = serde_json::json!({
            "content": [{ "type": "text", "text": inner }]
        })
        .to_string();
        let bytes = payload.as_bytes();
        buffer[..bytes.len()].copy_from_slice(bytes);
        Ok(bytes.len())
    }

    fn repo_private_search_fallback_callback(
        tool: &str,
        _args: &str,
        buffer: &mut [u8],
    ) -> Result<usize, i32> {
        match tool {
            "search_repositories" => copy_payload(
                serde_json::json!({
                    "items": [
                        {
                            "full_name": "lpcox/github-guard",
                            "private": true
                        }
                    ]
                }),
                buffer,
            ),
            _ => Err(-1),
        }
    }

    fn repo_public_visibility_search_fallback_callback(
        tool: &str,
        _args: &str,
        buffer: &mut [u8],
    ) -> Result<usize, i32> {
        match tool {
            "search_repositories" => copy_payload(
                serde_json::json!({
                    "items": [
                        {
                            "full_name": "octocat/Hello-World",
                            "visibility": "public"
                        }
                    ]
                }),
                buffer,
            ),
            _ => Err(-1),
        }
    }

    fn repo_public_owner_name_search_fallback_callback(
        tool: &str,
        _args: &str,
        buffer: &mut [u8],
    ) -> Result<usize, i32> {
        match tool {
            "search_repositories" => copy_payload(
                serde_json::json!({
                    "items": [
                        {
                            "owner": { "login": "octocat" },
                            "name": "Hello-World",
                            "isPrivate": false
                        }
                    ]
                }),
                buffer,
            ),
            _ => Err(-1),
        }
    }

    fn clear_repo_visibility_cache_for_tests() {
        if let Ok(mut cache) = repo_visibility_cache().lock() {
            cache.clear();
        }
    }

    static RATE_LIMIT_RETRY_CALL_COUNT: AtomicUsize = AtomicUsize::new(0);

    fn rate_limited_then_public_search_callback(
        tool: &str,
        _args: &str,
        buffer: &mut [u8],
    ) -> Result<usize, i32> {
        if tool != "search_repositories" {
            return Err(-1);
        }

        let call = RATE_LIMIT_RETRY_CALL_COUNT.fetch_add(1, Ordering::SeqCst);
        if call == 0 {
            return copy_payload(
                serde_json::json!({
                    "content": [
                        {
                            "type": "text",
                            "text": "failed to search repositories: 403 API rate limit exceeded [rate reset in 0s]"
                        }
                    ],
                    "isError": true
                }),
                buffer,
            );
        }

        copy_payload(
            serde_json::json!({
                "items": [
                    {
                        "full_name": "rl-owner/rl-repo",
                        "private": false
                    }
                ]
            }),
            buffer,
        )
    }

    fn repo_public_cache_repo_callback(
        tool: &str,
        _args: &str,
        buffer: &mut [u8],
    ) -> Result<usize, i32> {
        match tool {
            "search_repositories" => copy_payload(
                serde_json::json!({
                    "items": [
                        {
                            "full_name": "cache-owner/cache-repo",
                            "private": false
                        }
                    ]
                }),
                buffer,
            ),
            _ => Err(-1),
        }
    }

    fn always_rate_limited_callback(
        tool: &str,
        _args: &str,
        buffer: &mut [u8],
    ) -> Result<usize, i32> {
        if tool != "search_repositories" {
            return Err(-1);
        }

        copy_payload(
            serde_json::json!({
                "content": [
                    {
                        "type": "text",
                        "text": "failed to search repositories: 403 API rate limit exceeded [rate reset in 0s]"
                    }
                ],
                "isError": true
            }),
            buffer,
        )
    }

    #[test]
    fn test_is_forked_pull_request_direct() {
        let result =
            is_forked_pull_request_with_callback(direct_pr_callback, "owner", "repo", "123");
        assert_eq!(result, Some(false));
    }

    #[test]
    fn test_is_forked_pull_request_forked() {
        let result = is_forked_pull_request_with_callback(fork_pr_callback, "owner", "repo", "123");
        assert_eq!(result, Some(true));
    }

    #[test]
    fn test_is_forked_pull_request_wrapped_response() {
        let result =
            is_forked_pull_request_with_callback(wrapped_fork_pr_callback, "owner", "repo", "123");
        assert_eq!(result, Some(true));
    }

    #[test]
    fn test_is_repo_private_falls_back_to_search() {
        let result = is_repo_private_with_callback(
            repo_private_search_fallback_callback,
            "lpcox",
            "github-guard",
        );
        assert_eq!(result, Some(true));
    }

    #[test]
    fn test_is_repo_private_uses_visibility_field_from_search() {
        let result = is_repo_private_with_callback(
            repo_public_visibility_search_fallback_callback,
            "octocat",
            "Hello-World",
        );
        assert_eq!(result, Some(false));
    }

    #[test]
    fn test_is_repo_private_matches_owner_name_shape() {
        let result = is_repo_private_with_callback(
            repo_public_owner_name_search_fallback_callback,
            "octocat",
            "Hello-World",
        );
        assert_eq!(result, Some(false));
    }

    #[test]
    fn test_is_repo_private_retries_once_after_rate_limit_error() {
        clear_repo_visibility_cache_for_tests();
        RATE_LIMIT_RETRY_CALL_COUNT.store(0, Ordering::SeqCst);

        let result = is_repo_private_with_callback(
            rate_limited_then_public_search_callback,
            "rl-owner",
            "rl-repo",
        );
        assert_eq!(result, Some(false));
    }

    #[test]
    fn test_is_repo_private_uses_cached_visibility_when_rate_limited() {
        clear_repo_visibility_cache_for_tests();

        let first = is_repo_private_with_callback(
            repo_public_cache_repo_callback,
            "cache-owner",
            "cache-repo",
        );
        assert_eq!(first, Some(false));

        let second = is_repo_private_with_callback(
            always_rate_limited_callback,
            "cache-owner",
            "cache-repo",
        );
        assert_eq!(second, Some(false));
    }
}

fn repo_visibility_from_items(value: &Value, repo_id: &str) -> Option<bool> {
    // search_repositories shape: { items: [...] }
    if let Some(items) = value.get("items").and_then(|v| v.as_array()) {
        for item in items {
            let item_repo_id = repo_id_from_repo_object(item);
            if item_repo_id
                .as_deref()
                .map(|id| id.eq_ignore_ascii_case(repo_id))
                .unwrap_or(false)
            {
                return private_flag_from_repo_object(item);
            }
        }
    }

    // Also support plain array responses
    if let Some(items) = value.as_array() {
        for item in items {
            let item_repo_id = repo_id_from_repo_object(item);
            if item_repo_id
                .as_deref()
                .map(|id| id.eq_ignore_ascii_case(repo_id))
                .unwrap_or(false)
            {
                return private_flag_from_repo_object(item);
            }
        }
    }

    // Sometimes a direct single-object response may be returned
    let item_repo_id = repo_id_from_repo_object(value);
    if item_repo_id
        .as_deref()
        .map(|id| id.eq_ignore_ascii_case(repo_id))
        .unwrap_or(false)
    {
        return private_flag_from_repo_object(value);
    }

    None
}

fn private_flag_from_repo_object(item: &Value) -> Option<bool> {
    if let Some(is_private) = item.get("private").and_then(|v| v.as_bool()) {
        return Some(is_private);
    }

    if let Some(is_private) = item.get("is_private").and_then(|v| v.as_bool()) {
        return Some(is_private);
    }

    if let Some(is_private) = item.get("isPrivate").and_then(|v| v.as_bool()) {
        return Some(is_private);
    }

    if let Some(visibility) = item.get("visibility").and_then(|v| v.as_str()) {
        if visibility.eq_ignore_ascii_case("private") || visibility.eq_ignore_ascii_case("internal")
        {
            return Some(true);
        }
        if visibility.eq_ignore_ascii_case("public") {
            return Some(false);
        }
    }

    None
}

fn repo_id_from_repo_object(item: &Value) -> Option<String> {
    if let Some(full_name) = item.get("full_name").and_then(|v| v.as_str()) {
        if !full_name.is_empty() {
            return Some(full_name.to_string());
        }
    }

    if let Some(full_name) = item.get("fullName").and_then(|v| v.as_str()) {
        if !full_name.is_empty() {
            return Some(full_name.to_string());
        }
    }

    let name = item.get("name").and_then(|v| v.as_str()).unwrap_or("");
    if name.is_empty() {
        return None;
    }

    if let Some(owner_login) = item
        .get("owner")
        .and_then(|owner| owner.get("login"))
        .and_then(|v| v.as_str())
    {
        if !owner_login.is_empty() {
            return Some(format!("{}/{}", owner_login, name));
        }
    }

    if let Some(owner_name) = item.get("owner").and_then(|v| v.as_str()) {
        if !owner_name.is_empty() {
            return Some(format!("{}/{}", owner_name, name));
        }
    }

    None
}
