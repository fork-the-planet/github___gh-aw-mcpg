//! Helper functions for label generation
//!
//! This module contains utility functions used across the labeling system,
//! including JSON extraction, integrity determination, and common operations.

use serde_json::Value;

use super::constants::{field_names, label_constants};

/// Extract a resource number from a JSON item, returning the number as a string.
/// Checks the `number` field first, then falls back to extracting the trailing
/// number segment from `html_url` or `url` (e.g. `.../issues/123` → `123`).
/// Returns "unknown" (with a log warning) if no number can be determined.
pub(crate) fn extract_resource_number(item: &Value, resource_type: &str, repo: &str) -> String {
    if let Some(n) = item.get("number").and_then(|v| v.as_u64()) {
        return n.to_string();
    }
    // Fallback: extract trailing number from html_url or url
    if let Some(n) = extract_number_from_url(item) {
        crate::log_debug(&format!(
            "{}:{} — extracted number {} from URL fallback",
            resource_type, repo, n
        ));
        return n;
    }
    crate::log_warn(&format!(
        "{}:{} — missing or invalid 'number' field, using 'unknown'",
        resource_type, repo
    ));
    "unknown".to_string()
}

/// Extract a resource number from URL fields (html_url, url).
/// Parses trailing number from paths like `.../issues/123` or `.../pull/456`.
fn extract_number_from_url(item: &Value) -> Option<String> {
    for field in &["html_url", "url"] {
        if let Some(url) = item.get(field).and_then(|v| v.as_str()) {
            if let Some(last) = url.rsplit('/').next() {
                if let Ok(n) = last.parse::<u64>() {
                    return Some(n.to_string());
                }
            }
        }
    }
    None
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ScopeKind {
    All,
    Public,
    Owner,
    Repo,
    RepoPrefix,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PolicyScopeEntry {
    pub scope_kind: ScopeKind,
    pub scope_owner: Option<String>,
    pub scope_repo: Option<String>,
    pub scope_label: String,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum MinIntegrity {
    None,
    Unapproved,
    Approved,
    Merged,
}

#[derive(Debug, Clone, Default)]
pub struct PolicyContext {
    pub scopes: Vec<PolicyScopeEntry>,
    /// Additional trusted bot usernames configured at the gateway level.
    /// Objects authored by these bots receive approved (writer) integrity regardless
    /// of their author_association, just like the built-in trusted first-party bots.
    /// This list is additive and cannot override the built-in trusted bot list.
    pub trusted_bots: Vec<String>,
    /// Usernames whose content items are always blocked (effective integrity = blocked).
    /// Blocked items are unconditionally denied regardless of approval labels or min-integrity.
    pub blocked_users: Vec<String>,
    /// GitHub label names that promote a content item's effective integrity to "approved"
    /// when present on the item. Does not override blocked_users.
    pub approval_labels: Vec<String>,
}

fn normalize_scope(scope: &str, ctx: &PolicyContext) -> String {
    let token = policy_scope_token(&ctx.scopes);
    if token.is_empty() {
        scope.to_string()
    } else if ctx
        .scopes
        .iter()
        .any(|entry| matches!(entry.scope_kind, ScopeKind::All | ScopeKind::Public))
    {
        token
    } else if let Some((owner, repo)) = split_repo_id(scope) {
        let matches_any_scope = ctx.scopes.iter().any(|entry| {
            let scoped_owner = entry.scope_owner.as_deref().unwrap_or("");
            let scoped_repo = entry.scope_repo.as_deref().unwrap_or("");
            repo_matches_scope(entry.scope_kind, owner, repo, scoped_owner, scoped_repo)
        });

        if matches_any_scope {
            token
        } else {
            scope.to_string()
        }
    } else {
        scope.to_string()
    }
}

fn split_repo_id(repo_id: &str) -> Option<(&str, &str)> {
    let (owner, repo) = repo_id.split_once('/')?;
    if owner.is_empty() || repo.is_empty() {
        return None;
    }
    Some((owner, repo))
}

fn policy_scope_token(scopes: &[PolicyScopeEntry]) -> String {
    let mut labels: Vec<String> = vec![];
    for scope in scopes {
        if !scope.scope_label.is_empty() {
            labels.push(scope.scope_label.clone());
        }
    }
    if labels.is_empty() {
        String::new()
    } else {
        labels.join(" | ")
    }
}

fn repo_matches_scope(
    scope_kind: ScopeKind,
    owner: &str,
    repo: &str,
    scoped_owner: &str,
    scoped_repo: &str,
) -> bool {
    match scope_kind {
        ScopeKind::All | ScopeKind::Public => true,
        ScopeKind::Owner => !scoped_owner.is_empty() && owner.eq_ignore_ascii_case(scoped_owner),
        ScopeKind::Repo => {
            !scoped_owner.is_empty()
                && !scoped_repo.is_empty()
                && owner.eq_ignore_ascii_case(scoped_owner)
                && repo.eq_ignore_ascii_case(scoped_repo)
        }
        ScopeKind::RepoPrefix => {
            !scoped_owner.is_empty()
                && !scoped_repo.is_empty()
                && owner.eq_ignore_ascii_case(scoped_owner)
                && repo.starts_with(scoped_repo)
        }
    }
}

fn first_matching_scope(owner: &str, repo: &str, ctx: &PolicyContext) -> Option<PolicyScopeEntry> {
    ctx.scopes
        .iter()
        .find(|scope| {
            let scoped_owner = scope.scope_owner.as_deref().unwrap_or("");
            let scoped_repo = scope.scope_repo.as_deref().unwrap_or("");
            repo_matches_scope(scope.scope_kind, owner, repo, scoped_owner, scoped_repo)
        })
        .cloned()
}

fn format_integrity_label(prefix: &str, scope: &str, base: &str) -> String {
    if scope.is_empty() {
        base.to_string()
    } else if scope.contains('|') {
        let scopes = scope
            .split('|')
            .map(|value| value.trim())
            .filter(|value| !value.is_empty())
            .collect::<Vec<_>>()
            .join(",");
        format!("integrity={};scopes={}", base, scopes)
    } else {
        format!("{}{}", prefix, scope)
    }
}

pub fn none_integrity(scope: &str, ctx: &PolicyContext) -> Vec<String> {
    let normalized_scope = normalize_scope(scope, ctx);
    vec![format_integrity_label(
        label_constants::NONE_PREFIX,
        &normalized_scope,
        label_constants::NONE,
    )]
}

/// Generate blocked-level integrity tags for a scope.
///
/// Items with blocked integrity are unconditionally denied by the DIFC filter
/// because no agent is ever assigned a "blocked:" tag. This represents the
/// integrity level for items authored by users in the `blocked-users` list.
pub fn blocked_integrity(scope: &str, ctx: &PolicyContext) -> Vec<String> {
    let normalized_scope = normalize_scope(scope, ctx);
    vec![format_integrity_label(
        label_constants::BLOCKED_PREFIX,
        &normalized_scope,
        label_constants::BLOCKED_BASE,
    )]
}

/// Check if a username appears in the configured blocked-users list (case-insensitive).
pub fn is_blocked_user(username: &str, ctx: &PolicyContext) -> bool {
    if ctx.blocked_users.is_empty() {
        return false;
    }
    let lower = username.to_lowercase();
    ctx.blocked_users.iter().any(|u| u.to_lowercase() == lower)
}

/// Extract GitHub label names from a content item's `labels` array.
///
/// Returns the `name` field from each element of the item's `labels` array.
fn extract_github_label_names<'a>(item: &'a Value) -> Vec<&'a str> {
    item.get("labels")
        .and_then(|v| v.as_array())
        .map(|arr| {
            arr.iter()
                .filter_map(|label| label.get("name").and_then(|v| v.as_str()))
                .collect()
        })
        .unwrap_or_default()
}

/// Check whether a content item carries at least one label from the configured
/// `approval-labels` list (case-insensitive comparison).
#[cfg(test)]
pub fn has_approval_label(item: &Value, ctx: &PolicyContext) -> bool {
    first_matching_approval_label(item, ctx).is_some()
}

/// Return the first matching approval label name from an item, if any.
fn first_matching_approval_label<'a>(item: &'a Value, ctx: &PolicyContext) -> Option<&'a str> {
    if ctx.approval_labels.is_empty() {
        return None;
    }
    let label_names = extract_github_label_names(item);
    label_names.into_iter().find(|name| {
        ctx.approval_labels
            .iter()
            .any(|al| al.eq_ignore_ascii_case(name))
    })
}

