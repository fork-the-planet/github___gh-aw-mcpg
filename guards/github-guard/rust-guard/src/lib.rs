//! GitHub Guard for MCP Gateway - Rust Implementation
//!
//! This guard implements DIFC (Decentralized Information Flow Control) integrity
//! and secrecy labels for GitHub objects, following the github-difc specification.
//!
//! Build with:
//!   cargo build --target wasm32-wasip1 --release
//!
//! The compiled WASM will be at:
//!   target/wasm32-wasip1/release/github_guard.wasm

mod labels;
mod tools;

use labels::constants::policy_integrity;
use labels::constants::scope_names;
use labels::{
    blocked_integrity, extract_repo_info, extract_repo_info_from_search_query, MinIntegrity,
    PolicyContext, PolicyScopeEntry, ScopeKind,
};
use serde::{Deserialize, Serialize, Serializer};
use serde_json::Value;
use std::alloc::{alloc as std_alloc, dealloc as std_dealloc, Layout};
use std::borrow::Cow;
use std::ops::Deref;
use std::slice;
use std::sync::{Arc, Mutex};

const POLICY_SCOPE_ALL: &str = "all";
const POLICY_SCOPE_PUBLIC: &str = "public";
const DIFC_MODE: &str = "filter";

/// Maximum number of bytes to include in a log preview of serialized JSON.
const PREVIEW_MAX_BYTES: usize = 500;

/// Truncate a string to at most `max_bytes` bytes on a valid UTF-8 character
/// boundary. Returns the full string when it is shorter than the limit.
fn safe_preview(s: &str, max_bytes: usize) -> &str {
    if s.len() <= max_bytes {
        return s;
    }

    let mut end = max_bytes;
    while end > 0 && !s.is_char_boundary(end) {
        end -= 1;
    }
    &s[..end]
}

fn should_fallback_to_single_item_label(response: &Value) -> bool {
    !response.is_array()
}

/// Global policy context for WASM runtime entry points.
///
/// `label_agent` stores the parsed policy here; `label_resource` and
/// `label_response` read it. This is safe because WASM is single-threaded.
/// All internal functions take `&PolicyContext` explicitly so that tests
/// can construct their own context without touching this global.
static RUNTIME_POLICY_CONTEXT: Mutex<Option<PolicyContext>> = Mutex::new(None);

fn get_runtime_policy_context() -> PolicyContext {
    RUNTIME_POLICY_CONTEXT
        .lock()
        .ok()
        .and_then(|guard| guard.clone())
        .unwrap_or_default()
}

fn set_runtime_policy_context(ctx: PolicyContext) {
    if let Ok(mut guard) = RUNTIME_POLICY_CONTEXT.lock() {
        *guard = Some(ctx);
    }
}

// ============================================================================
// Host Imports
// ============================================================================

#[cfg(not(test))]
#[link(wasm_import_module = "env")]
extern "C" {
    /// Call the backend MCP server
    fn call_backend(
        tool_name_ptr: u32,
        tool_name_len: u32,
        args_ptr: u32,
        args_len: u32,
        result_ptr: u32,
        result_size: u32,
    ) -> i32;

    /// Log a message to the gateway
    /// Levels: 0=debug, 1=info, 2=warn, 3=error
    fn host_log(level: u32, msg_ptr: u32, msg_len: u32);
}

#[cfg(test)]
/// Test stub for call_backend - returns -1 since host backend is unavailable during testing
unsafe extern "C" fn call_backend(
    _tool_name_ptr: u32,
    _tool_name_len: u32,
    _args_ptr: u32,
    _args_len: u32,
    _result_ptr: u32,
    _result_size: u32,
) -> i32 {
    const TEST_STUB_ERROR: i32 = -1;
    TEST_STUB_ERROR // Backend unavailable in test environment
}

#[cfg(test)]
/// Test stub for host_log - no-op in tests
unsafe extern "C" fn host_log(_level: u32, _msg_ptr: u32, _msg_len: u32) {
    // No-op in tests
}

// ============================================================================
// Backend Callback Helper
// ============================================================================

/// Call a backend tool and return the result
/// This is a helper wrapper around call_backend with logging
pub fn invoke_backend(
    tool_name: &str,
    args_json: &str,
    result_buffer: &mut [u8],
) -> Result<usize, i32> {
    log_info(&format!(">>> call_backend: tool={}", tool_name));
    log_debug(&format!("    args={}", args_json));

    let tool_bytes = tool_name.as_bytes();
    let args_bytes = args_json.as_bytes();

    let result = unsafe {
        call_backend(
            tool_bytes.as_ptr() as u32,
            tool_bytes.len() as u32,
            args_bytes.as_ptr() as u32,
            args_bytes.len() as u32,
            result_buffer.as_mut_ptr() as u32,
            result_buffer.len() as u32,
        )
    };

    if result < 0 {
        if result == -2 {
            log_warn("<<< call_backend buffer too small; caller should retry with a larger buffer");
        } else {
            log_error(&format!("<<< call_backend FAILED with code {}", result));
        }
        Err(result)
    } else {
        log_info(&format!("<<< call_backend returned {} bytes", result));
        Ok(result as usize)
    }
}

// ============================================================================
// Logging Helpers
// ============================================================================

/// Log levels matching the gateway's expectations
#[repr(u32)]
enum LogLevel {
    Debug = 0,
    Info = 1,
    Warn = 2,
    Error = 3,
}

/// Send a log message to the gateway
fn log(level: LogLevel, msg: &str) {
    if msg.is_empty() {
        return;
    }
    let bytes = msg.as_bytes();
    unsafe {
        host_log(level as u32, bytes.as_ptr() as u32, bytes.len() as u32);
    }
}

fn log_debug(msg: &str) {
    log(LogLevel::Debug, msg);
}

fn log_info(msg: &str) {
    log(LogLevel::Info, msg);
}

fn log_warn(msg: &str) {
    log(LogLevel::Warn, msg);
}

fn log_error(msg: &str) {
    log(LogLevel::Error, msg);
}

