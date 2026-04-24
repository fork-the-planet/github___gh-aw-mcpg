//! Helper functions for label generation
//!
//! This module contains utility functions used across the labeling system,
//! including JSON extraction, integrity determination, and common operations.

use std::sync::atomic::{AtomicBool, Ordering};

use serde_json::Value;

use super::backend::GithubMcpCallback;
use super::constants::{field_names, label_constants};

/// Ensures the endorsement gateway-mode warning is emitted at most once per process lifetime.
static ENDORSEMENT_GATEWAY_WARNING_EMITTED: AtomicBool = AtomicBool::new(false);

/// Ensures the disapproval gateway-mode warning is emitted at most once per process lifetime.
static DISAPPROVAL_GATEWAY_WARNING_EMITTED: AtomicBool = AtomicBool::new(false);

/// Extract a resource number from a JSON item, returning the number as a string.
/// Checks the `number` field first, then falls back to extracting the trailing
/// number segment from `html_url` or `url` (e.g. `.../issues/123` → `123`).
/// Returns "unknown" (with a log warning) if no number can be determined.
pub(crate) fn extract_resource_number(item: &Value, resource_type: &str, repo: &str) -> String {
    if let Some(n) = item.get(field_names::NUMBER).and_then(|v| v.as_u64()) {
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

impl ScopeKind {
    pub fn as_str(self) -> &'static str {
        match self {
            ScopeKind::All => "All",
            ScopeKind::Public => "Public",
            ScopeKind::Owner => "Owner",
            ScopeKind::Repo => "Repo",
            ScopeKind::RepoPrefix => "RepoPrefix",
        }
    }
}

impl std::fmt::Display for ScopeKind {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(self.as_str())
    }
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

impl MinIntegrity {
    /// Returns the canonical policy-facing string for this integrity level.
    pub fn as_str(self) -> &'static str {
        use super::constants::policy_integrity;
        match self {
            MinIntegrity::None => policy_integrity::NONE,
            MinIntegrity::Unapproved => policy_integrity::UNAPPROVED,
            MinIntegrity::Approved => policy_integrity::APPROVED,
            MinIntegrity::Merged => policy_integrity::MERGED,
        }
    }
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
    /// GitHub usernames that are elevated to approved (writer) integrity regardless of
    /// their author_association. Analogous to trusted_bots but for regular human users.
    /// blocked_users takes precedence over trusted_users.
    pub trusted_users: Vec<String>,
    /// GitHub ReactionContent values (e.g. "THUMBS_UP", "HEART") that count as maintainer
    /// endorsement. When a maintainer with sufficient integrity reacts with one of these,
    /// the item's integrity is promoted to at least approved. Empty = feature disabled.
    pub endorsement_reactions: Vec<String>,
    /// GitHub ReactionContent values (e.g. "THUMBS_DOWN", "CONFUSED") that count as
    /// maintainer disapproval. When a maintainer with sufficient integrity reacts with
    /// one of these, the item's integrity is capped at `disapproval_integrity`.
    /// Disapproval overrides endorsement. Empty = feature disabled.
    pub disapproval_reactions: Vec<String>,
    /// The integrity level to cap an item at when a maintainer disapproval reaction is
    /// detected. Defaults to "none" when empty. Options: "none", "unapproved",
    /// "approved", "merged".
    pub disapproval_integrity: String,
    /// Minimum integrity level that a reactor must have for their reaction to count as
    /// endorsement or disapproval. Defaults to "approved" when empty. Options:
    /// "none", "unapproved", "approved", "merged".
    pub endorser_min_integrity: String,
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

/// Hierarchical integrity levels, ordered from lowest to highest.
const INTEGRITY_LEVELS: [(
    &str, // prefix
    &str, // base
); 4] = [
    (label_constants::NONE_PREFIX, label_constants::NONE),
    (label_constants::READER_PREFIX, label_constants::READER_BASE),
    (label_constants::WRITER_PREFIX, label_constants::WRITER_BASE),
    (label_constants::MERGED_PREFIX, label_constants::MERGED_BASE),
];

/// Build hierarchical integrity labels up to and including `max_level`.
///
/// Level 0 = none, 1 = reader, 2 = writer, 3 = merged.
/// Each level includes all labels below it (hierarchical subsumption).
fn build_integrity_labels(normalized_scope: &str, max_level: usize) -> Vec<String> {
    INTEGRITY_LEVELS[..=max_level]
        .iter()
        .map(|(prefix, base)| format_integrity_label(prefix, normalized_scope, base))
        .collect()
}

pub fn none_integrity(scope: &str, ctx: &PolicyContext) -> Vec<String> {
    build_integrity_labels(&normalize_scope(scope, ctx), 0)
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

/// Returns true if `username` matches any entry in `list` (case-insensitive).
fn username_in_list(username: &str, list: &[String]) -> bool {
    list.iter().any(|u| u.eq_ignore_ascii_case(username))
}

/// Check if a username appears in the configured blocked-users list (case-insensitive).
pub fn is_blocked_user(username: &str, ctx: &PolicyContext) -> bool {
    username_in_list(username, &ctx.blocked_users)
}

/// Extract GitHub label names from a content item's `labels` array.
///
/// Returns the `name` field from each element of the item's `labels` array.
fn extract_github_label_names(item: &Value) -> Vec<&str> {
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

/// Apply approval-label promotion: if the item carries a configured approval label,
/// raise integrity to at least writer (approved) level.
fn apply_approval_label_promotion(
    item: &Value,
    resource_type: &str,
    repo_full_name: &str,
    integrity: Vec<String>,
    ctx: &PolicyContext,
) -> Vec<String> {
    if let Some(label) = first_matching_approval_label(item, ctx) {
        let number = item.get(field_names::NUMBER).and_then(|v| v.as_u64()).unwrap_or(0);
        crate::log_info(&format!(
            "[integrity] {}:{}#{} promoted to approved (label '{}' in approval-labels)",
            resource_type, repo_full_name, number, label
        ));
        max_integrity(repo_full_name, integrity, writer_integrity(repo_full_name, ctx), ctx)
    } else {
        integrity
    }
}

// ============================================================================
// Reaction-based endorsement and disapproval helpers
// ============================================================================

/// Maximum number of reactions to inspect per item. Caps API enrichment calls.
const MAX_REACTIONS_TO_CHECK: usize = 20;

/// Return the effective `disapproval_integrity` level from context, defaulting to "none".
fn effective_disapproval_integrity<'a>(ctx: &'a PolicyContext) -> &'a str {
    let v = ctx.disapproval_integrity.trim();
    if v.is_empty() { "none" } else { v }
}

/// Return the effective `endorser_min_integrity` level from context, defaulting to "approved".
fn effective_endorser_min_integrity<'a>(ctx: &'a PolicyContext) -> &'a str {
    let v = ctx.endorser_min_integrity.trim();
    if v.is_empty() { "approved" } else { v }
}

/// Convert an integrity level name to its rank for comparison.
/// Returns: 1 = none, 2 = unapproved, 3 = approved, 4 = merged.
/// Unrecognised levels default to rank 3 (approved) with a warning log.
fn integrity_level_rank(level: &str) -> u8 {
    match level.trim().to_ascii_lowercase().as_str() {
        "none" => 1,
        "unapproved" => 2,
        "approved" => 3,
        "merged" => 4,
        other => {
            crate::log_warn(&format!(
                "integrity_level_rank: unrecognised level {:?}, defaulting to 'approved'",
                other
            ));
            3 // unrecognised → safe default is "approved" (matches endorser_min_integrity default)
        }
    }
}