pub fn ensure_integrity_baseline(
    scope: &str,
    integrity: Vec<String>,
    ctx: &PolicyContext,
) -> Vec<String> {
    if integrity.is_empty() {
        none_integrity(scope, ctx)
    } else {
        max_integrity(scope, integrity, none_integrity(scope, ctx), ctx)
    }
}

// ============================================================================
// Common Label Helpers
// ============================================================================
//
// Design Note: These functions return `Vec<String>` rather than iterators.
//
// This is intentional because they create OWNED data (String objects) that must
// be allocated somewhere. Returning Vec is the right choice here because:
//
// 1. The data doesn't exist before the function call - it's created fresh
// 2. The Vec is immediately consumed/moved in all usage sites
// 3. These are small, fixed-size collections (0-2 items)
// 4. Returning an iterator would require Box<dyn Iterator> (heap allocation anyway)
//    or complex lifetime management
//
// Compare with `maintainers()` and `contributors()` which return `impl Iterator`
// because they return REFERENCES to existing data, enabling zero-allocation
// operations like `.count()` or lazy evaluation with `.filter()`.
//
// See: maintainers() and contributors() in permissions.rs for the iterator pattern
// ============================================================================

/// Returns a vec with the "secret" label
#[inline]
pub fn secret_label() -> Vec<String> {
    vec![label_constants::SECRET.to_string()]
}

/// Returns a vec with the "private:user" label
#[inline]
pub fn private_user_label() -> Vec<String> {
    vec![label_constants::PRIVATE_USER.to_string()]
}

/// Returns a vec with the "approved:github" label
#[inline]
pub fn project_github_label(ctx: &PolicyContext) -> Vec<String> {
    writer_integrity("github", ctx)
}

/// Returns a vec with a "private:{scope}" label
/// Returns empty vec if scope is empty
#[inline]
pub fn private_scope_label(scope: &str) -> Vec<String> {
    if scope.is_empty() {
        return vec![];
    }
    vec![format!("{}{}", label_constants::PRIVATE_PREFIX, scope)]
}

/// Returns a scope-aware private secrecy label based on cached policy scope kind.
///
/// - public scope_kind => ["private"]
/// - owner scope_kind => ["private:<owner>"]
/// - repo scope_kind => ["private:<owner>/<repo>"]
pub fn policy_private_scope_label(
    owner: &str,
    repo: &str,
    repo_id: &str,
    ctx: &PolicyContext,
) -> Vec<String> {
    let (resource_owner, resource_repo) = if !owner.is_empty() && !repo.is_empty() {
        (owner, repo)
    } else if let Some((parsed_owner, parsed_repo)) = split_repo_id(repo_id) {
        (parsed_owner, parsed_repo)
    } else {
        ("", "")
    };

    if !resource_owner.is_empty() && !resource_repo.is_empty() {
        if let Some(matched_scope) = first_matching_scope(resource_owner, resource_repo, ctx) {
            match matched_scope.scope_kind {
                ScopeKind::All => vec![],
                ScopeKind::Public => vec!["private".to_string()],
                ScopeKind::Owner => {
                    private_scope_label(matched_scope.scope_owner.as_deref().unwrap_or(""))
                }
                ScopeKind::Repo | ScopeKind::RepoPrefix => {
                    private_scope_label(&matched_scope.scope_label)
                }
            }
        } else {
            private_scope_label(&format!("{}/{}", resource_owner, resource_repo))
        }
    } else {
        vec!["private".to_string()]
    }
}