/// Copy `bytes` into the WASM linear-memory output buffer.
///
/// # Safety
/// `output_ptr` must point to at least `bytes.len()` writable bytes in WASM
/// linear memory. The caller must have already verified `bytes.len() <= output_size`.
unsafe fn write_bytes_to_output(output_ptr: u32, bytes: &[u8]) {
    let dest = slice::from_raw_parts_mut(output_ptr as *mut u8, bytes.len());
    dest.copy_from_slice(bytes);
}

/// Write pre-serialized JSON to the output buffer.
/// Returns the number of bytes written on success, or -1 if the buffer is too small
/// or if the length cannot be safely represented in the WASM ABI types.
fn try_write_json_output(
    output_json: &str,
    output_ptr: u32,
    output_size: u32,
    fn_name: &str,
) -> i32 {
    let len = match u32::try_from(output_json.len()) {
        Ok(n) => n,
        Err(_) => {
            log_error(&format!(
                "    FAILED: output too large ({} bytes)",
                output_json.len()
            ));
            return -1;
        }
    };
    if len > output_size {
        log_error(&format!(
            "    FAILED: output buffer too small ({} > {})",
            len, output_size
        ));
        return -1;
    }
    unsafe { write_bytes_to_output(output_ptr, output_json.as_bytes()) };
    log_info(&format!("<<< {} returning {} bytes", fn_name, len));
    match i32::try_from(len) {
        Ok(n) => n,
        Err(_) => {
            log_error(&format!("    FAILED: byte count {} overflows i32", len));
            -1
        }
    }
}

// ============================================================================
// Input/Output Types
// ============================================================================

/// Input structure for label_resource
#[derive(Debug, Deserialize)]
struct LabelResourceInput {
    tool_name: String,
    #[serde(default)]
    tool_args: Value,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct SharedLabels(Arc<Vec<String>>);

impl Serialize for SharedLabels {
    fn serialize<S>(&self, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: Serializer,
    {
        self.0.serialize(serializer)
    }
}

impl From<Vec<String>> for SharedLabels {
    fn from(labels: Vec<String>) -> Self {
        Self(Arc::new(labels))
    }
}

impl Deref for SharedLabels {
    type Target = [String];

    fn deref(&self) -> &Self::Target {
        self.0.as_slice()
    }
}

impl PartialEq<Vec<String>> for SharedLabels {
    fn eq(&self, other: &Vec<String>) -> bool {
        self.0.as_ref() == other
    }
}

impl PartialEq<SharedLabels> for Vec<String> {
    fn eq(&self, other: &SharedLabels) -> bool {
        self == other.0.as_ref()
    }
}

/// Resource labels following DIFC spec
#[derive(Debug, Serialize, Clone, Default)]
pub struct ResourceLabels {
    pub description: String,
    pub secrecy: SharedLabels,
    pub integrity: SharedLabels,
}

/// Output structure for label_resource
#[derive(Debug, Serialize)]
struct LabelResourceOutput {
    resource: ResourceLabels,
    operation: &'static str,
}

/// Input structure for label_response
/// The gateway passes: {"tool_name": "...", "tool_result": <response data>}
#[derive(Debug, Deserialize)]
struct LabelResponseInput {
    tool_name: String,
    #[serde(default)]
    tool_args: Value,
    #[serde(default)]
    tool_result: Value, // Gateway uses "tool_result" not "response"
}

/// Path-based label for response items (RFC 6901 JSON Pointer)
/// This is the preferred format for collections - no data copying required
#[derive(Debug, Serialize)]
pub struct PathLabel {
    pub path: String,
    pub labels: ResourceLabels,
}

/// Output structure for path-based labeling (preferred for collections)
/// Gateway auto-detects this format via "labeled_paths" key
#[derive(Debug, Serialize)]
struct PathLabeledOutput {
    labeled_paths: Vec<PathLabel>,
    #[serde(skip_serializing_if = "Option::is_none")]
    default_labels: Option<ResourceLabels>,
    #[serde(skip_serializing_if = "Option::is_none")]
    items_path: Option<&'static str>,
}

/// Labeled item for legacy response format (used for singletons)
#[derive(Debug, Serialize)]
struct LabeledItem {
    data: Value,
    labels: ResourceLabels,
}

/// Output structure for label_response using legacy format
/// Used for singleton responses where copying is acceptable
#[derive(Debug, Serialize)]
struct LabelResponseOutput {
    items: Vec<LabeledItem>,
}

enum FallbackAction {
    ContinueProcessing,
    SkipLabeling,
}

/// Applies metadata/singleton fallback labeling when no fine-grained items exist.
/// Returns [`FallbackAction::SkipLabeling`] when the caller should return `0`
/// (top-level array passthrough), or [`FallbackAction::ContinueProcessing`]
/// when normal output generation should continue.
fn apply_singleton_fallback_if_needed(
    input: &LabelResponseInput,
    ctx: &PolicyContext,
    labeled_items: &mut Vec<LabeledItem>,
) -> FallbackAction {
    if !labeled_items.is_empty() {
        return FallbackAction::ContinueProcessing;
    }

    // Extract repo info from tool args (same logic as label_resource)
    let (_, _, repo_id) = extract_repo_info(&input.tool_args);
    let baseline_scope = infer_scope_for_baseline(&input.tool_name, &input.tool_args, &repo_id);

    // Server-generated metadata (pagination errors, empty search results) contains
    // no repository data — pass through with approved integrity so the agent can
    // see instructional messages and empty-result confirmations.
    let actual_response = labels::extract_mcp_response(&input.tool_result);
    let is_server_metadata = labels::is_mcp_text_wrapper(&actual_response)
        || (labels::is_search_result_wrapper(&actual_response)
            && labels::search_result_total_count(&actual_response) == Some(0));

    if is_server_metadata {
        let scope = if baseline_scope.is_empty() {
            scope_names::GITHUB
        } else {
            &baseline_scope
        };
        // Use writer_integrity which goes through normalize_scope to match
        // the policy scope token (e.g., "github" for owner-scoped policies).
        let integrity = labels::writer_integrity(scope, ctx);
        let desc = format!("metadata:{}", input.tool_name);

        log_info(&format!(
            "    server metadata (text message or empty search), integrity={:?}",
            integrity
        ));

        labeled_items.push(LabeledItem {
            data: input.tool_result.clone(),
            labels: ResourceLabels {
                description: desc,
                secrecy: vec![].into(),
                integrity: integrity.into(),
            },
        });
        return FallbackAction::ContinueProcessing;
    }

    if !should_fallback_to_single_item_label(&actual_response) {
        log_info("    no fine-grained items for top-level array response, skipping fallback label");
        return FallbackAction::SkipLabeling;
    }

    log_info("    no fine-grained items, creating fallback single-item label");

    // Use apply_tool_labels to get proper labels for this tool
    let desc = format!("resource:{}", input.tool_name);
    let (secrecy, integrity, final_desc) = labels::apply_tool_labels(
        &input.tool_name,
        &input.tool_args,
        &repo_id,
        vec![], // default secrecy
        vec![], // default integrity
        desc,
        ctx,
    );

    let integrity = labels::ensure_integrity_baseline(&baseline_scope, integrity, ctx);

    log_info(&format!(
        "    fallback labels: secrecy={:?}, integrity={:?}",
        secrecy, integrity
    ));

    labeled_items.push(LabeledItem {
        data: input.tool_result.clone(),
        labels: ResourceLabels {
            description: final_desc,
            secrecy: secrecy.into(),
            integrity: integrity.into(),
        },
    });

    FallbackAction::ContinueProcessing
}

fn infer_scope_for_baseline<'a>(
    tool_name: &str,
    tool_args: &Value,
    repo_id: &'a str,
) -> Cow<'a, str> {
    if !repo_id.is_empty() {
        return Cow::Borrowed(repo_id);
    }