/// Cap integrity at the given level. Returns `min(current, cap)` using the integrity hierarchy.
fn cap_integrity(
    scope: &str,
    current: Vec<String>,
    cap: Vec<String>,
    ctx: &PolicyContext,
) -> Vec<String> {
    let current_rank = integrity_rank(scope, &current, ctx);
    let cap_rank = integrity_rank(scope, &cap, ctx);
    integrity_for_rank(scope, current_rank.min(cap_rank), ctx)
}

/// Build the integrity `Vec<String>` for a given level name over a scope.
fn integrity_for_level(level: &str, scope: &str, ctx: &PolicyContext) -> Vec<String> {
    match level.trim().to_ascii_lowercase().as_str() {
        "none" => none_integrity(scope, ctx),
        "unapproved" => reader_integrity(scope, ctx),
        "approved" => writer_integrity(scope, ctx),
        "merged" => merged_integrity(scope, ctx),
        _ => none_integrity(scope, ctx), // safe default
    }
}

/// Core reaction evaluation helper.
///
/// Returns `true` if any reaction in `reaction_list` on the item was made by a
/// user whose collaborator permission meets or exceeds `endorser_min_integrity`.
///
/// - Uses `callback` to invoke `get_collaborator_permission` for each qualifying reactor.
/// - Inspects at most `MAX_REACTIONS_TO_CHECK` reactions to bound API call count.
/// - When `reactions` data is present but contains no per-user nodes (gateway mode),
///   emits a warning at most once per process lifetime and returns `false`.
pub fn has_maintainer_reaction_with_callback(
    item: &Value,
    repo_full_name: &str,
    reaction_list: &[String],
    endorser_min: &str,
    ctx: &PolicyContext,
    callback: GithubMcpCallback,
    reaction_kind: &str, // "endorsement" or "disapproval" — used for log messages
) -> bool {
    if reaction_list.is_empty() {
        return false;
    }

    let (owner, repo) = match repo_full_name.split_once('/') {
        Some((o, r)) if !o.is_empty() && !r.is_empty() => (o, r),
        _ => return false,
    };

    // Try to get per-user reaction nodes.
    let nodes = item
        .get("reactions")
        .and_then(|r| r.get("nodes"))
        .and_then(|n| n.as_array());

    let nodes = match nodes {
        Some(n) => n,
        None => {
            // If a `reactions` field is present but has no `nodes` array, we are in
            // gateway mode: reaction counts are available but reactor identity is not.
            if item.get("reactions").is_some() {
                // Use reaction-kind-specific flags so each kind logs its own warning once.
                let already_warned = match reaction_kind {
                    "endorsement" => ENDORSEMENT_GATEWAY_WARNING_EMITTED.swap(true, Ordering::Relaxed),
                    "disapproval" => DISAPPROVAL_GATEWAY_WARNING_EMITTED.swap(true, Ordering::Relaxed),
                    _ => false,
                };
                if !already_warned {
                    crate::log_warn(&format!(
                        "[integrity] {}: {}-reactions configured but reactor identity unavailable \
                         (gateway mode) — ignoring reactions for integrity evaluation",
                        repo_full_name, reaction_kind
                    ));
                }
            }
            return false;
        }
    };

    let endorser_min_rank = integrity_level_rank(endorser_min);
    let item_updated_at = item
        .get("lastEditedAt")
        .or_else(|| item.get("editedAt"))
        .or_else(|| item.get("last_edited_at"))
        .or_else(|| item.get("edited_at"))
        .or_else(|| item.get("updatedAt"))
        .or_else(|| item.get("updated_at"))
        .and_then(|v| v.as_str());

    for node in nodes.iter().take(MAX_REACTIONS_TO_CHECK) {
        let content = match node.get("content").and_then(|v| v.as_str()) {
            Some(c) => c,
            None => continue,
        };

        // Check if this reaction type is in our configured list (case-insensitive).
        if !reaction_list.iter().any(|r| r.eq_ignore_ascii_case(content)) {
            continue;
        }

        // Retrieve the reactor's login.
        let login = match node
            .get("user")
            .and_then(|u| u.get("login"))
            .and_then(|v| v.as_str())
            .filter(|l| !l.is_empty())
        {
            Some(l) => l,
            None => continue,
        };

        let reaction_created_at = node
            .get("createdAt")
            .or_else(|| node.get("created_at"))
            .and_then(|v| v.as_str());
        if let (Some(item_updated), Some(reaction_created)) = (item_updated_at, reaction_created_at) {
            if item_updated > reaction_created {
                crate::log_debug(&format!(
                    "[integrity] {}: skipping stale {} reaction {} from @{} \
                     (item updatedAt={} > reaction createdAt={})",
                    repo_full_name,
                    reaction_kind,
                    content,
                    login,
                    item_updated,
                    reaction_created
                ));
                continue;
            }
        }

        // Fetch reactor's collaborator permission to determine their integrity level.
        let perm = super::backend::get_collaborator_permission_with_callback(
            callback, owner, repo, login,
        );
        let reactor_integrity = collaborator_permission_floor(
            repo_full_name,
            perm.as_ref().and_then(|p| p.permission.as_deref()),
            ctx,
        );

        let reactor_rank = integrity_rank(repo_full_name, &reactor_integrity, ctx);

        if reactor_rank >= endorser_min_rank {
            crate::log_debug(&format!(
                "[integrity] {}: reactor @{} has permission={:?}, integrity rank {} >= \
                 endorser-min-integrity rank {} — counting as {} reaction {}",
                repo_full_name,
                login,
                perm.as_ref().and_then(|p| p.permission.as_deref()),
                reactor_rank,
                endorser_min_rank,
                reaction_kind,
                content
            ));
            return true;
        } else {
            crate::log_info(&format!(
                "[integrity] {}: reactor @{} has integrity rank {}, below \
                 endorser-min-integrity rank {} — ignoring {} {}",
                repo_full_name, login, reactor_rank, endorser_min_rank, reaction_kind, content
            ));
        }
    }

    false
}

/// Returns `true` if the item has a qualifying maintainer endorsement reaction.
///
/// Uses the production backend callback. Respects `PolicyContext.endorsement_reactions`
/// and `PolicyContext.endorser_min_integrity`.
pub fn has_maintainer_endorsement(item: &Value, repo_full_name: &str, ctx: &PolicyContext) -> bool {
    has_maintainer_reaction_with_callback(
        item,
        repo_full_name,
        &ctx.endorsement_reactions,
        effective_endorser_min_integrity(ctx),
        ctx,
        crate::invoke_backend,
        "endorsement",
    )
}

/// Returns `true` if the item has a qualifying maintainer disapproval reaction.
///
/// Uses the production backend callback. Respects `PolicyContext.disapproval_reactions`
/// and `PolicyContext.endorser_min_integrity`.
pub fn has_maintainer_disapproval(item: &Value, repo_full_name: &str, ctx: &PolicyContext) -> bool {
    has_maintainer_reaction_with_callback(
        item,
        repo_full_name,
        &ctx.disapproval_reactions,
        effective_endorser_min_integrity(ctx),
        ctx,
        crate::invoke_backend,
        "disapproval",
    )
}