// ============================================================================
// Repository Visibility Helpers
// ============================================================================

/// Returns private secrecy labels for a repo if it is private, or an empty vec if public.
/// On unknown visibility (None), fails secure (returns private labels) except in tests.
pub(crate) fn repo_visibility_secrecy(
    owner: &str,
    repo: &str,
    repo_id: &str,
    ctx: &PolicyContext,
) -> Vec<String> {
    // If any identifier is missing, treat visibility as unknown and fail secure
    if owner.is_empty() || repo.is_empty() || repo_id.is_empty() {
        return policy_private_scope_label(owner, repo, repo_id, ctx);
    }
    match super::backend::is_repo_private(owner, repo) {
        Some(true) => policy_private_scope_label(owner, repo, repo_id, ctx),
        Some(false) => vec![],
        None => {
            if cfg!(test) {
                vec![]
            } else {
                policy_private_scope_label(owner, repo, repo_id, ctx)
            }
        }
    }
}

/// Convenience wrapper: splits `repo_id` as "owner/repo" and delegates to
/// [`repo_visibility_secrecy`].
pub(crate) fn repo_visibility_secrecy_for_repo_id(
    repo_id: &str,
    ctx: &PolicyContext,
) -> Vec<String> {
    if let Some((owner, repo)) = repo_id.split_once('/') {
        repo_visibility_secrecy(owner, repo, repo_id, ctx)
    } else {
        // Malformed repo_id: treat as unknown visibility and fail secure
        policy_private_scope_label("", "", repo_id, ctx)
    }
}

/// Returns `Some(true)` if the repo identified by `repo_id` ("owner/repo") is private,
/// `Some(false)` if public, or `None` if the visibility is unknown.
pub(crate) fn repo_visibility_private_for_repo_id(repo_id: &str) -> Option<bool> {
    let (owner, repo) = repo_id.split_once('/')?;
    super::backend::is_repo_private(owner, repo)
}

// ============================================================================
// JSON Field Extraction Helpers
// ============================================================================

/// Extract a string field from a JSON value, returning a default if missing
#[inline]
pub fn get_str_or<'a>(value: &'a Value, field: &str, default: &'a str) -> &'a str {
    value.get(field).and_then(|v| v.as_str()).unwrap_or(default)
}

/// Extract a nested string field (e.g., user.login) from a JSON value
#[inline]
#[allow(dead_code)]
pub fn get_nested_str<'a>(value: &'a Value, outer: &str, inner: &str) -> &'a str {
    value
        .get(outer)
        .and_then(|v| v.get(inner))
        .and_then(|v| v.as_str())
        .unwrap_or("")
}

/// Extract a boolean field from a JSON value, returning a default if missing
#[inline]
pub fn get_bool_or(value: &Value, field: &str, default: bool) -> bool {
    value
        .get(field)
        .and_then(|v| v.as_bool())
        .unwrap_or(default)
}

/// Limit a slice to MAX_ITEMS_PER_RESPONSE, logging a warning when truncated
///
/// This helper centralizes the item-limiting logic used in all response labeling
/// handlers. The `tool_name` is included in the warning message for diagnostics.
pub fn limit_items_with_log<'a, T>(items: &'a [T], tool_name: &str) -> &'a [T] {
    let max = super::constants::MAX_ITEMS_PER_RESPONSE;
    if items.len() > max {
        crate::log_warn(&format!(
            "{}: limiting {} items to {}",
            tool_name,
            items.len(),
            max
        ));
        &items[..max]
    } else {
        items
    }
}

/// Extract a string field from a JSON value
/// Returns empty string if field doesn't exist or isn't a string
#[inline]
pub fn get_string_field(value: &Value, field: &str) -> String {
    value
        .get(field)
        .and_then(|v| v.as_str())
        .unwrap_or("")
        .to_string()
}

/// Format repository ID as "owner/repo"
/// Returns empty string if either owner or repo is empty
#[inline]
pub fn format_repo_id(owner: &str, repo: &str) -> String {
    if owner.is_empty() || repo.is_empty() {
        String::new()
    } else {
        format!("{}/{}", owner, repo)
    }
}

/// Extract owner, repo, and repo_id from tool arguments
/// Returns (owner, repo, repo_id) where repo_id is "owner/repo" or empty
pub fn extract_repo_info(tool_args: &Value) -> (String, String, String) {
    let owner = get_string_field(tool_args, field_names::OWNER);
    let repo = get_string_field(tool_args, field_names::REPO);
    let repo_id = format_repo_id(&owner, &repo);
    (owner, repo, repo_id)
}