    match tool_name {
        "dismiss_notification"
        | "mark_all_notifications_read"
        | "manage_notification_subscription"
        | "manage_repository_notification_subscription"
        | "create_repository"
        | "fork_repository" => Cow::Borrowed(scope_names::GITHUB),
        "search_code" | "search_issues" | "search_pull_requests" | "search_commits" => {
            let query = tool_args
                .get("query")
                .and_then(|v| v.as_str())
                .unwrap_or("");
            let (_, _, repo_from_query) = extract_repo_info_from_search_query(query);
            Cow::Owned(repo_from_query)
        }
        _ => Cow::Borrowed(""),
    }
}

#[derive(Debug, Deserialize)]
struct LabelAgentInput {
    #[serde(rename = "allow-only")]
    allow_only: AllowOnlyPolicy,
    #[serde(rename = "trusted-bots", default)]
    trusted_bots: Vec<String>,
}

#[derive(Debug, Deserialize)]
struct AllowOnlyPolicy {
    #[serde(rename = "repos")]
    scope: ReposValue,
    #[serde(rename = "min-integrity")]
    min_integrity: String,
    #[serde(rename = "blocked-users", default)]
    blocked_users: Vec<String>,
    #[serde(rename = "refusal-labels", default)]
    refusal_labels: Vec<String>,
    #[serde(rename = "approval-labels", default)]
    approval_labels: Vec<String>,
    #[serde(rename = "trusted-users", default)]
    trusted_users: Vec<String>,
    #[serde(rename = "endorsement-reactions", default)]
    endorsement_reactions: Vec<String>,
    #[serde(rename = "disapproval-reactions", default)]
    disapproval_reactions: Vec<String>,
    #[serde(rename = "disapproval-integrity", default)]
    disapproval_integrity: String,
    #[serde(rename = "endorser-min-integrity", default)]
    endorser_min_integrity: String,
    #[serde(rename = "promotion-label", default)]
    promotion_label: String,
    #[serde(rename = "demotion-label", default)]
    demotion_label: String,
}

#[derive(Debug, Deserialize)]
#[serde(untagged)]
enum ReposValue {
    Named(String),
    ScopedList(Vec<String>),
}

#[derive(Debug, Serialize)]
struct LabelAgentOutput {
    agent: AgentLabels,
    difc_mode: &'static str,
    normalized_policy: NormalizedPolicy,
}

#[derive(Debug, Serialize)]
struct AgentLabels {
    secrecy: Vec<String>,
    integrity: Vec<String>,
}

#[derive(Debug, Serialize)]
struct NormalizedPolicy {
    scope_kind: &'static str,
    #[serde(rename = "min-integrity")]
    min_integrity: &'static str,
}

// ============================================================================
// Exported Functions
// ============================================================================

fn parse_scoped_entry(entry: &str) -> Result<(ScopeKind, Option<String>, Option<String>), String> {
    if entry.trim().is_empty() {
        return Err("AllowOnly.repos scoped entry must be non-empty".to_string());
    }

    let (owner, repo_part) = entry
        .split_once('/')
        .ok_or_else(|| "AllowOnly.repos entries must include '/'".to_string())?;

    if owner.is_empty() {
        return Err("AllowOnly.repos owner segment must be non-empty".to_string());
    }

    if repo_part.contains('/') {
        return Err("AllowOnly.repos repo segment must not include '/'".to_string());
    }

    if repo_part == "*" {
        return Ok((ScopeKind::Owner, Some(owner.to_string()), None));
    }

    let star_count = repo_part.matches('*').count();
    if star_count > 1 || (star_count == 1 && !repo_part.ends_with('*')) {
        return Err(
            "AllowOnly.repos wildcard '*' must appear once and only at the end".to_string(),
        );
    }

    if repo_part.ends_with('*') {
        // Invariant: star_count == 1 (checked above) and repo_part != "*" (caught above),
        // so there is at least one non-'*' character before the trailing '*' — prefix is
        // always non-empty here.
        let prefix = repo_part.trim_end_matches('*');
        return Ok((
            ScopeKind::RepoPrefix,
            Some(owner.to_string()),
            Some(prefix.to_string()),
        ));
    }

    Ok((
        ScopeKind::Repo,
        Some(owner.to_string()),
        Some(repo_part.to_string()),
    ))
}

