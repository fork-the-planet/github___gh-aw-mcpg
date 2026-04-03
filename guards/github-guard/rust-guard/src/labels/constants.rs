//! Label and configuration constants
//!
//! This module contains all constant values used throughout the labeling system.

/// Common label string constants to ensure consistency across the codebase
pub mod label_constants {
    pub const NONE: &str = "none";
    #[cfg(test)]
    pub const SECRET: &str = "secret";
    pub const PRIVATE_USER: &str = "private:user";
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

    pub const ORDER_HIGH_TO_LOW: [&str; 4] = [MERGED, APPROVED, UNAPPROVED, NONE];
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