/// Extract owner/repo from a search query containing `repo:owner/repo`
/// Returns (owner, repo, repo_id) where repo_id is "owner/repo" or empty
pub fn extract_repo_info_from_search_query(query: &str) -> (String, String, String) {
    if query.is_empty() {
        return (String::new(), String::new(), String::new());
    }

    for token in query.split_whitespace() {
        let cleaned = token.trim_matches(|c: char| {
            c == '"' || c == '\'' || c == ',' || c == '(' || c == ')' || c == ';'
        });

        if let Some(repo_ref) = cleaned.strip_prefix("repo:") {
            let repo_ref = repo_ref.trim_matches(|c: char| {
                c == '"' || c == '\'' || c == ',' || c == '(' || c == ')' || c == ';'
            });
            if let Some((owner, repo)) = repo_ref.split_once('/') {
                if !owner.is_empty() && !repo.is_empty() {
                    let owner = owner.to_string();
                    let repo = repo.to_string();
                    let repo_id = format_repo_id(&owner, &repo);
                    return (owner, repo, repo_id);
                }
            }
        }
    }

    (String::new(), String::new(), String::new())
}

fn extract_repo_from_github_url(url: &str) -> Option<String> {
    let parse_owner_repo = |path: &str| {
        let mut parts = path.split('/').filter(|segment| !segment.is_empty());
        let owner = parts.next()?;
        let repo = parts.next()?;
        Some(format!("{}/{}", owner, repo))
    };

    if let Some(path) = url
        .strip_prefix("https://api.github.com/repos/")
        .or_else(|| url.strip_prefix("http://api.github.com/repos/"))
        .or_else(|| url.strip_prefix("https://github.com/"))
        .or_else(|| url.strip_prefix("http://github.com/"))
    {
        return parse_owner_repo(path);
    }

    None
}

/// Extract repository full name from a response item
/// Tries multiple fields in order: full_name, repository.full_name,
/// base.repo.full_name, head.repo.full_name, html_url parsing
/// Returns empty string if no repo info found
pub fn extract_repo_from_item(item: &Value) -> String {
    // Direct full_name (repositories)
    if let Some(name) = item.get("full_name").and_then(|v| v.as_str()) {
        return name.to_string();
    }
    // repository.full_name (issues, PRs with repo info)
    if let Some(name) = item
        .get("repository")
        .and_then(|r| r.get("full_name"))
        .and_then(|v| v.as_str())
    {
        return name.to_string();
    }
    // base.repo.full_name (pull requests)
    if let Some(name) = item
        .get("base")
        .and_then(|b| b.get("repo"))
        .and_then(|r| r.get("full_name"))
        .and_then(|v| v.as_str())
    {
        return name.to_string();
    }
    // head.repo.full_name (pull requests)
    if let Some(name) = item
        .get("head")
        .and_then(|h| h.get("repo"))
        .and_then(|r| r.get("full_name"))
        .and_then(|v| v.as_str())
    {
        return name.to_string();
    }
    // repository_url parsing for search endpoints
    if let Some(url) = item.get("repository_url").and_then(|v| v.as_str()) {
        if let Some(repo_id) = extract_repo_from_github_url(url) {
            return repo_id;
        }
    }
    // html_url parsing as last resort - extract owner/repo from URLs like:
    // https://github.com/owner/repo/pull/123 or https://github.com/owner/repo/issues/456
    if let Some(url) = item.get("html_url").and_then(|v| v.as_str()) {
        if let Some(repo_id) = extract_repo_from_github_url(url) {
            return repo_id;
        }
    }
    // Generic URL field fallback
    if let Some(url) = item.get("url").and_then(|v| v.as_str()) {
        if let Some(repo_id) = extract_repo_from_github_url(url) {
            return repo_id;
        }
    }
    String::new()
}

/// Extract items array from response, handling REST, items field, and GraphQL formats.
/// Returns (Option<items_array>, items_path) where items_path is a JSON Pointer prefix:
///   - "" for root array
///   - "/items" for {items: [...]}
///   - "/data/repository/pullRequests/nodes" for GraphQL nested format
///   - etc.
pub fn extract_items_array(response: &Value) -> (Option<&Vec<Value>>, String) {
    // REST formats
    if let Some(arr) = response.as_array() {
        return (Some(arr), String::new());
    }
    if let Some(arr) = response.get("items").and_then(|v| v.as_array()) {
        return (Some(arr), "/items".to_string());
    }
    if let Some(arr) = response.get("issues").and_then(|v| v.as_array()) {
        return (Some(arr), "/issues".to_string());
    }
    if let Some(arr) = response.get("pull_requests").and_then(|v| v.as_array()) {
        return (Some(arr), "/pull_requests".to_string());
    }

    // GraphQL format: data.repository.<resource>.nodes or data.search.nodes
    if let Some(data) = response.get("data") {
        // data.repository.<field>.nodes (issues, pullRequests, discussions, etc.)
        if let Some(repo) = data.get("repository") {
            for (field, pointer) in GRAPHQL_COLLECTION_FIELDS {
                if let Some(arr) = repo.get(*field).and_then(|v| v.get("nodes")).and_then(|v| v.as_array()) {
                    return (Some(arr), pointer.to_string());
                }
            }
        }
        // data.search.nodes
        if let Some(arr) = data.get("search").and_then(|v| v.get("nodes")).and_then(|v| v.as_array()) {
            return (Some(arr), "/data/search/nodes".to_string());
        }
        // data.search.edges[].node — flatten into nodes
        // (not supported as direct reference; caller should use search.nodes form)
    }

    (None, String::new())
}

/// GraphQL collection fields under data.repository and their JSON Pointer paths.
const GRAPHQL_COLLECTION_FIELDS: &[(&str, &str)] = &[
    ("issues", "/data/repository/issues/nodes"),
    ("pullRequests", "/data/repository/pullRequests/nodes"),
    ("discussions", "/data/repository/discussions/nodes"),
    ("discussionCategories", "/data/repository/discussionCategories/nodes"),
];