/// Apply endorsement promotion: if a qualified maintainer has reacted with an endorsement
/// reaction, raise integrity to at least writer (approved) level.
fn apply_endorsement_promotion(
    item: &Value,
    resource_type: &str,
    repo_full_name: &str,
    integrity: Vec<String>,
    ctx: &PolicyContext,
) -> Vec<String> {
    if has_maintainer_endorsement(item, repo_full_name, ctx) {
        let number = item.get(field_names::NUMBER).and_then(|v| v.as_u64()).unwrap_or(0);
        crate::log_info(&format!(
            "[integrity] {}:{}#{} promoted to approved (maintainer endorsement reaction)",
            resource_type, repo_full_name, number
        ));
        max_integrity(repo_full_name, integrity, writer_integrity(repo_full_name, ctx), ctx)
    } else {
        integrity
    }
}

/// Apply disapproval demotion: if a qualified maintainer has reacted with a disapproval
/// reaction, cap the item's integrity at the configured `disapproval_integrity` level.
/// Disapproval overrides endorsement and approval labels (runs last in the chain).
fn apply_disapproval_demotion(
    item: &Value,
    resource_type: &str,
    repo_full_name: &str,
    integrity: Vec<String>,
    ctx: &PolicyContext,
) -> Vec<String> {
    if has_maintainer_disapproval(item, repo_full_name, ctx) {
        let number = item.get(field_names::NUMBER).and_then(|v| v.as_u64()).unwrap_or(0);
        let demote_level = effective_disapproval_integrity(ctx);
        crate::log_info(&format!(
            "[integrity] {}:{}#{} demoted to {} (maintainer disapproval reaction)",
            resource_type, repo_full_name, number, demote_level
        ));
        let cap = integrity_for_level(demote_level, repo_full_name, ctx);
        cap_integrity(repo_full_name, integrity, cap, ctx)
    } else {
        integrity
    }
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
#[cfg(test)]
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
                ScopeKind::Public => vec![label_constants::PRIVATE_BASE.to_string()],
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
        vec![label_constants::PRIVATE_BASE.to_string()]
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

/// Extract (owner, repo, repo_id) from tool_args, falling back to the
/// `query` field's `repo:` qualifier when the explicit fields are absent.
/// This is the canonical resolution for tools that accept either explicit
/// owner/repo args OR a free-text search query with a `repo:` scope.
pub(crate) fn extract_repo_scope_with_query_fallback(
    tool_args: &Value,
) -> (String, String, String) {
    let (owner, repo, repo_id) = extract_repo_info(tool_args);
    if owner.is_empty() || repo.is_empty() {
        let query = tool_args.get("query").and_then(|v| v.as_str()).unwrap_or("");
        let (q_owner, q_repo, q_repo_id) = extract_repo_info_from_search_query(query);
        if !q_repo_id.is_empty() {
            return (q_owner, q_repo, q_repo_id);
        }
    }
    (owner, repo, repo_id)
}

pub(crate) fn extract_repo_from_github_url(url: &str) -> Option<String> {
    let parse_owner_repo = |path: &str| {
        let mut parts = path.split('/').filter(|segment| !segment.is_empty());
        let owner = parts.next()?;
        let repo = parts.next()?;
        Some(format!("{}/{}", owner, repo))
    };

    // Fast path for well-known github.com URLs
    if let Some(path) = url
        .strip_prefix("https://api.github.com/repos/")
        .or_else(|| url.strip_prefix("http://api.github.com/repos/"))
        .or_else(|| url.strip_prefix("https://github.com/"))
        .or_else(|| url.strip_prefix("http://github.com/"))
    {
        return parse_owner_repo(path);
    }

    // Generic path: handle GHEC (api.*.ghe.com) and GHES (*/api/v3/repos/*)
    // by looking for /repos/<owner>/<repo> in the URL path.
    if let Some(pos) = url.find("/repos/") {
        return parse_owner_repo(&url[pos + 7..]);
    }

    None
}

/// Extract repository full name from a response item
/// Tries multiple fields in order: full_name, repository.full_name,
/// base.repo.full_name, head.repo.full_name, then URL parsing from
/// repository_url, html_url, and url.
/// Returns empty string if no repo info found
pub fn extract_repo_from_item(item: &Value) -> String {
    // Direct full_name (repositories)
    if let Some(name) = item.get(field_names::FULL_NAME).and_then(|v| v.as_str()) {
        return name.to_string();
    }
    // repository.full_name (issues, PRs with repo info)
    if let Some(name) = item
        .get("repository")
        .and_then(|r| r.get(field_names::FULL_NAME))
        .and_then(|v| v.as_str())
    {
        return name.to_string();
    }
    // base.repo.full_name (pull requests)
    if let Some(name) = item
        .get("base")
        .and_then(|b| b.get("repo"))
        .and_then(|r| r.get(field_names::FULL_NAME))
        .and_then(|v| v.as_str())
    {
        return name.to_string();
    }
    // head.repo.full_name (pull requests)
    if let Some(name) = item
        .get("head")
        .and_then(|h| h.get("repo"))
        .and_then(|r| r.get(field_names::FULL_NAME))
        .and_then(|v| v.as_str())
    {
        return name.to_string();
    }
    // URL field fallback (repository_url for search results, html_url / url as generic fallbacks)
    for field in &["repository_url", "html_url", "url"] {
        if let Some(url) = item.get(field).and_then(|v| v.as_str()) {
            if let Some(repo_id) = extract_repo_from_github_url(url) {
                return repo_id;
            }
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
    if let Some((arr, pointer)) = find_graphql_nodes_with_path(response) {
        return (Some(arr), pointer.to_string());
    }

    (None, String::new())
}

/// Collect items from a response that is either a JSON array or a single object.
///
/// Returns a `Vec<&Value>` of items to process. Wrappers like MCP text envelopes
/// and search-result metadata objects are excluded from single-object promotion.
pub(crate) fn collect_items_simple(response: &Value) -> Vec<&Value> {
    if let Some(arr) = response.as_array() {
        arr.iter().collect()
    } else if response.is_object()
        && !is_search_result_wrapper(response)
        && !is_mcp_text_wrapper(response)
    {
        vec![response]
    } else {
        vec![]
    }
}

/// GraphQL collection fields under data.repository and their JSON Pointer paths.
const GRAPHQL_COLLECTION_FIELDS: &[(&str, &str)] = &[
    ("issues", "/data/repository/issues/nodes"),
    ("pullRequests", "/data/repository/pullRequests/nodes"),
    ("discussions", "/data/repository/discussions/nodes"),
    ("discussionCategories", "/data/repository/discussionCategories/nodes"),
];

/// Private helper: find GraphQL nodes and return both the array and its JSON Pointer path.
fn find_graphql_nodes_with_path(response: &Value) -> Option<(&Vec<Value>, &'static str)> {
    let data = response.get("data")?;
    if let Some(repo) = data.get("repository") {
        for (field, pointer) in GRAPHQL_COLLECTION_FIELDS {
            if let Some(arr) = repo.get(*field).and_then(|v| v.get("nodes")).and_then(|v| v.as_array()) {
                return Some((arr, pointer));
            }
        }
    }
    if let Some(arr) = data.get("search").and_then(|v| v.get("nodes")).and_then(|v| v.as_array()) {
        return Some((arr, "/data/search/nodes"));
    }
    None
}

/// Extract the items array from a GraphQL response.
/// Traverses data.repository.<field>.nodes and data.search.nodes paths.
pub fn extract_graphql_nodes(response: &Value) -> Option<&Vec<Value>> {
    find_graphql_nodes_with_path(response).map(|(arr, _)| arr)
}

/// Returns true if the response is a GraphQL wrapper (has a "data" key).
/// Used to prevent treating the entire GraphQL object as a single item.
pub fn is_graphql_wrapper(response: &Value) -> bool {
    response.get("data").is_some()
}

/// Returns true if the response is a search result wrapper.
/// Handles both REST format (`total_count`) and GraphQL format (`totalCount`)
/// returned by different MCP server versions. Used to prevent treating
/// `{"total_count":0,"incomplete_results":false}` or
/// `{"totalCount":0,"issues":[],"pageInfo":{}}` as single data items.
pub fn is_search_result_wrapper(response: &Value) -> bool {
    response.get("total_count").is_some() || response.get("totalCount").is_some()
}

/// Returns the total count from a search result wrapper, handling both
/// REST format (`total_count`) and GraphQL format (`totalCount`).
pub fn search_result_total_count(response: &Value) -> Option<u64> {
    response
        .get("total_count")
        .and_then(|v| v.as_u64())
        .or_else(|| response.get("totalCount").and_then(|v| v.as_u64()))
}

/// Returns true if the response is an MCP content wrapper where the text was not
/// parseable as JSON. These are `{"content":[{"type":"text","text":"..."}]}` objects
/// that `extract_mcp_response` left unwrapped because the text field was not valid
/// JSON (e.g. plain-text error messages or human-readable summaries).
pub fn is_mcp_text_wrapper(response: &Value) -> bool {
    response
        .get("content")
        .and_then(|v| v.as_array())
        .and_then(|arr| arr.first())
        .and_then(|item| item.get("type"))
        .and_then(|t| t.as_str())
        .map(|t| t == "text")
        .unwrap_or(false)
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
    build_integrity_labels(&normalize_scope(scope, ctx), 1)
}

/// Generate approved-level integrity tags for a scope.
/// Includes unapproved level (hierarchical: approved > unapproved)
pub fn writer_integrity(scope: &str, ctx: &PolicyContext) -> Vec<String> {
    build_integrity_labels(&normalize_scope(scope, ctx), 2)
}

/// Generate merged-level integrity tags for a scope.
/// Includes approved and unapproved (hierarchical: merged > approved > unapproved)
pub fn merged_integrity(scope: &str, ctx: &PolicyContext) -> Vec<String> {
    build_integrity_labels(&normalize_scope(scope, ctx), 3)
}

fn integrity_rank(scope: &str, labels: &[String], ctx: &PolicyContext) -> u8 {
    let normalized_scope = normalize_scope(scope, ctx);

    // Check from highest to lowest, allocating one label at a time.
    for (rank, (prefix, base)) in INTEGRITY_LEVELS.iter().enumerate().rev() {
        let tag = format_integrity_label(prefix, &normalized_scope, base);
        if labels.iter().any(|l| l == &tag) {
            return (rank + 1) as u8;
        }
    }
    0
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
/// - CONTRIBUTOR, FIRST_TIME_CONTRIBUTOR, NONE => unapproved
/// - FIRST_TIMER, missing, unknown => [] (the `none:<scope>` floor is applied later by baseline enforcement)
///
/// ### `NONE` vs `FIRST_TIMER`
///
/// GitHub's API definitions for these values are intentionally vague
/// (see <https://docs.github.com/en/graphql/reference/enums#commentauthorassociation>):
///
/// - `FIRST_TIMER`: "Author has not previously committed to GitHub."
///   This indicates a brand-new GitHub account with no commit history anywhere.
///
/// - `FIRST_TIME_CONTRIBUTOR`: "Author has not previously committed to the repository."
///   The user has committed elsewhere on GitHub but not to this specific repo.
///
/// - `NONE`: "Author has no association with the repository."
///   This does **not** mean the user is established or trustworthy — only that
///   they have no special relationship with the repo. In practice `NONE` covers
///   users who have opened issues or commented but never committed, as well as
///   accounts that have simply never interacted before.
///
/// We map `NONE` to `unapproved` (same as `FIRST_TIME_CONTRIBUTOR`) because
/// both represent users with no prior contributions to the specific repo who
/// are not brand-new to GitHub. The only value that indicates a truly new
/// GitHub account is `FIRST_TIMER`.
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
        "CONTRIBUTOR" | "FIRST_TIME_CONTRIBUTOR" | "NONE" => reader_integrity(scope, ctx),
        _ => vec![], // FIRST_TIMER or any unrecognised value
    }
}

/// Extract the author login from an item, checking common GitHub API fields.
/// Returns empty string if no login found.
fn extract_author_login(item: &Value) -> &str {
    // Issues and PRs use user.login
    let login = get_nested_str(item, "user", field_names::LOGIN);
    if !login.is_empty() {
        return login;
    }
    // Commits use author.login
    get_nested_str(item, "author", field_names::LOGIN)
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
/// Users in the trusted_users list are also elevated to approved integrity.
pub fn author_association_floor(item: &Value, scope: &str, ctx: &PolicyContext) -> Vec<String> {
    let author_login = extract_author_login(item);
    if !author_login.is_empty() && is_any_trusted_actor(author_login, ctx) {
        return writer_integrity(scope, ctx);
    }

    let association = item
        .get("author_association")
        .and_then(|v| v.as_str())
        .or_else(|| item.get("authorAssociation").and_then(|v| v.as_str()));

    author_association_floor_from_str(scope, association, ctx)
}

/// Map collaborator permission level to integrity.
/// Uses the effective permission from GET /repos/{owner}/{repo}/collaborators/{username}/permission
/// which correctly reflects inherited org permissions (unlike author_association).
///
/// Mapping:
/// - admin, maintain, write => approved (writer integrity)
/// - triage, read => unapproved (reader integrity)
/// - none, missing => none
pub fn collaborator_permission_floor(
    scope: &str,
    permission: Option<&str>,
    ctx: &PolicyContext,
) -> Vec<String> {
    let Some(raw) = permission else {
        return vec![];
    };

    let normalized = raw.trim().to_ascii_lowercase();
    match normalized.as_str() {
        "admin" | "maintain" | "write" => writer_integrity(scope, ctx),
        "triage" | "read" => reader_integrity(scope, ctx),
        _ => vec![], // "none" or any unrecognised value → no integrity
    }
}

/// Elevate integrity via collaborator permission fallback for org repos.
///
/// Rank threshold for writer-level integrity (none=1, reader=2, writer=3, merged=4).
const WRITER_RANK: u8 = 3;

/// Attempt to elevate integrity for an author in a public repository
/// by checking their effective collaborator permission.
///
/// When `author_association` gives insufficient integrity (below writer level),
/// this function checks the user's effective permission via the GitHub
/// collaborator permission API. This correctly handles owners/admins whose
/// `author_association` is absent or reported as "NONE".
///
/// Backend calls are cached per-user, so repeated lookups for the same author
/// across list/search items are inexpensive.
///
/// Parameters:
/// - `author_login`: the issue/PR/commit author's GitHub login
/// - `repo_full_name`: "owner/repo" string
/// - `resource_label`: label for logging (e.g. "issue", "pr", "commit")
/// - `resource_id`: number or identifier for logging
/// - `integrity`: current integrity labels to potentially elevate
/// - `ctx`: policy context
///
/// Returns the (potentially elevated) integrity labels.
pub fn elevate_via_collaborator_permission(
    author_login: &str,
    repo_full_name: &str,
    resource_label: &str,
    resource_id: &str,
    integrity: Vec<String>,
    ctx: &PolicyContext,
) -> Vec<String> {
    if integrity_rank(repo_full_name, &integrity, ctx) >= WRITER_RANK || author_login.is_empty() {
        return integrity;
    }
    let (owner, repo) = match repo_full_name.split_once('/') {
        Some((o, r)) if !o.is_empty() && !r.is_empty() => (o, r),
        _ => return integrity,
    };
    crate::log_debug(&format!(
        "[integrity] {}:{}: author_association floor below writer (rank={}), checking collaborator permission for {}",
        resource_label, resource_id, integrity_rank(repo_full_name, &integrity, ctx), author_login
    ));
    if let Some(collab) = super::backend::get_collaborator_permission(owner, repo, author_login) {
        let perm_floor = collaborator_permission_floor(repo_full_name, collab.permission.as_deref(), ctx);
        let merged = max_integrity(repo_full_name, integrity, perm_floor, ctx);
        crate::log_debug(&format!(
            "[integrity] {}:{}: collaborator permission={:?} → merged rank={}",
            resource_label, resource_id, collab.permission, integrity_rank(repo_full_name, &merged, ctx)
        ));
        merged
    } else {
        crate::log_debug(&format!(
            "[integrity] {}:{}: collaborator permission lookup returned None for {}, keeping author_association floor",
            resource_label, resource_id, author_login
        ));
        integrity
    }
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
        let number = item.get(field_names::NUMBER).and_then(|v| v.as_u64()).unwrap_or(0);
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
            .get(field_names::NUMBER)
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
                    // Elevate trusted bots and trusted users
                    let enriched_floor = if let Some(ref login) = facts.author_login {
                        if is_any_trusted_actor(login, ctx) {
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

    // Collaborator permission fallback for org repos (handles org owners/admins
    // whose author_association is "NONE" due to inherited org access).
    if !repo_private {
        let number = item.get(field_names::NUMBER).and_then(|v| v.as_u64()).unwrap_or(0);
        integrity = elevate_via_collaborator_permission(
            author_login, repo_full_name, "pr", &format!("{}#{}", repo_full_name, number),
            integrity, ctx,
        );
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
    let integrity = apply_approval_label_promotion(item, "pr", repo_full_name, integrity, ctx);
    // Step 3: Apply endorsement promotion — raise to at least approved if a qualified
    //         maintainer reacted with a configured endorsement reaction.
    let integrity = apply_endorsement_promotion(item, "pr", repo_full_name, integrity, ctx);
    // Step 4: Apply disapproval demotion — cap at configured level if a qualified
    //         maintainer reacted with a configured disapproval reaction.
    //         Disapproval runs last so it always wins over all promotion rules.
    apply_disapproval_demotion(item, "pr", repo_full_name, integrity, ctx)
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
        let number = item.get(field_names::NUMBER).and_then(|v| v.as_u64()).unwrap_or(0);
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
            .get(field_names::NUMBER)
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

    // Collaborator permission fallback for org repos (handles org owners/admins
    // whose author_association is "NONE" due to inherited org access).
    if !repo_private {
        let number = item.get(field_names::NUMBER).and_then(|v| v.as_u64()).unwrap_or(0);
        integrity = elevate_via_collaborator_permission(
            author_login, repo_full_name, "issue", &format!("{}#{}", repo_full_name, number),
            integrity, ctx,
        );
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
    let integrity = apply_approval_label_promotion(item, "issue", repo_full_name, integrity, ctx);
    // Step 3: Apply endorsement promotion — raise to at least approved if a qualified
    //         maintainer reacted with a configured endorsement reaction.
    let integrity = apply_endorsement_promotion(item, "issue", repo_full_name, integrity, ctx);
    // Step 4: Apply disapproval demotion — cap at configured level if a qualified
    //         maintainer reacted with a configured disapproval reaction.
    //         Disapproval runs last so it always wins over all promotion rules.
    apply_disapproval_demotion(item, "issue", repo_full_name, integrity, ctx)
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

    // For public personal repositories, commit payloads often omit
    // `author_association`. Ensure owner-authored commits still get writer floor.
    if !repo_private {
        if let Some((owner, _repo)) = repo_full_name.split_once('/') {
            if author_login.eq_ignore_ascii_case(owner) {
                integrity = max_integrity(
                    repo_full_name,
                    integrity,
                    writer_integrity(repo_full_name, ctx),
                    ctx,
                );
            }
        }
    }

    // Collaborator permission fallback for public repos (handles owners/admins
    // whose author_association is missing or "NONE").
    if !repo_private {
        let sha = item.get("sha").and_then(|v| v.as_str()).unwrap_or("unknown");
        let short_sha = if sha.len() > 8 { &sha[..8] } else { sha };
        integrity = elevate_via_collaborator_permission(
            author_login, repo_full_name, "commit", &format!("{}@{}", repo_full_name, short_sha),
            integrity, ctx,
        );
    }

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
    username.eq_ignore_ascii_case("dependabot[bot]")
        || username.eq_ignore_ascii_case("github-actions[bot]")
        || username.eq_ignore_ascii_case("github-actions")
        || username.eq_ignore_ascii_case("app/github-actions")
        || username.eq_ignore_ascii_case("github-merge-queue[bot]")
        || username.eq_ignore_ascii_case("copilot")
        || username.eq_ignore_ascii_case("copilot-swe-agent[bot]")
        || username.eq_ignore_ascii_case("copilot-swe-agent")
        || username.eq_ignore_ascii_case("app/copilot-swe-agent")
}

/// Check if a user is in the gateway-configured trusted bot list.
///
/// This checks the `trusted_bots` list in `PolicyContext`, which is populated from
/// the gateway configuration's `trustedBots` field. Comparison is case-insensitive.
/// This list is additive and cannot remove entries from the built-in trusted bot list.
pub fn is_configured_trusted_bot(username: &str, ctx: &PolicyContext) -> bool {
    username_in_list(username, &ctx.trusted_bots)
}

/// Check if a user is in the gateway-configured trusted users list.
///
/// This checks the `trusted_users` list in `PolicyContext`, which is populated from
/// the allow-only policy's `trusted-users` field. Users in this list receive approved
/// (writer) integrity regardless of their `author_association`. Comparison is
/// case-insensitive. `blocked_users` takes precedence over `trusted_users`.
pub fn is_trusted_user(username: &str, ctx: &PolicyContext) -> bool {
    username_in_list(username, &ctx.trusted_users)
}

/// Returns `true` if `username` belongs to any trusted actor tier:
/// first-party bots, gateway-configured bots, or trusted users.
pub(crate) fn is_any_trusted_actor(username: &str, ctx: &PolicyContext) -> bool {
    is_trusted_first_party_bot(username)
        || is_configured_trusted_bot(username, ctx)
        || is_trusted_user(username, ctx)
}


#[cfg(test)]
mod tests {
    use super::*;

    fn test_ctx() -> PolicyContext {
        PolicyContext::default()
    }

    #[test]
    fn test_is_any_trusted_actor_tiers_and_negative() {
        let ctx = PolicyContext {
            trusted_bots: vec!["custom-bot".to_string()],
            trusted_users: vec!["trusted-human".to_string()],
            ..Default::default()
        };

        assert!(is_any_trusted_actor("github-actions[bot]", &ctx));
        assert!(is_any_trusted_actor("custom-bot", &ctx));
        assert!(is_any_trusted_actor("trusted-human", &ctx));
        assert!(!is_any_trusted_actor("random-user", &ctx));
    }

    #[test]
    fn test_collaborator_permission_floor_admin() {
        let ctx = test_ctx();
        let result = collaborator_permission_floor("owner/repo", Some("admin"), &ctx);
        assert!(!result.is_empty(), "admin should give approved integrity");
        assert_eq!(result.len(), 3, "writer integrity has 3 tags (none+reader+writer)");
    }

    #[test]
    fn test_collaborator_permission_floor_maintain() {
        let ctx = test_ctx();
        let result = collaborator_permission_floor("owner/repo", Some("maintain"), &ctx);
        assert_eq!(result.len(), 3, "maintain should give writer/approved integrity");
    }

    #[test]
    fn test_collaborator_permission_floor_write() {
        let ctx = test_ctx();
        let result = collaborator_permission_floor("owner/repo", Some("write"), &ctx);
        assert_eq!(result.len(), 3, "write should give writer/approved integrity");
    }

    #[test]
    fn test_collaborator_permission_floor_triage() {
        let ctx = test_ctx();
        let result = collaborator_permission_floor("owner/repo", Some("triage"), &ctx);
        assert_eq!(result.len(), 2, "triage should give reader/unapproved integrity");
    }

    #[test]
    fn test_collaborator_permission_floor_read() {
        let ctx = test_ctx();
        let result = collaborator_permission_floor("owner/repo", Some("read"), &ctx);
        assert_eq!(result.len(), 2, "read should give reader/unapproved integrity");
    }

    #[test]
    fn test_collaborator_permission_floor_none() {
        let ctx = test_ctx();
        let result = collaborator_permission_floor("owner/repo", Some("none"), &ctx);
        assert!(result.is_empty(), "none permission should give empty integrity");
    }

    #[test]
    fn test_collaborator_permission_floor_missing() {
        let ctx = test_ctx();
        let result = collaborator_permission_floor("owner/repo", None, &ctx);
        assert!(result.is_empty(), "missing permission should give empty integrity");
    }

    #[test]
    fn test_collaborator_permission_floor_case_insensitive() {
        let ctx = test_ctx();
        let upper = collaborator_permission_floor("owner/repo", Some("ADMIN"), &ctx);
        let mixed = collaborator_permission_floor("owner/repo", Some("Admin"), &ctx);
        let lower = collaborator_permission_floor("owner/repo", Some("admin"), &ctx);
        assert_eq!(upper, mixed);
        assert_eq!(mixed, lower);
        assert_eq!(lower.len(), 3);
    }

    #[test]
    fn test_collaborator_permission_floor_whitespace() {
        let ctx = test_ctx();
        let result = collaborator_permission_floor("owner/repo", Some("  write  "), &ctx);
        assert_eq!(result.len(), 3, "should trim whitespace");
    }

    #[test]
    fn test_collaborator_permission_floor_unknown() {
        let ctx = test_ctx();
        let result = collaborator_permission_floor("owner/repo", Some("unknown"), &ctx);
        assert!(result.is_empty(), "unknown permission should give empty integrity");
    }

    #[test]
    fn test_collaborator_permission_matches_author_association_writer() {
        let ctx = test_ctx();
        let perm_result = collaborator_permission_floor("owner/repo", Some("write"), &ctx);
        let assoc_result = author_association_floor_from_str("owner/repo", Some("COLLABORATOR"), &ctx);
        assert_eq!(perm_result, assoc_result, "write permission and COLLABORATOR association should produce same integrity");
    }

    #[test]
    fn test_collaborator_permission_matches_author_association_reader() {
        let ctx = test_ctx();
        let perm_result = collaborator_permission_floor("owner/repo", Some("read"), &ctx);
        let assoc_result = author_association_floor_from_str("owner/repo", Some("CONTRIBUTOR"), &ctx);
        assert_eq!(perm_result, assoc_result, "read permission and CONTRIBUTOR association should produce same integrity");
    }

    #[test]
    fn test_min_integrity_as_str() {
        use super::super::constants::policy_integrity;
        assert_eq!(MinIntegrity::None.as_str(), policy_integrity::NONE);
        assert_eq!(MinIntegrity::Unapproved.as_str(), policy_integrity::UNAPPROVED);
        assert_eq!(MinIntegrity::Approved.as_str(), policy_integrity::APPROVED);
        assert_eq!(MinIntegrity::Merged.as_str(), policy_integrity::MERGED);
    }

    // =========================================================================
    // Tests for reaction-based endorsement / disapproval helpers
    // =========================================================================

    fn ctx_with_endorsement_reactions(reactions: Vec<&str>) -> PolicyContext {
        PolicyContext {
            endorsement_reactions: reactions.into_iter().map(|s| s.to_string()).collect(),
            ..Default::default()
        }
    }

    /// Mock callback that returns admin permission for any user.
    fn admin_permission_callback(_tool: &str, _args: &str, buf: &mut [u8]) -> Result<usize, i32> {
        let response = r#"{"permission":"admin","user":{"login":"maintainer"}}"#;
        let bytes = response.as_bytes();
        let len = bytes.len().min(buf.len());
        buf[..len].copy_from_slice(&bytes[..len]);
        Ok(len)
    }

    /// Mock callback that returns read (low) permission for any user.
    fn read_permission_callback(_tool: &str, _args: &str, buf: &mut [u8]) -> Result<usize, i32> {
        let response = r#"{"permission":"read","user":{"login":"external"}}"#;
        let bytes = response.as_bytes();
        let len = bytes.len().min(buf.len());
        buf[..len].copy_from_slice(&bytes[..len]);
        Ok(len)
    }

    /// Mock callback that returns an error (simulates unavailable backend).
    fn error_callback(_tool: &str, _args: &str, _buf: &mut [u8]) -> Result<usize, i32> {
        Err(-1)
    }

    #[test]
    fn test_has_maintainer_reaction_no_reactions_in_ctx() {
        let ctx = PolicyContext::default();
        // endorsement_reactions is empty — should always return false
        let item = serde_json::json!({
            "number": 1,
            "reactions": {"nodes": [{"user": {"login": "alice"}, "content": "THUMBS_UP"}]}
        });
        assert!(!has_maintainer_reaction_with_callback(
            &item, "owner/repo", &ctx.endorsement_reactions, "approved", &ctx,
            admin_permission_callback, "endorsement"
        ));
    }

    #[test]
    fn test_has_maintainer_reaction_with_matching_admin_reactor() {
        let ctx = ctx_with_endorsement_reactions(vec!["THUMBS_UP"]);
        let item = serde_json::json!({
            "number": 42,
            "reactions": {"nodes": [{"user": {"login": "alice"}, "content": "THUMBS_UP"}]}
        });
        assert!(has_maintainer_reaction_with_callback(
            &item, "owner/repo", &ctx.endorsement_reactions, "approved", &ctx,
            admin_permission_callback, "endorsement"
        ));
    }

    #[test]
    fn test_has_maintainer_reaction_reactor_below_threshold() {
        let ctx = ctx_with_endorsement_reactions(vec!["THUMBS_UP"]);
        let item = serde_json::json!({
            "number": 42,
            "reactions": {"nodes": [{"user": {"login": "external"}, "content": "THUMBS_UP"}]}
        });
        // read permission → unapproved integrity, below "approved" threshold
        assert!(!has_maintainer_reaction_with_callback(
            &item, "owner/repo", &ctx.endorsement_reactions, "approved", &ctx,
            read_permission_callback, "endorsement"
        ));
    }

    #[test]
    fn test_has_maintainer_reaction_wrong_content() {
        let ctx = ctx_with_endorsement_reactions(vec!["HEART"]);
        let item = serde_json::json!({
            "number": 42,
            "reactions": {"nodes": [{"user": {"login": "alice"}, "content": "THUMBS_UP"}]}
        });
        // Reaction content "THUMBS_UP" is not in endorsement list ["HEART"]
        assert!(!has_maintainer_reaction_with_callback(
            &item, "owner/repo", &ctx.endorsement_reactions, "approved", &ctx,
            admin_permission_callback, "endorsement"
        ));
    }

    #[test]
    fn test_has_maintainer_reaction_case_insensitive_content() {
        let ctx = ctx_with_endorsement_reactions(vec!["thumbs_up"]);
        let item = serde_json::json!({
            "number": 42,
            "reactions": {"nodes": [{"user": {"login": "alice"}, "content": "THUMBS_UP"}]}
        });
        // Case-insensitive match between "thumbs_up" (config) and "THUMBS_UP" (data)
        assert!(has_maintainer_reaction_with_callback(
            &item, "owner/repo", &ctx.endorsement_reactions, "approved", &ctx,
            admin_permission_callback, "endorsement"
        ));
    }

    #[test]
    fn test_has_maintainer_reaction_no_nodes_gateway_mode() {
        // reactions field present but no nodes array (gateway mode — only counts available)
        let ctx = ctx_with_endorsement_reactions(vec!["THUMBS_UP"]);
        let item = serde_json::json!({
            "number": 42,
            "reactions": {"total_count": 3, "thumbs_up": 3, "+1": 3}
        });
        // Should return false (graceful degradation)
        assert!(!has_maintainer_reaction_with_callback(
            &item, "owner/repo", &ctx.endorsement_reactions, "approved", &ctx,
            admin_permission_callback, "endorsement"
        ));
    }

    #[test]
    fn test_has_maintainer_reaction_no_reactions_field() {
        let ctx = ctx_with_endorsement_reactions(vec!["THUMBS_UP"]);
        let item = serde_json::json!({"number": 42, "title": "no reactions"});
        // No reactions field → skip silently
        assert!(!has_maintainer_reaction_with_callback(
            &item, "owner/repo", &ctx.endorsement_reactions, "approved", &ctx,
            admin_permission_callback, "endorsement"
        ));
    }

    #[test]
    fn test_has_maintainer_reaction_empty_nodes() {
        let ctx = ctx_with_endorsement_reactions(vec!["THUMBS_UP"]);
        let item = serde_json::json!({"number": 42, "reactions": {"nodes": []}});
        assert!(!has_maintainer_reaction_with_callback(
            &item, "owner/repo", &ctx.endorsement_reactions, "approved", &ctx,
            admin_permission_callback, "endorsement"
        ));
    }

    #[test]
    fn test_has_maintainer_reaction_backend_error_skips() {
        let ctx = ctx_with_endorsement_reactions(vec!["THUMBS_UP"]);
        // Use a unique login to avoid hitting the global permission cache populated
        // by other tests (e.g. admin_permission_callback caching "alice").
        let item = serde_json::json!({
            "number": 42,
            "reactions": {"nodes": [{"user": {"login": "error-test-user"}, "content": "THUMBS_UP"}]}
        });
        // Backend error → can't confirm permission → should not count as endorsement
        assert!(!has_maintainer_reaction_with_callback(
            &item, "owner/repo", &ctx.endorsement_reactions, "approved", &ctx,
            error_callback, "endorsement"
        ));
    }

    #[test]
    fn test_has_maintainer_reaction_honors_unmodified_item_endorsement() {
        let ctx = ctx_with_endorsement_reactions(vec!["THUMBS_UP"]);
        let item = serde_json::json!({
            "number": 42,
            "updatedAt": "2026-04-20T00:00:00Z",
            "reactions": {"nodes": [{
                "user": {"login": "alice"},
                "content": "THUMBS_UP",
                "createdAt": "2026-04-20T00:00:00Z"
            }]}
        });
        assert!(has_maintainer_reaction_with_callback(
            &item, "owner/repo", &ctx.endorsement_reactions, "approved", &ctx,
            admin_permission_callback, "endorsement"
        ));
    }

    #[test]
    fn test_has_maintainer_reaction_skips_stale_endorsement_after_edit() {
        let ctx = ctx_with_endorsement_reactions(vec!["THUMBS_UP"]);
        let item = serde_json::json!({
            "number": 42,
            "updatedAt": "2026-04-21T00:00:00Z",
            "reactions": {"nodes": [{
                "user": {"login": "alice"},
                "content": "THUMBS_UP",
                "createdAt": "2026-04-20T00:00:00Z"
            }]}
        });
        assert!(!has_maintainer_reaction_with_callback(
            &item, "owner/repo", &ctx.endorsement_reactions, "approved", &ctx,
            admin_permission_callback, "endorsement"
        ));
    }

    #[test]
    fn test_has_maintainer_reaction_honors_endorsement_added_after_edit() {
        let ctx = ctx_with_endorsement_reactions(vec!["THUMBS_UP"]);
        let item = serde_json::json!({
            "number": 42,
            "updated_at": "2026-04-20T00:00:00Z",
            "reactions": {"nodes": [{
                "user": {"login": "alice"},
                "content": "THUMBS_UP",
                "createdAt": "2026-04-21T00:00:00Z"
            }]}
        });
        assert!(has_maintainer_reaction_with_callback(
            &item, "owner/repo", &ctx.endorsement_reactions, "approved", &ctx,
            admin_permission_callback, "endorsement"
        ));
    }

    #[test]
    fn test_has_maintainer_reaction_counts_fresh_when_stale_and_fresh_mixed() {
        let ctx = ctx_with_endorsement_reactions(vec!["THUMBS_UP"]);
        let item = serde_json::json!({
            "number": 42,
            "updatedAt": "2026-04-21T00:00:00Z",
            "reactions": {"nodes": [
                {
                    "user": {"login": "alice"},
                    "content": "THUMBS_UP",
                    "createdAt": "2026-04-20T00:00:00Z"
                },
                {
                    "user": {"login": "bob"},
                    "content": "THUMBS_UP",
                    "createdAt": "2026-04-22T00:00:00Z"
                }
            ]}
        });
        assert!(has_maintainer_reaction_with_callback(
            &item, "owner/repo", &ctx.endorsement_reactions, "approved", &ctx,
            admin_permission_callback, "endorsement"
        ));
    }

    #[test]
    fn test_has_maintainer_reaction_missing_timestamps_keeps_existing_behavior() {
        let ctx = ctx_with_endorsement_reactions(vec!["THUMBS_UP"]);
        let item = serde_json::json!({
            "number": 42,
            "reactions": {"nodes": [{
                "user": {"login": "alice"},
                "content": "THUMBS_UP"
            }]}
        });
        assert!(has_maintainer_reaction_with_callback(
            &item, "owner/repo", &ctx.endorsement_reactions, "approved", &ctx,
            admin_permission_callback, "endorsement"
        ));
    }

    #[test]
    fn test_cap_integrity_at_none() {
        let ctx = test_ctx();
        let scope = "owner/repo";
        let current = writer_integrity(scope, &ctx);
        let cap = none_integrity(scope, &ctx);
        let result = cap_integrity(scope, current, cap, &ctx);
        assert_eq!(result, none_integrity(scope, &ctx), "capping approved at none should give none");
    }

    #[test]
    fn test_cap_integrity_cap_higher_than_current() {
        let ctx = test_ctx();
        let scope = "owner/repo";
        let current = reader_integrity(scope, &ctx);
        let cap = writer_integrity(scope, &ctx);
        // cap > current → should stay at current (min(reader, writer) = reader)
        let result = cap_integrity(scope, current.clone(), cap, &ctx);
        assert_eq!(result, current, "cap higher than current should not change integrity");
    }

    #[test]
    fn test_integrity_for_level_mapping() {
        let ctx = test_ctx();
        let scope = "owner/repo";
        assert_eq!(integrity_for_level("none", scope, &ctx), none_integrity(scope, &ctx));
        assert_eq!(integrity_for_level("unapproved", scope, &ctx), reader_integrity(scope, &ctx));
        assert_eq!(integrity_for_level("approved", scope, &ctx), writer_integrity(scope, &ctx));
        assert_eq!(integrity_for_level("merged", scope, &ctx), merged_integrity(scope, &ctx));
        // Unknown should default to none (safe)
        assert_eq!(integrity_for_level("unknown", scope, &ctx), none_integrity(scope, &ctx));
    }

    #[test]
    fn test_author_association_none_maps_to_unapproved() {
        // NONE means "no association with the repository" — NOT "brand new to GitHub".
        // It should map to unapproved (reader_integrity), same as FIRST_TIME_CONTRIBUTOR.
        // See https://docs.github.com/en/graphql/reference/enums#commentauthorassociation
        let ctx = test_ctx();
        let scope = "owner/repo";

        // NONE → unapproved (reader_integrity)
        assert_eq!(
            author_association_floor_from_str(scope, Some("NONE"), &ctx),
            reader_integrity(scope, &ctx),
            "NONE should map to unapproved (reader) integrity"
        );

        // FIRST_TIME_CONTRIBUTOR → unapproved (same as NONE)
        assert_eq!(
            author_association_floor_from_str(scope, Some("FIRST_TIME_CONTRIBUTOR"), &ctx),
            reader_integrity(scope, &ctx),
            "FIRST_TIME_CONTRIBUTOR should map to unapproved (reader) integrity"
        );

        // FIRST_TIMER → none (brand-new GitHub account)
        assert_eq!(
            author_association_floor_from_str(scope, Some("FIRST_TIMER"), &ctx),
            vec![] as Vec<String>,
            "FIRST_TIMER should map to none (empty) integrity"
        );

        // NONE and FIRST_TIME_CONTRIBUTOR produce the same integrity
        assert_eq!(
            author_association_floor_from_str(scope, Some("NONE"), &ctx),
            author_association_floor_from_str(scope, Some("FIRST_TIME_CONTRIBUTOR"), &ctx),
            "NONE and FIRST_TIME_CONTRIBUTOR should produce identical integrity"
        );

        // NONE and FIRST_TIMER produce different integrity
        assert_ne!(
            author_association_floor_from_str(scope, Some("NONE"), &ctx),
            author_association_floor_from_str(scope, Some("FIRST_TIMER"), &ctx),
            "NONE and FIRST_TIMER should produce different integrity levels"
        );
    }

    #[test]
    fn test_effective_disapproval_integrity_defaults_to_none() {
        let ctx = PolicyContext::default();
        assert_eq!(effective_disapproval_integrity(&ctx), "none");
    }

    #[test]
    fn test_effective_endorser_min_integrity_defaults_to_approved() {
        let ctx = PolicyContext::default();
        assert_eq!(effective_endorser_min_integrity(&ctx), "approved");
    }

    #[test]
    fn test_disapproval_overrides_endorsement_on_same_item() {
        // The core precedence rule: when the same item has both an endorsement
        // and a disapproval reaction from qualified maintainers, disapproval wins
        // because it runs last in the chain.
        let repo = "owner/repo";
        let ctx = PolicyContext {
            endorsement_reactions: vec!["THUMBS_UP".to_string()],
            disapproval_reactions: vec!["THUMBS_DOWN".to_string()],
            disapproval_integrity: "none".to_string(),
            ..Default::default()
        };
        let item = serde_json::json!({
            "number": 42,
            "reactions": {"nodes": [
                {"user": {"login": "alice"}, "content": "THUMBS_UP"},
                {"user": {"login": "bob"}, "content": "THUMBS_DOWN"}
            ]}
        });

        // Both endorsement and disapproval should match with admin callback
        assert!(has_maintainer_reaction_with_callback(
            &item, repo, &ctx.endorsement_reactions, "approved", &ctx,
            admin_permission_callback, "endorsement"
        ));
        assert!(has_maintainer_reaction_with_callback(
            &item, repo, &ctx.disapproval_reactions, "approved", &ctx,
            admin_permission_callback, "disapproval"
        ));

        // Simulate the integrity chain: start with none (external contributor),
        // apply endorsement (promotes to approved), then apply disapproval (caps to none).
        let base = none_integrity(repo, &ctx);
        let after_endorsement = max_integrity(repo, base, writer_integrity(repo, &ctx), &ctx);
        assert_eq!(after_endorsement, writer_integrity(repo, &ctx), "endorsement should promote to approved");

        let demote_cap = integrity_for_level("none", repo, &ctx);
        let after_disapproval = cap_integrity(repo, after_endorsement, demote_cap, &ctx);
        assert_eq!(after_disapproval, none_integrity(repo, &ctx), "disapproval should override endorsement back to none");
    }

    #[test]
    fn test_disapproval_reaction_with_admin_callback() {
        // Verify has_maintainer_reaction_with_callback works for disapproval reaction kind
        let ctx = PolicyContext {
            disapproval_reactions: vec!["THUMBS_DOWN".to_string()],
            ..Default::default()
        };
        let item = serde_json::json!({
            "number": 42,
            "reactions": {"nodes": [{"user": {"login": "alice"}, "content": "THUMBS_DOWN"}]}
        });
        assert!(has_maintainer_reaction_with_callback(
            &item, "owner/repo", &ctx.disapproval_reactions, "approved", &ctx,
            admin_permission_callback, "disapproval"
        ));
    }

    #[test]
    fn test_disapproval_reaction_below_threshold() {
        // Reactor with read permission should not count for disapproval
        let ctx = PolicyContext {
            disapproval_reactions: vec!["THUMBS_DOWN".to_string()],
            ..Default::default()
        };
        let item = serde_json::json!({
            "number": 42,
            "reactions": {"nodes": [{"user": {"login": "external"}, "content": "THUMBS_DOWN"}]}
        });
        assert!(!has_maintainer_reaction_with_callback(
            &item, "owner/repo", &ctx.disapproval_reactions, "approved", &ctx,
            read_permission_callback, "disapproval"
        ));
    }

    #[test]
    fn test_extract_items_array_bare_array() {
        let response = serde_json::json!([{"id": 1}, {"id": 2}]);
        let (items, path) = extract_items_array(&response);
        assert!(items.is_some());
        assert_eq!(items.unwrap().len(), 2);
        assert_eq!(path, "");
    }

    #[test]
    fn test_extract_items_array_items_wrapper() {
        let response = serde_json::json!({"items": [{"id": 1}], "total_count": 1});
        let (items, path) = extract_items_array(&response);
        assert!(items.is_some());
        assert_eq!(path, "/items");
    }

    #[test]
    fn test_extract_items_array_issues_wrapper() {
        let response = serde_json::json!({"issues": [{"number": 42}]});
        let (items, path) = extract_items_array(&response);
        assert!(items.is_some());
        assert_eq!(path, "/issues");
    }

    #[test]
    fn test_extract_items_array_pull_requests_wrapper() {
        let response = serde_json::json!({"pull_requests": [{"number": 7}]});
        let (items, path) = extract_items_array(&response);
        assert!(items.is_some());
        assert_eq!(path, "/pull_requests");
    }

    #[test]
    fn test_extract_items_array_unknown_shape_returns_none() {
        let response = serde_json::json!({"something_else": [{"id": 1}]});
        let (items, path) = extract_items_array(&response);
        assert!(items.is_none());
        assert_eq!(path, "");
    }
}