fn parse_scope(scope: ReposValue) -> Result<Vec<PolicyScopeEntry>, String> {
    let mut entries = vec![];

    match scope {
        ReposValue::Named(value) => {
            let scope_kind = if value == POLICY_SCOPE_ALL {
                ScopeKind::All
            } else if value == POLICY_SCOPE_PUBLIC {
                ScopeKind::Public
            } else {
                return Err("AllowOnly.repos string must be one of all|public".to_string());
            };

            entries.push(PolicyScopeEntry {
                scope_kind,
                scope_owner: None,
                scope_repo: None,
                scope_label: scope_string(scope_kind, None, None),
            });
        }
        ReposValue::ScopedList(scopes) => {
            if scopes.is_empty() {
                return Err("AllowOnly.repos array must contain at least one scope".to_string());
            }

            for scope_entry in scopes {
                let (scope_kind, scope_owner, scope_repo) = parse_scoped_entry(scope_entry.trim())?;
                let scope_label =
                    scope_string(scope_kind, scope_owner.as_deref(), scope_repo.as_deref());
                entries.push(PolicyScopeEntry {
                    scope_kind,
                    scope_owner,
                    scope_repo,
                    scope_label,
                });
            }
        }
    }

    Ok(entries)
}

fn parse_integrity(value: &str) -> Result<MinIntegrity, String> {
    MinIntegrity::from_policy_str(value).ok_or_else(|| {
        format!(
            "AllowOnly.min-integrity must be one of {}",
            policy_integrity::ORDER_LOW_TO_HIGH_PIPED
        )
    })
}

fn scope_string(scope_kind: ScopeKind, owner: Option<&str>, repo: Option<&str>) -> String {
    match scope_kind {
        ScopeKind::All => POLICY_SCOPE_ALL.to_string(),
        ScopeKind::Public => POLICY_SCOPE_PUBLIC.to_string(),
        ScopeKind::Owner => owner.unwrap_or("").to_string(),
        ScopeKind::Repo => match (owner, repo) {
            (Some(o), Some(r)) => format!("{}/{}", o, r),
            _ => String::new(),
        },
        ScopeKind::RepoPrefix => match (owner, repo) {
            (Some(o), Some(prefix)) => format!("{}/{}*", o, prefix),
            _ => String::new(),
        },
    }
}

fn normalized_scope_kind(scopes: &[PolicyScopeEntry]) -> &'static str {
    if scopes.len() == 1 {
        scopes[0].scope_kind.as_str()
    } else {
        "Composite"
    }
}

#[no_mangle]
pub extern "C" fn label_agent(
    input_ptr: u32,
    input_len: u32,
    output_ptr: u32,
    output_size: u32,
) -> i32 {
    log_info(">>> label_agent called");
    let input_bytes = unsafe { slice::from_raw_parts(input_ptr as *const u8, input_len as usize) };

    let input: LabelAgentInput = match serde_json::from_slice(input_bytes) {
        Ok(v) => v,
        Err(e) => {
            log_error(&format!("    FAILED to parse policy input: {}", e));
            return -1;
        }
    };

    let policy = input.allow_only;
    let trusted_bots = input.trusted_bots;

    let scopes = match parse_scope(policy.scope) {
        Ok(v) => v,
        Err(e) => {
            log_error(&format!("    FAILED policy scope validation: {}", e));
            return -1;
        }
    };

    let integrity_floor = match parse_integrity(&policy.min_integrity) {
        Ok(v) => v,
        Err(e) => {
            log_error(&format!("    FAILED policy integrity validation: {}", e));
            return -1;
        }
    };

    // Compute scope-derived values while `scopes` is still owned, before moving it into ctx.
    let secrecy: Vec<String> = scopes
        .iter()
        .filter_map(|scope| match scope.scope_kind {
            ScopeKind::All | ScopeKind::Public => None,
            ScopeKind::Owner | ScopeKind::Repo | ScopeKind::RepoPrefix => {
                if scope.scope_label.is_empty() {
                    None
                } else {
                    Some(format!("private:{}", scope.scope_label))
                }
            }
        })
        .collect();

    let scope_kind_str = normalized_scope_kind(&scopes);

    let ctx = PolicyContext {
        scopes,
        trusted_bots,
        blocked_users: policy.blocked_users,
        refusal_labels: policy.refusal_labels,
        approval_labels: policy.approval_labels,
        trusted_users: policy.trusted_users,
        endorsement_reactions: policy.endorsement_reactions,
        disapproval_reactions: policy.disapproval_reactions,
        disapproval_integrity: policy.disapproval_integrity,
        endorser_min_integrity: policy.endorser_min_integrity,
        promotion_label: policy.promotion_label,
        demotion_label: policy.demotion_label,
    };

    // Compute integrity before moving ctx into the global — borrows ctx, no clone needed.
    let token = labels::helpers::policy_scope_token(&ctx.scopes);
    let integrity = integrity_floor.build_labels(&token, &ctx);
    set_runtime_policy_context(ctx);

    let normalized_policy = NormalizedPolicy {
        scope_kind: scope_kind_str,
        min_integrity: integrity_floor.as_str(),
    };

    let output = LabelAgentOutput {
        agent: AgentLabels { secrecy, integrity },
        difc_mode: DIFC_MODE,
        normalized_policy,
    };

    let output_json = match serde_json::to_string(&output) {
        Ok(s) => s,
        Err(e) => {
            log_error(&format!(
                "    FAILED to serialize label_agent output: {}",
                e
            ));
            return -1;
        }
    };

    try_write_json_output(&output_json, output_ptr, output_size, "label_agent")
}