/// Extract the items array from a GraphQL response.
/// Traverses data.repository.<field>.nodes and data.search.nodes paths.
pub fn extract_graphql_nodes(response: &Value) -> Option<&Vec<Value>> {
    let data = response.get("data")?;

    // data.repository.<field>.nodes
    if let Some(repo) = data.get("repository") {
        for (field, _) in GRAPHQL_COLLECTION_FIELDS {
            if let Some(arr) = repo.get(*field).and_then(|v| v.get("nodes")).and_then(|v| v.as_array()) {
                return Some(arr);
            }
        }
    }
    // data.search.nodes
    if let Some(arr) = data.get("search").and_then(|v| v.get("nodes")).and_then(|v| v.as_array()) {
        return Some(arr);
    }

    None
}

/// Returns true if the response is a GraphQL wrapper (has a "data" key).
/// Used to prevent treating the entire GraphQL object as a single item.
pub fn is_graphql_wrapper(response: &Value) -> bool {
    response.get("data").is_some()
}

/// Extract a single object from a GraphQL response for singular queries.
/// Traverses data.repository.<field> for fields like "issue", "pullRequest".
pub fn extract_graphql_single_object(response: &Value) -> Option<&Value> {
    let data = response.get("data")?;
    let repo = data.get("repository")?;

    for field in GRAPHQL_SINGLE_OBJECT_FIELDS {
        if let Some(obj) = repo.get(*field) {
            if obj.is_object() {
                return Some(obj);
            }
        }
    }
    None
}

/// GraphQL singular object fields under data.repository.
const GRAPHQL_SINGLE_OBJECT_FIELDS: &[&str] = &[
    "issue",
    "pullRequest",
    "discussion",
];

/// Generate JSON Pointer path for an item index in a collection
/// Returns a path like "/items/0" or "/0" depending on the items_path
#[inline]
pub fn make_item_path(items_path: &str, index: usize) -> String {
    if items_path.is_empty() {
        format!("/{}", index)
    } else {
        format!("{}/{}", items_path, index)
    }
}

/// Extract issue or PR number from tool arguments as a String
/// Handles string, i64, and u64 fields without memory leaks
///
/// # Arguments
/// * `tool_args` - The JSON value containing tool arguments
/// * `field` - The field name to extract (e.g., "issue_number", "pull_number")
///
/// # Returns
/// * `Some(String)` - The number as a string
/// * `None` - If the field doesn't exist or isn't a string/number
pub fn extract_number_as_string(tool_args: &Value, field: &str) -> Option<String> {
    tool_args.get(field).and_then(|v| {
        v.as_str()
            .map(String::from)
            .or_else(|| v.as_i64().map(|n| n.to_string()))
            .or_else(|| v.as_u64().map(|n| n.to_string()))
    })
}

// ============================================================================
// Integrity Scope Helpers
// ============================================================================

/// Generate unapproved-level integrity tags for a scope.
///
/// This helper normalizes the provided `scope` using the `PolicyContext`
/// and returns integrity labels for:
/// - a "none" integrity level for the scope
/// - an "unapproved" integrity level for the scope
///
/// These labels represent the lowest integrity levels; higher levels
/// (such as approved) build on top of them.
pub fn reader_integrity(scope: &str, ctx: &PolicyContext) -> Vec<String> {
    let normalized_scope = normalize_scope(scope, ctx);
    vec![
        format_integrity_label(
            label_constants::NONE_PREFIX,
            &normalized_scope,
            label_constants::NONE,
        ),
        format_integrity_label(
            label_constants::READER_PREFIX,
            &normalized_scope,
            label_constants::READER_BASE,
        ),
    ]
}

/// Generate approved-level integrity tags for a scope.
/// Includes unapproved level (hierarchical: approved > unapproved)
pub fn writer_integrity(scope: &str, ctx: &PolicyContext) -> Vec<String> {
    let normalized_scope = normalize_scope(scope, ctx);
    vec![
        format_integrity_label(
            label_constants::NONE_PREFIX,
            &normalized_scope,
            label_constants::NONE,
        ),
        format_integrity_label(
            label_constants::READER_PREFIX,
            &normalized_scope,
            label_constants::READER_BASE,
        ),
        format_integrity_label(
            label_constants::WRITER_PREFIX,
            &normalized_scope,
            label_constants::WRITER_BASE,
        ),
    ]
}

/// Generate merged-level integrity tags for a scope.
/// Includes approved and unapproved (hierarchical: merged > approved > unapproved)
pub fn merged_integrity(scope: &str, ctx: &PolicyContext) -> Vec<String> {
    let normalized_scope = normalize_scope(scope, ctx);
    vec![
        format_integrity_label(
            label_constants::NONE_PREFIX,
            &normalized_scope,
            label_constants::NONE,
        ),
        format_integrity_label(
            label_constants::READER_PREFIX,
            &normalized_scope,
            label_constants::READER_BASE,
        ),
        format_integrity_label(
            label_constants::WRITER_PREFIX,
            &normalized_scope,
            label_constants::WRITER_BASE,
        ),
        format_integrity_label(
            label_constants::MERGED_PREFIX,
            &normalized_scope,
            label_constants::MERGED_BASE,
        ),
    ]
}

fn integrity_rank(scope: &str, labels: &[String], ctx: &PolicyContext) -> u8 {
    let normalized_scope = normalize_scope(scope, ctx);

    let merged = format_integrity_label(
        label_constants::MERGED_PREFIX,
        &normalized_scope,
        label_constants::MERGED_BASE,
    );
    let writer = format_integrity_label(
        label_constants::WRITER_PREFIX,
        &normalized_scope,
        label_constants::WRITER_BASE,
    );
    let reader = format_integrity_label(
        label_constants::READER_PREFIX,
        &normalized_scope,
        label_constants::READER_BASE,
    );
    let none = format_integrity_label(
        label_constants::NONE_PREFIX,
        &normalized_scope,
        label_constants::NONE,
    );

    if labels.iter().any(|l| l == &merged) {
        4
    } else if labels.iter().any(|l| l == &writer) {
        3
    } else if labels.iter().any(|l| l == &reader) {
        2
    } else if labels.iter().any(|l| l == &none) {
        1
    } else {
        0
    }
}

