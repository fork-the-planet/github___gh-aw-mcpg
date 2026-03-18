//! Helper functions for label generation
//!
//! This module contains utility functions used across the labeling system,
//! including JSON extraction, integrity determination, and common operations.

use serde_json::Value;

use super::constants::{field_names, label_constants};

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

#[allow(dead_code)]
pub fn repo_matches_policy_scope(owner: &str, repo: &str, ctx: &PolicyContext) -> bool {
    ctx.scopes.iter().any(|scope| {
        let scoped_owner = scope.scope_owner.as_deref().unwrap_or("");
        let scoped_repo = scope.scope_repo.as_deref().unwrap_or("");
        repo_matches_scope(scope.scope_kind, owner, repo, scoped_owner, scoped_repo)
    })
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

/// Extract items array from response, handling both root array and items field
/// Returns (Option<items_array>, items_path) where items_path is "" for root array, "/items" for items field
pub fn extract_items_array(response: &Value) -> (Option<&Vec<Value>>, &'static str) {
    if let Some(arr) = response.as_array() {
        (Some(arr), "")
    } else if let Some(arr) = response.get("items").and_then(|v| v.as_array()) {
        (Some(arr), "/items")
    } else if let Some(arr) = response.get("issues").and_then(|v| v.as_array()) {
        (Some(arr), "/issues")
    } else if let Some(arr) = response.get("pull_requests").and_then(|v| v.as_array()) {
        (Some(arr), "/pull_requests")
    } else {
        (None, "")
    }
}

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

/// Extract author_association from an item and return initial integrity floor.
pub fn author_association_floor(item: &Value, scope: &str, ctx: &PolicyContext) -> Vec<String> {
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
/// - merged PR => merged-level
/// - private repo PR => approved
/// - public forked PR => unapproved
/// - public direct PR => approved
pub fn pr_integrity(
    item: &Value,
    repo_full_name: &str,
    repo_private: bool,
    is_forked: Option<bool>,
    ctx: &PolicyContext,
) -> Vec<String> {
    let mut integrity = author_association_floor(item, repo_full_name, ctx);

    // Check if PR is merged (either merged_at field exists or merged boolean is true)
    let is_merged = item
        .get(field_names::MERGED_AT)
        .map(|v| !v.is_null())
        .or_else(|| item.get(field_names::MERGED).and_then(|v| v.as_bool()))
        .unwrap_or(false);

    if repo_private {
        integrity = max_integrity(
            repo_full_name,
            integrity,
            writer_integrity(repo_full_name, ctx),
            ctx,
        );
    } else {
        integrity = match is_forked {
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

    ensure_integrity_baseline(repo_full_name, integrity, ctx)
}

/// Determine integrity level for an issue
/// Rules:
/// - private repo issues => approved
/// - public repo issues => no integrity
pub fn issue_integrity(
    item: &Value,
    repo_full_name: &str,
    _owner: &str,
    _repo: &str,
    repo_private: bool,
    ctx: &PolicyContext,
) -> Vec<String> {
    let mut integrity = author_association_floor(item, repo_full_name, ctx);
    if repo_private {
        integrity = max_integrity(
            repo_full_name,
            integrity,
            writer_integrity(repo_full_name, ctx),
            ctx,
        );
    }
    ensure_integrity_baseline(repo_full_name, integrity, ctx)
}

/// Determine integrity level for a commit.
///
/// Rules:
/// - Start from author_association floor
/// - Private repo commits elevate to approved
/// - Default-branch reachable commits elevate to merged
pub fn commit_integrity(
    item: &Value,
    repo_full_name: &str,
    repo_private: bool,
    is_default_branch: bool,
    ctx: &PolicyContext,
) -> Vec<String> {
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

/// Check if a user appears to be a bot
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

/// Check if a user has verified contributor status
/// This is a placeholder that delegates to the backend module
#[allow(dead_code)]
pub fn is_verified_contributor(username: &str, owner: &str, repo: &str) -> bool {
    // Import from backend to avoid circular dependency
    super::backend::is_verified_contributor(username, owner, repo)
}