/// label_resource is called by the gateway to label a resource before access
#[no_mangle]
pub extern "C" fn label_resource(
    input_ptr: u32,
    input_len: u32,
    output_ptr: u32,
    output_size: u32,
) -> i32 {
    log_info(">>> label_resource called");

    // Read input bytes
    let input_bytes = unsafe { slice::from_raw_parts(input_ptr as *const u8, input_len as usize) };
    log_info(&format!(
        "    input_len={}, output_size={}",
        input_len, output_size
    ));

    // Parse input JSON
    let input: LabelResourceInput = match serde_json::from_slice(input_bytes) {
        Ok(v) => v,
        Err(e) => {
            log_error(&format!("    FAILED to parse input: {}", e));
            return -1;
        }
    };

    log_info(&format!("    tool_name={}", input.tool_name));

    // Extract owner/repo for scoped tags
    let (_, _, repo_id) = extract_repo_info(&input.tool_args);
    let ctx = get_runtime_policy_context();

    // Build initial labels
    let desc = format!("resource:{}", input.tool_name);
    let secrecy: Vec<String> = vec![];
    let mut integrity: Vec<String> = vec![];
    let mut operation = "read";

    // Classify operation with scoped integrity tags
    if tools::is_write_operation(&input.tool_name) {
        operation = "write";
        if !repo_id.is_empty() {
            integrity = if tools::is_merge_operation(&input.tool_name)
                || tools::is_delete_operation(&input.tool_name)
            {
                labels::writer_integrity(&repo_id, &ctx)
            } else {
                labels::reader_integrity(&repo_id, &ctx)
            };
        }
    }

    if tools::is_read_write_operation(&input.tool_name) {
        operation = "read-write";
        // Writer-level baseline
        if !repo_id.is_empty() {
            integrity = labels::writer_integrity(&repo_id, &ctx);
        }
    }

    // Apply tool-specific labels
    let (final_secrecy, final_integrity, final_desc) = labels::apply_tool_labels(
        &input.tool_name,
        &input.tool_args,
        &repo_id,
        secrecy,
        integrity,
        desc,
        &ctx,
    );

    let baseline_scope = infer_scope_for_baseline(&input.tool_name, &input.tool_args, &repo_id);
    let final_integrity = labels::ensure_integrity_baseline(&baseline_scope, final_integrity, &ctx);

    // Unconditionally blocked tools: override integrity to blocked_integrity so the
    // DIFC evaluator always denies them.  This must happen after ensure_integrity_baseline
    // because that helper would otherwise raise blocked-level tags to none-level.
    let final_integrity = if tools::is_blocked_tool(&input.tool_name) {
        log_info(&format!(
            "    tool '{}' is unconditionally blocked — overriding integrity to blocked",
            input.tool_name
        ));
        let scope = if repo_id.is_empty() {
            scope_names::GLOBAL
        } else {
            &repo_id
        };
        blocked_integrity(scope, &ctx)
    } else {
        final_integrity
    };

    // Log computed labels
    log_info(&format!("    desc={}", final_desc));
    if final_secrecy.is_empty() {
        log_info("    secrecy=[] (public)");
    } else {
        log_info(&format!("    secrecy={:?}", final_secrecy));
    }
    if final_integrity.is_empty() {
        log_info("    integrity=[] (untrusted)");
    } else {
        log_info(&format!("    integrity={:?}", final_integrity));
    }
    log_info(&format!("    operation={}", operation));

    // Build output
    let output = LabelResourceOutput {
        resource: ResourceLabels {
            description: final_desc,
            secrecy: final_secrecy.into(),
            integrity: final_integrity.into(),
        },
        operation,
    };

    // Serialize output
    let output_json = match serde_json::to_string(&output) {
        Ok(s) => s,
        Err(e) => {
            log_error(&format!("    FAILED to serialize output: {}", e));
            return -1;
        }
    };

    try_write_json_output(&output_json, output_ptr, output_size, "label_resource")
}

/// label_response is called by the gateway to label response data
/// This can implement fine-grained, per-item labeling for collection responses
/// Uses path-based labeling (preferred) for collections, legacy format for singletons
#[no_mangle]
pub extern "C" fn label_response(
    input_ptr: u32,
    input_len: u32,
    output_ptr: u32,
    output_size: u32,
) -> i32 {
    log_info(">>> label_response called");
    log_info(&format!(
        "    input_len={}, output_size={}",
        input_len, output_size
    ));

    // Read input bytes
    let input_bytes = unsafe { slice::from_raw_parts(input_ptr as *const u8, input_len as usize) };

    // Log a bounded preview of the input for debugging.
    // Only decode up to PREVIEW_MAX_BYTES so logging stays cheap, and
    // if the prefix ends mid-codepoint, fall back to the valid UTF-8 prefix.
    let preview_bytes = &input_bytes[..input_bytes.len().min(PREVIEW_MAX_BYTES)];
    let preview = match std::str::from_utf8(preview_bytes) {
        Ok(s) => s,
        Err(e) => {
            let valid_up_to = e.valid_up_to();
            if valid_up_to == 0 {
                ""
            } else {
                std::str::from_utf8(&preview_bytes[..valid_up_to]).unwrap_or("")
            }
        }
    };
    if !preview.is_empty() {
        log_info(&format!("    input_preview={}", preview));
    }

    // Parse input JSON
    let input: LabelResponseInput = match serde_json::from_slice(input_bytes) {
        Ok(v) => v,
        Err(e) => {
            log_error(&format!("    FAILED to parse input: {}", e));
            log_info("<<< label_response returning 0 (parse error)");
            return 0; // Return 0 to skip fine-grained labeling
        }
    };

    log_info(&format!("    tool_name={}", input.tool_name));

    let ctx = get_runtime_policy_context();

    // First, try path-based labeling (preferred for collections - no data copying)
    if let Some(path_result) =
        labels::label_response_paths(&input.tool_name, &input.tool_args, &input.tool_result, &ctx)
    {
        log_info(&format!(
            "    using path-based labeling with {} paths",
            path_result.labeled_paths.len()
        ));

        let output = PathLabeledOutput {
            labeled_paths: path_result.labeled_paths,
            default_labels: path_result.default_labels,
            items_path: path_result.items_path,
        };

        // Serialize and return
        let output_json = match serde_json::to_string(&output) {
            Ok(s) => s,
            Err(e) => {
                log_error(&format!("    FAILED to serialize path output: {}", e));
                log_info("<<< label_response returning 0 (serialize error)");
                return 0;
            }
        };

        // Log output preview for debugging
        let output_preview = safe_preview(&output_json, PREVIEW_MAX_BYTES);
        log_info(&format!("    path_output_preview={}", output_preview));

        let n = try_write_json_output(&output_json, output_ptr, output_size, "label_response/path");
        if n < 0 {
            log_info("<<< label_response returning 0 (output write failed)");
            return 0;
        }
        return n;
    }

    // Fall back to legacy item-based labeling for singletons
    log_info("    using legacy item-based labeling");

    // Apply response-specific labeling (gateway passes response as "tool_result")
    let mut labeled_items =
        labels::label_response_items(&input.tool_name, &input.tool_args, &input.tool_result, &ctx);

    // If no items were generated, wrap entire response as single item with computed labels
    // when appropriate. This ensures single-item responses (like get_file_contents)
    // are properly labeled while preserving unlabeled top-level array passthrough.
    if matches!(
        apply_singleton_fallback_if_needed(&input, &ctx, &mut labeled_items),
        FallbackAction::SkipLabeling
    ) {
        log_info("<<< label_response returning 0 (top-level array passthrough)");
        return 0;
    }

    log_info(&format!(
        "    generated {} labeled items",
        labeled_items.len()
    ));

    // Build output - gateway expects legacy format {"items": [...]}
    let output = LabelResponseOutput {
        items: labeled_items,
    };

    // Serialize output
    let output_json = match serde_json::to_string(&output) {
        Ok(s) => s,
        Err(e) => {
            log_error(&format!("    FAILED to serialize output: {}", e));
            log_info("<<< label_response returning 0 (serialize error)");
            return 0;
        }
    };

    // Log output preview for debugging
    let output_preview = safe_preview(&output_json, PREVIEW_MAX_BYTES);
    log_info(&format!("    output_preview={}", output_preview));

    let n = try_write_json_output(&output_json, output_ptr, output_size, "label_response/legacy");
    if n < 0 { 0 } else { n }
}