fn integrity_for_rank(scope: &str, rank: u8, ctx: &PolicyContext) -> Vec<String> {
    match rank {
        4 => merged_integrity(scope, ctx),
        3 => writer_integrity(scope, ctx),
        2 => reader_integrity(scope, ctx),
        _ => none_integrity(scope, ctx),
    }
}

/// Elevate integrity to the max of current and candidate levels for a scope.
pub fn max_integrity(
    scope: &str,
    current: Vec<String>,
    candidate: Vec<String>,
    ctx: &PolicyContext,
) -> Vec<String> {
    let left = integrity_rank(scope, &current, ctx);
    let right = integrity_rank(scope, &candidate, ctx);
    integrity_for_rank(scope, left.max(right), ctx)
}

/// Map a GitHub `author_association` value to initial integrity labels.
///
/// Mapping (case-insensitive):
/// - OWNER, MEMBER, COLLABORATOR => approved
/// - CONTRIBUTOR, FIRST_TIME_CONTRIBUTOR => unapproved
/// - FIRST_TIMER, NONE, missing => none
pub fn author_association_floor_from_str(
    scope: &str,
    association: Option<&str>,
    ctx: &PolicyContext,
) -> Vec<String> {
    let Some(raw) = association else {
        return vec![];
    };

    let normalized = raw.trim().to_ascii_uppercase();
    match normalized.as_str() {
        "OWNER" | "MEMBER" | "COLLABORATOR" => writer_integrity(scope, ctx),
        "CONTRIBUTOR" | "FIRST_TIME_CONTRIBUTOR" => reader_integrity(scope, ctx),
        "FIRST_TIMER" | "NONE" => vec![],
        _ => vec![],
    }
}

/// Extract the author login from an item, checking common GitHub API fields.
/// Returns empty string if no login found.
fn extract_author_login(item: &Value) -> &str {
    // Issues and PRs use user.login
    let login = get_nested_str(item, "user", "login");
    if !login.is_empty() {
        return login;
    }
    // Commits use author.login
    get_nested_str(item, "author", "login")
}

/// Check whether an item contains an `author_association` (or `authorAssociation`) field.
pub fn has_author_association(item: &Value) -> bool {
    item.get("author_association")
        .and_then(|v| v.as_str())
        .is_some()
        || item
            .get("authorAssociation")
            .and_then(|v| v.as_str())
            .is_some()
}

/// Extract author_association from an item and return initial integrity floor.
/// Trusted first-party GitHub bots and any gateway-configured trusted bots are
/// elevated to approved (writer) integrity regardless of their author_association value.
pub fn author_association_floor(item: &Value, scope: &str, ctx: &PolicyContext) -> Vec<String> {
    let author_login = extract_author_login(item);
    if !author_login.is_empty()
        && (is_trusted_first_party_bot(author_login)
            || is_configured_trusted_bot(author_login, ctx))
    {
        return writer_integrity(scope, ctx);
    }

    let association = item
        .get("author_association")
        .and_then(|v| v.as_str())
        .or_else(|| item.get("authorAssociation").and_then(|v| v.as_str()));

    author_association_floor_from_str(scope, association, ctx)
}

/// Check if a branch/ref should be treated as default branch context
pub fn is_default_branch_ref(branch_ref: &str) -> bool {
    branch_ref.is_empty()
        || branch_ref.eq_ignore_ascii_case("main")
        || branch_ref.eq_ignore_ascii_case("master")
        || branch_ref.eq_ignore_ascii_case("HEAD")
}

fn looks_like_commit_sha(reference: &str) -> bool {
    let length = reference.len();
    if !(7..=40).contains(&length) {
        return false;
    }
    reference.chars().all(|value| value.is_ascii_hexdigit())
}

pub fn is_default_branch_commit_context(tool_name: &str, sha_or_ref: &str) -> bool {
    if is_default_branch_ref(sha_or_ref) {
        return true;
    }

    tool_name == "get_commit" && looks_like_commit_sha(sha_or_ref)
}

