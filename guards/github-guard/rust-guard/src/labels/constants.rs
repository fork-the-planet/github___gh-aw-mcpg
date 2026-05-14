//! Label and configuration constants
//!
//! This module contains all constant values used throughout the labeling system.

/// Common label string constants to ensure consistency across the codebase
pub mod label_constants {
    pub const NONE: &str = "none";
    #[cfg(test)]
    pub const SECRET: &str = "secret";
    pub const PRIVATE_USER: &str = "private:user";
    pub const PRIVATE_BASE: &str = "private";
    pub const READER_PREFIX: &str = "unapproved:";
    pub const WRITER_PREFIX: &str = "approved:";
    pub const MERGED_PREFIX: &str = "merged:";
    pub const NONE_PREFIX: &str = "none:";
    pub const BLOCKED_PREFIX: &str = "blocked:";
    pub const BLOCKED_BASE: &str = "blocked";
    pub const READER_BASE: &str = "unapproved";
    pub const WRITER_BASE: &str = "approved";
    pub const MERGED_BASE: &str = "merged";
    pub const PRIVATE_PREFIX: &str = "private:";
}

/// Canonical policy-facing integrity level tokens.
pub mod policy_integrity {
    pub const NONE: &str = "none";
    pub const UNAPPROVED: &str = "unapproved";
    pub const APPROVED: &str = "approved";
    pub const MERGED: &str = "merged";

    #[cfg(test)]
    pub const ORDER_HIGH_TO_LOW: [&str; 4] = [MERGED, APPROVED, UNAPPROVED, NONE];
    /// Low-to-high order joined with `|`, ready for use in error messages.
    pub const ORDER_LOW_TO_HIGH_PIPED: &str = "none|unapproved|approved|merged";
}

#[cfg(test)]
mod tests {
    use super::policy_integrity;

    /// Ensures ORDER_LOW_TO_HIGH_PIPED stays in sync with ORDER_HIGH_TO_LOW.
    /// If a new integrity level is added or reordered, this test will catch the drift.
    #[test]
    fn order_low_to_high_piped_matches_order_high_to_low() {
        let derived: String = policy_integrity::ORDER_HIGH_TO_LOW
            .iter()
            .rev()
            .copied()
            .collect::<Vec<_>>()
            .join("|");
        assert_eq!(
            derived,
            policy_integrity::ORDER_LOW_TO_HIGH_PIPED,
            "ORDER_LOW_TO_HIGH_PIPED is out of sync with ORDER_HIGH_TO_LOW"
        );
    }
}

/// Canonical *reserved* scope token strings used for baseline and integrity scoping.
///
/// These are the three well-known, fixed scope tokens that represent broad resource
/// categories (org-level, user-level, and cross-repo). Other scopes exist at runtime
/// (e.g. `owner` or `owner/repo` for concrete repositories) — those are constructed
/// dynamically and are not represented here.
/// Using constants avoids silent typos (e.g. "Github") that produce wrong DIFC labels
/// with no compiler error.
pub mod scope_names {
    /// Owner-scoped policy (GitHub-org-level resources)
    pub const GITHUB: &str = "github";
    /// User-scoped policy (personal resources)
    pub const USER: &str = "user";
    /// Global-scoped policy (cross-repo / no specific owner)
    pub const GLOBAL: &str = "global";
}

/// Field name constants for JSON extraction
pub mod field_names {
    pub const OWNER: &str = "owner";
    pub const REPO: &str = "repo";
    pub const ISSUE_NUMBER: &str = "issue_number";
    pub const PULL_NUMBER: &str = "pull_number";
    pub const SHA: &str = "sha";
    pub const MERGED_AT: &str = "merged_at";
    pub const MERGED: &str = "merged";
    // Commonly accessed response fields
    pub const FULL_NAME: &str = "full_name";
    pub const NUMBER: &str = "number";
    pub const PRIVATE: &str = "private";
    pub const LOGIN: &str = "login";
}

/// Sensitive file patterns for detecting secret-containing files
pub const SENSITIVE_FILE_PATTERNS: &[&str] = &[
    ".env",
    ".key",
    ".pem",
    ".p12",
    ".pfx",
    "id_rsa",
    "id_dsa",
    "id_ecdsa",
    "id_ed25519",
];

/// Sensitive keywords in filenames
pub const SENSITIVE_FILE_KEYWORDS: &[&str] = &["secret", "credential", "password", "token"];

/// Buffer size constants for backend calls
pub const SMALL_BUFFER_SIZE: usize = 16 * 1024; // 16KB
pub const MEDIUM_BUFFER_SIZE: usize = 64 * 1024; // 64KB

/// Maximum items to process per response to prevent WASM memory exhaustion
pub const MAX_ITEMS_PER_RESPONSE: usize = 100;