// ============================================================================
// Memory Management
// ============================================================================

/// Allocate memory for the host to write into
#[no_mangle]
pub extern "C" fn alloc(size: u32) -> u32 {
    log_debug(&format!(">>> alloc({})", size));
    let layout = match Layout::from_size_align(size as usize, 8) {
        Ok(l) => l,
        Err(_) => {
            log_error(&format!(
                "    alloc FAILED: invalid layout for size {}",
                size
            ));
            return 0;
        }
    };
    let ptr = unsafe { std_alloc(layout) as u32 };
    log_debug(&format!("<<< alloc returning ptr={}", ptr));
    ptr
}

/// Deallocate memory previously allocated with alloc
#[no_mangle]
pub extern "C" fn dealloc(ptr: u32, size: u32) {
    log_debug(&format!(">>> dealloc(ptr={}, size={})", ptr, size));
    if ptr == 0 || size == 0 {
        log_debug("    dealloc skipped (null ptr or zero size)");
        return;
    }
    let layout = match Layout::from_size_align(size as usize, 8) {
        Ok(l) => l,
        Err(_) => {
            log_error(&format!(
                "    dealloc FAILED: invalid layout for size {}",
                size
            ));
            return;
        }
    };
    unsafe { std_dealloc(ptr as *mut u8, layout) }
    log_debug("<<< dealloc complete");
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn top_level_array_responses_skip_single_item_fallback() {
        assert!(!should_fallback_to_single_item_label(&json!([{"id": 1}])));
    }

    #[test]
    fn singleton_object_responses_use_single_item_fallback() {
        assert!(should_fallback_to_single_item_label(&json!({"id": 1})));
    }

    #[test]
    fn label_response_control_flow_skips_fallback_for_unlabeled_top_level_array() {
        let input = LabelResponseInput {
            tool_name: "issue_read".to_string(),
            tool_args: json!({
                "owner": "org",
                "repo": "repo",
                "issue_number": "7",
                "method": "get_comments"
            }),
            tool_result: json!([
                {"id": 1, "body": "first"},
                {"id": 2, "body": "second"}
            ]),
        };

        let mut labeled_items = Vec::new();
        let action = apply_singleton_fallback_if_needed(
            &input,
            &PolicyContext::default(),
            &mut labeled_items,
        );

        assert!(matches!(action, FallbackAction::SkipLabeling));
        assert!(labeled_items.is_empty());
    }

    #[test]
    fn parse_scope_accepts_owner_wildcard_array_entry() {
        let parsed = parse_scope(ReposValue::ScopedList(vec!["octocat/*".to_string()]))
            .expect("owner wildcard should parse");

        assert_eq!(parsed[0].scope_kind, ScopeKind::Owner);
        assert_eq!(parsed[0].scope_owner.as_deref(), Some("octocat"));
        assert_eq!(parsed[0].scope_repo, None);
    }

    #[test]
    fn parse_scope_rejects_scoped_array_with_multiple_entries() {
        let parsed = parse_scope(ReposValue::ScopedList(vec![
            "octocat/*".to_string(),
            "octocat/hello-world".to_string(),
        ]))
        .expect("multiple scoped entries should be accepted");

        assert_eq!(parsed.len(), 2);
    }

    #[test]
    fn parse_scope_accepts_repo_prefix_wildcard_array_entry() {
        let parsed = parse_scope(ReposValue::ScopedList(vec!["octocat/hello*".to_string()]))
            .expect("repo prefix wildcard should parse");

        assert_eq!(parsed[0].scope_kind, ScopeKind::RepoPrefix);
        assert_eq!(parsed[0].scope_owner.as_deref(), Some("octocat"));
        assert_eq!(parsed[0].scope_repo.as_deref(), Some("hello"));
    }

    #[test]
    fn parse_scope_named_public_sets_scope_label() {
        let parsed = parse_scope(ReposValue::Named("public".to_string()))
            .expect("public named scope should parse");

        assert_eq!(parsed.len(), 1);
        assert_eq!(parsed[0].scope_kind, ScopeKind::Public);
        assert_eq!(parsed[0].scope_label, "public");
    }

    #[test]
    fn parse_scope_named_all_sets_scope_label() {
        let parsed = parse_scope(ReposValue::Named("all".to_string()))
            .expect("all named scope should parse");

        assert_eq!(parsed.len(), 1);
        assert_eq!(parsed[0].scope_kind, ScopeKind::All);
        assert_eq!(parsed[0].scope_label, "all");
    }

    #[test]
    fn parse_scope_rejects_repo_segment_with_extra_slash() {
        let err = parse_scope(ReposValue::ScopedList(vec![
            "octocat/hello/world".to_string()
        ]))
        .expect_err("repo segment with slash must be rejected");

        assert!(err.contains("must not include '/'"));
    }

    #[test]
    fn infer_scope_for_baseline_uses_search_code_query_repo() {
        let tool_args = json!({"query": "repo:lpcox/github-guard README"});

        let inferred = infer_scope_for_baseline("search_code", &tool_args, "");
        assert!(matches!(&inferred, Cow::Owned(_)));
        assert_eq!(inferred, "lpcox/github-guard");
    }

    #[test]
    fn infer_scope_for_baseline_borrows_repo_id_when_present() {
        let tool_args = json!({});
        let inferred =
            infer_scope_for_baseline("get_file_contents", &tool_args, "octocat/hello-world");

        assert!(matches!(inferred, Cow::Borrowed("octocat/hello-world")));
    }

    #[test]
    fn infer_scope_for_baseline_borrows_github_scope_for_repo_creation() {
        let tool_args = json!({});
        let inferred = infer_scope_for_baseline("create_repository", &tool_args, "");

        assert!(matches!(inferred, Cow::Borrowed(scope_names::GITHUB)));
    }

    #[test]
    fn infer_scope_for_baseline_borrows_empty_scope_for_other_tools() {
        let tool_args = json!({});
        let inferred = infer_scope_for_baseline("get_file_contents", &tool_args, "");

        assert!(matches!(inferred, Cow::Borrowed("")));
    }

    #[test]
    fn search_code_baseline_preserves_scoped_integrity() {
        let ctx = PolicyContext {
            scopes: vec![PolicyScopeEntry {
                scope_kind: ScopeKind::RepoPrefix,
                scope_owner: Some("lpcox".to_string()),
                scope_repo: Some("git".to_string()),
                scope_label: "lpcox/git*".to_string(),
            }],
            ..Default::default()
        };

        let tool_args = json!({"query": "repo:lpcox/github-guard README"});
        let (_, integrity, _) = labels::apply_tool_labels(
            "search_code",
            &tool_args,
            "",
            vec![],
            vec![],
            String::new(),
            &ctx,
        );

        let inferred_scope = infer_scope_for_baseline("search_code", &tool_args, "");
        let baseline = labels::ensure_integrity_baseline(&inferred_scope, integrity, &ctx);

        assert_eq!(
            baseline,
            vec![
                "none:lpcox/git*".to_string(),
                "unapproved:lpcox/git*".to_string(),
                "approved:lpcox/git*".to_string(),
            ]
        );
    }

    #[test]
    fn infer_scope_for_baseline_uses_search_issues_query_repo() {
        let tool_args = json!({"query": "repo:github/gh-aw-mcpg is:open bug"});
        let inferred = infer_scope_for_baseline("search_issues", &tool_args, "");
        assert_eq!(inferred, "github/gh-aw-mcpg");
    }

    #[test]
    fn infer_scope_for_baseline_uses_search_pull_requests_query_repo() {
        let tool_args = json!({"query": "repo:github/gh-aw-mcpg is:pr is:open"});
        let inferred = infer_scope_for_baseline("search_pull_requests", &tool_args, "");
        assert_eq!(inferred, "github/gh-aw-mcpg");
    }

    #[test]
    fn infer_scope_for_baseline_uses_search_commits_query_repo() {
        let tool_args = json!({"query": "repo:github/gh-aw-mcpg fix"});
        let inferred = infer_scope_for_baseline("search_commits", &tool_args, "");
        assert_eq!(inferred, "github/gh-aw-mcpg");
    }

    #[test]
    fn infer_scope_for_baseline_uses_github_scope_for_notification_management_tools() {
        let tool_args = json!({ "threadId": "123" });
        for tool in &[
            "dismiss_notification",
            "mark_all_notifications_read",
            "manage_notification_subscription",
            "manage_repository_notification_subscription",
        ] {
            let inferred = infer_scope_for_baseline(tool, &tool_args, "");
            assert_eq!(
                inferred,
                scope_names::GITHUB,
                "{} should infer github baseline scope",
                tool
            );
        }
    }

    #[test]
    fn infer_scope_for_baseline_uses_github_scope_for_repo_creation_tools() {
        let tool_args = json!({ "name": "new-repo" });
        for tool in &["create_repository", "fork_repository"] {
            let inferred = infer_scope_for_baseline(tool, &tool_args, "");
            assert_eq!(
                inferred,
                scope_names::GITHUB,
                "{} should infer github baseline scope",
                tool
            );
        }
    }

    #[test]
    fn notification_management_integrity_preserved_after_baseline() {
        let ctx = PolicyContext::default();
        let tool_args = json!({ "threadId": "123" });
        for tool in &[
            "dismiss_notification",
            "mark_all_notifications_read",
            "manage_notification_subscription",
            "manage_repository_notification_subscription",
        ] {
            let (_, integrity, _) = labels::apply_tool_labels(
                tool,
                &tool_args,
                "",
                vec![],
                vec![],
                String::new(),
                &ctx,
            );
            let baseline_scope = infer_scope_for_baseline(tool, &tool_args, "");
            let after_baseline =
                labels::ensure_integrity_baseline(&baseline_scope, integrity, &ctx);

            assert_eq!(
                after_baseline,
                labels::project_github_label(&ctx),
                "{} integrity should remain github-scoped after baseline enforcement",
                tool
            );
        }
    }

    #[test]
    fn repo_creation_integrity_preserved_after_baseline() {
        let ctx = PolicyContext::default();
        let tool_args = json!({ "name": "new-repo" });
        for tool in &["create_repository", "fork_repository"] {
            let (_, integrity, _) = labels::apply_tool_labels(
                tool,
                &tool_args,
                "",
                vec![],
                vec![],
                String::new(),
                &ctx,
            );
            let baseline_scope = infer_scope_for_baseline(tool, &tool_args, "");
            let after_baseline =
                labels::ensure_integrity_baseline(&baseline_scope, integrity, &ctx);

            assert_eq!(
                after_baseline,
                labels::writer_integrity(scope_names::GITHUB, &ctx),
                "{} integrity should remain github writer-scoped after baseline enforcement",
                tool
            );
        }
    }

    #[test]
    fn transfer_repository_integrity_is_blocked_after_ensure_baseline() {
        // Verify that the is_blocked_tool + blocked_integrity override logic produces
        // a "blocked:" tag, proving that ensure_integrity_baseline cannot raise it
        // back to "none:".
        let ctx = PolicyContext::default();
        let repo_id = "github/copilot";

        let tool_args = json!({
            "owner": "github",
            "repo": "copilot",
            "new_owner": "other-org"
        });

        let (_, integrity, _) = labels::apply_tool_labels(
            "transfer_repository",
            &tool_args,
            repo_id,
            vec![],
            vec![],
            String::new(),
            &ctx,
        );
        let baseline_scope = infer_scope_for_baseline("transfer_repository", &tool_args, repo_id);
        let after_baseline = labels::ensure_integrity_baseline(&baseline_scope, integrity, &ctx);

        // Simulate the is_blocked_tool override performed in label_resource
        let final_integrity = if tools::is_blocked_tool("transfer_repository") {
            let scope = if repo_id.is_empty() {
                scope_names::GLOBAL
            } else {
                repo_id
            };
            blocked_integrity(scope, &ctx)
        } else {
            after_baseline
        };

        assert!(
            final_integrity.iter().any(|t| t.contains("blocked")),
            "transfer_repository must have blocked integrity after label_resource override; \
             got: {:?}",
            final_integrity
        );
    }

    // === UTF-8 safe preview tests (issue #3711) ===

    #[test]
    fn test_safe_preview_ascii_under_limit() {
        let s = "hello";
        assert_eq!(safe_preview(s, 500), "hello");
    }

    #[test]
    fn test_safe_preview_ascii_at_limit() {
        let s = "a".repeat(500);
        assert_eq!(safe_preview(&s, 500), s.as_str());
    }

    #[test]
    fn test_safe_preview_ascii_over_limit() {
        let s = "a".repeat(600);
        assert_eq!(safe_preview(&s, 500).len(), 500);
    }

    #[test]
    fn test_safe_preview_cjk_boundary() {
        // Each CJK character is 3 bytes in UTF-8. Build a string where byte 500
        // falls in the middle of a character (500 is not divisible by 3).
        // 166 chars = 498 bytes, 167 chars = 501 bytes.
        let cjk = "中".repeat(167); // 501 bytes
        assert_eq!(cjk.len(), 501);

        let preview = safe_preview(&cjk, 500);
        // Must truncate to 498 bytes (166 chars) — the last valid boundary before 500.
        assert_eq!(preview.len(), 498);
        assert_eq!(preview.chars().count(), 166);
    }

    #[test]
    fn test_safe_preview_emoji_boundary() {
        // 🎉 is 4 bytes in UTF-8. 125 emojis = 500 bytes exactly (boundary safe).
        // 126 emojis = 504 bytes; truncating at 500 would split the 126th emoji.
        let emoji = "🎉".repeat(126); // 504 bytes
        assert_eq!(emoji.len(), 504);

        let preview = safe_preview(&emoji, 500);
        // Must truncate to 500 bytes (125 complete emojis).
        assert_eq!(preview.len(), 500);
        assert_eq!(preview.chars().count(), 125);
    }

    #[test]
    fn test_safe_preview_mixed_content_near_boundary() {
        // Simulate a JSON string with ASCII keys and a CJK value crossing byte 500.
        // {"body":"<padding>中中中..."}
        let prefix = "{\"body\":\""; // 9 bytes
        let padding = "x".repeat(489); // 489 bytes — total so far: 498
        let cjk_tail = "中中中中中"; // 5 × 3 = 15 bytes — subtotal: 513

        let json = format!("{}{}{}\"}}", prefix, padding, cjk_tail); // +3 bytes for "\"}}" => 516 total
        assert!(json.len() > 500);

        let preview = safe_preview(&json, 500);
        // Byte 498 is the start of the first CJK char (498..501). Byte 500 is
        // mid-character, so floor_char_boundary(500) should give 498.
        assert_eq!(preview.len(), 498);
        // Verify it's valid UTF-8 (implicit — it's a &str).
        assert!(preview.ends_with('x'));
    }

    #[test]
    fn test_safe_preview_empty_string() {
        assert_eq!(safe_preview("", 500), "");
    }

    #[test]
    fn test_safe_preview_two_byte_chars() {
        // é is 2 bytes in UTF-8. 250 chars = 500 bytes (exact boundary).
        // 251 chars = 502 bytes; byte 500 is the first byte of the 251st char.
        let accented = "é".repeat(251); // 502 bytes
        assert_eq!(accented.len(), 502);

        let preview = safe_preview(&accented, 500);
        assert_eq!(preview.len(), 500);
        assert_eq!(preview.chars().count(), 250);
    }

    #[test]
    fn parse_integrity_accepts_all_valid_values() {
        assert_eq!(parse_integrity("none"), Ok(MinIntegrity::None));
        assert_eq!(parse_integrity("unapproved"), Ok(MinIntegrity::Unapproved));
        assert_eq!(parse_integrity("approved"), Ok(MinIntegrity::Approved));
        assert_eq!(parse_integrity("merged"), Ok(MinIntegrity::Merged));
    }

    #[test]
    fn parse_integrity_accepts_mixed_case() {
        assert_eq!(parse_integrity("None"), Ok(MinIntegrity::None));
        assert_eq!(parse_integrity("APPROVED"), Ok(MinIntegrity::Approved));
        assert_eq!(parse_integrity("  merged  "), Ok(MinIntegrity::Merged));
    }

    #[test]
    fn parse_integrity_rejects_unknown_value() {
        let err = parse_integrity("superuser").expect_err("unknown value must be rejected");
        assert!(
            err.contains("must be one of"),
            "error should describe constraint: {err}"
        );
        assert!(
            err.contains(policy_integrity::ORDER_LOW_TO_HIGH_PIPED),
            "error should contain the full valid-options string \"{}\": {err}",
            policy_integrity::ORDER_LOW_TO_HIGH_PIPED
        );
    }

    #[test]
    fn scope_string_all_arms() {
        assert_eq!(scope_string(ScopeKind::All, None, None), POLICY_SCOPE_ALL);
        assert_eq!(
            scope_string(ScopeKind::Public, None, None),
            POLICY_SCOPE_PUBLIC
        );
        assert_eq!(
            scope_string(ScopeKind::Owner, Some("octocat"), None),
            "octocat"
        );
        assert_eq!(scope_string(ScopeKind::Owner, None, None), "");
        assert_eq!(scope_string(ScopeKind::Repo, Some("o"), Some("r")), "o/r");
        assert_eq!(scope_string(ScopeKind::Repo, None, None), "");
        assert_eq!(
            scope_string(ScopeKind::RepoPrefix, Some("o"), Some("pfx")),
            "o/pfx*"
        );
        assert_eq!(scope_string(ScopeKind::RepoPrefix, None, None), "");
    }
}