/// Determine integrity level for a pull request
/// Rules:
/// - PR authored by a blocked user => blocked-level (unconditional denial)
/// - merged PR => merged-level
/// - private repo PR => approved
/// - public forked PR => unapproved
/// - public direct PR => approved
/// - PR with an approval label => at least approved
/// - Backend enrichment: when `author_association` is missing from the item,
///   fetch the individual PR via REST to get the correct association and fork status.
pub fn pr_integrity(
    item: &Value,
    repo_full_name: &str,
    repo_private: bool,
    is_forked: Option<bool>,
    ctx: &PolicyContext,
) -> Vec<String> {
    // Step 1: Check if author is in blocked_users — takes precedence over all other rules.
    let author_login = extract_author_login(item);
    if !author_login.is_empty() && is_blocked_user(author_login, ctx) {
        let number = item.get("number").and_then(|v| v.as_u64()).unwrap_or(0);
        crate::log_info(&format!(
            "[integrity] pr:{}#{} → blocked (author '{}' in blocked-users)",
            repo_full_name, number, author_login
        ));
        return blocked_integrity(repo_full_name, ctx);
    }

    let mut integrity = author_association_floor(item, repo_full_name, ctx);

    // Check if PR is merged (either merged_at field exists or merged boolean is true)
    let mut is_merged = item
        .get(field_names::MERGED_AT)
        .map(|v| !v.is_null())
        .or_else(|| item.get(field_names::MERGED).and_then(|v| v.as_bool()))
        .unwrap_or(false);

    // Track whether fork status was enriched from the backend
    let mut effective_is_forked = is_forked;

    // Backend enrichment: when author_association is absent from the response
    // (e.g. GitHub MCP Server omits it from MinimalPullRequest), fetch the
    // individual PR via REST to obtain the correct association, fork status,
    // and merge status.
    if integrity.is_empty() && !has_author_association(item) && !repo_private {
        let number_opt = item
            .get("number")
            .and_then(|v| v.as_u64())
            .map(|n| n.to_string())
            .or_else(|| extract_number_from_url(item));
        if let Some(number_str) = number_opt {
            let (owner, repo) = repo_full_name.split_once('/').unwrap_or(("", ""));
            if !owner.is_empty() && !repo.is_empty() {
                if let Some(facts) =
                    super::backend::get_pull_request_facts(owner, repo, &number_str)
                {
                    crate::log_debug(&format!(
                        "[integrity] pr:{}#{} enriched: author_association={:?}, is_forked={:?}, is_merged={}",
                        repo_full_name, number_str, facts.author_association, facts.is_forked, facts.is_merged
                    ));
                    let enriched_floor = author_association_floor_from_str(
                        repo_full_name,
                        facts.author_association.as_deref(),
                        ctx,
                    );
                    // Elevate trusted bots
                    let enriched_floor = if let Some(ref login) = facts.author_login {
                        if is_trusted_first_party_bot(login)
                            || is_configured_trusted_bot(login, ctx)
                        {
                            max_integrity(
                                repo_full_name,
                                enriched_floor,
                                writer_integrity(repo_full_name, ctx),
                                ctx,
                            )
                        } else {
                            enriched_floor
                        }
                    } else {
                        enriched_floor
                    };
                    integrity =
                        max_integrity(repo_full_name, integrity, enriched_floor, ctx);
                    // Use enriched fork/merge status if missing from item
                    if effective_is_forked.is_none() {
                        effective_is_forked = facts.is_forked;
                    }
                    if !is_merged && facts.is_merged {
                        is_merged = true;
                    }
                } else {
                    crate::log_debug(&format!(
                        "[integrity] pr:{}#{} enrichment failed (backend returned None)",
                        repo_full_name, number_str
                    ));
                }
            }
        }
    }

    if repo_private {
        integrity = max_integrity(
            repo_full_name,
            integrity,
            writer_integrity(repo_full_name, ctx),
            ctx,
        );
    } else {
        integrity = match effective_is_forked {
            Some(true) => max_integrity(
                repo_full_name,
                integrity,
                reader_integrity(repo_full_name, ctx),
                ctx,
            ),
            Some(false) => max_integrity(
                repo_full_name,
                integrity,
                writer_integrity(repo_full_name, ctx),
                ctx,
            ),
            None => integrity,
        };
    }

    if is_merged {
        integrity = max_integrity(
            repo_full_name,
            integrity,
            merged_integrity(repo_full_name, ctx),
            ctx,
        );
    }

    let integrity = ensure_integrity_baseline(repo_full_name, integrity, ctx);

    // Step 2: Apply approval-labels promotion — raise to at least approved.
    if let Some(label) = first_matching_approval_label(item, ctx) {
        let number = item.get("number").and_then(|v| v.as_u64()).unwrap_or(0);
        crate::log_info(&format!(
            "[integrity] pr:{}#{} promoted to approved (label '{}' in approval-labels)",
            repo_full_name, number, label
        ));
        max_integrity(
            repo_full_name,
            integrity,
            writer_integrity(repo_full_name, ctx),
            ctx,
        )
    } else {
        integrity
    }
}

/// Determine integrity level for an issue
/// Rules:
/// - Issue authored by a blocked user => blocked-level (unconditional denial)
/// - private repo issues => approved
/// - public repo issues => no integrity
/// - Issue with an approval label => at least approved
/// - Backend enrichment: when `author_association` is missing from the item
///   (e.g. GitHub MCP Server GraphQL path omits it), fetch the individual issue
///   via REST to get the correct association value.
pub fn issue_integrity(
    item: &Value,
    repo_full_name: &str,
    repo_private: bool,
    ctx: &PolicyContext,
) -> Vec<String> {
    // Step 1: Check if author is in blocked_users — takes precedence over all other rules.
    let author_login = extract_author_login(item);
    if !author_login.is_empty() && is_blocked_user(author_login, ctx) {
        let number = item.get("number").and_then(|v| v.as_u64()).unwrap_or(0);
        crate::log_info(&format!(
            "[integrity] issue:{}#{} → blocked (author '{}' in blocked-users)",
            repo_full_name, number, author_login
        ));
        return blocked_integrity(repo_full_name, ctx);
    }

    let mut integrity = author_association_floor(item, repo_full_name, ctx);

    // Backend enrichment: when author_association is absent from the response
    // (e.g. GitHub MCP Server's list_issues GraphQL path omits it), fetch the
    // individual issue via REST to obtain the correct value. This avoids
    // incorrectly assigning "none" integrity to members/collaborators.
    if integrity.is_empty() && !has_author_association(item) && !repo_private {
        let number_opt = item
            .get("number")
            .and_then(|v| v.as_u64())
            .map(|n| n.to_string())
            .or_else(|| extract_number_from_url(item));
        if let Some(number_str) = number_opt {
            let (owner, repo) = repo_full_name.split_once('/').unwrap_or(("", ""));
            if !owner.is_empty() && !repo.is_empty() {
                if let Some(association) =
                    super::backend::get_issue_author_association(owner, repo, &number_str)
                {
                    crate::log_debug(&format!(
                        "[integrity] issue:{}#{} enriched author_association='{}'",
                        repo_full_name, number_str, association
                    ));
                    // Re-check trusted bot status with enriched login
                    let enriched_floor =
                        author_association_floor_from_str(repo_full_name, Some(&association), ctx);
                    integrity =
                        max_integrity(repo_full_name, integrity, enriched_floor, ctx);
                } else {
                    crate::log_debug(&format!(
                        "[integrity] issue:{}#{} enrichment failed (backend returned None)",
                        repo_full_name, number_str
                    ));
                }
            }
        }
    }

    if repo_private {
        integrity = max_integrity(
            repo_full_name,
            integrity,
            writer_integrity(repo_full_name, ctx),
            ctx,
        );
    }
    let integrity = ensure_integrity_baseline(repo_full_name, integrity, ctx);

    // Step 2: Apply approval-labels promotion — raise to at least approved.
    if let Some(label) = first_matching_approval_label(item, ctx) {
        let number = item.get("number").and_then(|v| v.as_u64()).unwrap_or(0);
        crate::log_info(&format!(
            "[integrity] issue:{}#{} promoted to approved (label '{}' in approval-labels)",
            repo_full_name, number, label
        ));
        max_integrity(
            repo_full_name,
            integrity,
            writer_integrity(repo_full_name, ctx),
            ctx,
        )
    } else {
        integrity
    }
}

/// Determine integrity level for a commit.
///
/// Rules:
/// - Commit authored by a blocked user => blocked-level (unconditional denial)
/// - Start from author_association floor
/// - Private repo commits elevate to approved
/// - Default-branch reachable commits elevate to merged
///
/// Note: approval-labels promotion does not apply to commits because GitHub
/// commits do not carry issue/PR-style labels.
pub fn commit_integrity(
    item: &Value,
    repo_full_name: &str,
    repo_private: bool,
    is_default_branch: bool,
    ctx: &PolicyContext,
) -> Vec<String> {
    // Step 1: Check if author is in blocked_users — takes precedence over all other rules.
    let author_login = extract_author_login(item);
    if !author_login.is_empty() && is_blocked_user(author_login, ctx) {
        let sha = item.get("sha").and_then(|v| v.as_str()).unwrap_or("unknown");
        let short_sha = if sha.len() > 8 { &sha[..8] } else { sha };
        crate::log_info(&format!(
            "[integrity] commit:{}@{} → blocked (author '{}' in blocked-users)",
            repo_full_name, short_sha, author_login
        ));
        return blocked_integrity(repo_full_name, ctx);
    }

    let mut integrity = author_association_floor(item, repo_full_name, ctx);

    if repo_private {
        integrity = max_integrity(
            repo_full_name,
            integrity,
            writer_integrity(repo_full_name, ctx),
            ctx,
        );
    }

    if is_default_branch {
        integrity = max_integrity(
            repo_full_name,
            integrity,
            merged_integrity(repo_full_name, ctx),
            ctx,
        );
    }

    ensure_integrity_baseline(repo_full_name, integrity, ctx)
}

/// Check if a user is a trusted first-party GitHub bot.
///
/// These bots are platform services whose presence requires explicit admin
/// configuration. Their authored objects receive approved (writer) integrity
/// regardless of author_association.
///
/// Trusted bots:
/// - dependabot[bot]: GitHub dependency updater
/// - github-actions[bot]: GitHub Actions workflow actor (GITHUB_TOKEN)
/// - github-actions: GitHub Actions workflow actor (without [bot] suffix, as returned by some APIs)
/// - app/github-actions: GitHub Actions workflow actor (with app/ prefix, as returned by gh CLI)
/// - github-merge-queue[bot]: GitHub merge queue automation
/// - copilot: GitHub Copilot coding agent (app login)
/// - copilot-swe-agent[bot]: GitHub Copilot SWE agent (bot user login from REST API)
/// - copilot-swe-agent: GitHub Copilot SWE agent (without [bot] suffix)
/// - app/copilot-swe-agent: GitHub Copilot SWE agent (with app/ prefix, as returned by gh CLI)
pub fn is_trusted_first_party_bot(username: &str) -> bool {
    let lower = username.to_lowercase();
    lower == "dependabot[bot]"
        || lower == "github-actions[bot]"
        || lower == "github-actions"
        || lower == "app/github-actions"
        || lower == "github-merge-queue[bot]"
        || lower == "copilot"
        || lower == "copilot-swe-agent[bot]"
        || lower == "copilot-swe-agent"
        || lower == "app/copilot-swe-agent"
}

/// Check if a user is in the gateway-configured trusted bot list.
///
/// This checks the `trusted_bots` list in `PolicyContext`, which is populated from
/// the gateway configuration's `trustedBots` field. Comparison is case-insensitive.
/// This list is additive and cannot remove entries from the built-in trusted bot list.
pub fn is_configured_trusted_bot(username: &str, ctx: &PolicyContext) -> bool {
    if ctx.trusted_bots.is_empty() {
        return false;
    }
    let lower = username.to_lowercase();
    ctx.trusted_bots.iter().any(|b| b.to_lowercase() == lower)
}

/// Check if a user appears to be a bot (broad detection).
///
/// This is a broader check that includes third-party bots.
/// For integrity elevation, use is_trusted_first_party_bot() instead.
#[allow(dead_code)]
pub fn is_bot(username: &str) -> bool {
    let lower = username.to_lowercase();
    lower.ends_with("[bot]")
        || lower.ends_with("-bot")
        || lower == "dependabot"
        || lower == "renovate"
        || lower == "github-actions"
        || lower == "copilot"
}

/// Check if a user is the repository owner (case-insensitive)
#[allow(dead_code)]
pub fn is_owner(username: &str, owner: &str) -> bool {
    username.eq_ignore_ascii_case(owner)
}
