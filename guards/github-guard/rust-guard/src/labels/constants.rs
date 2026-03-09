//! Label and configuration constants
//!
//! This module contains all constant values used throughout the labeling system.

/// Common label string constants to ensure consistency across the codebase
pub mod label_constants {
    pub const NONE: &str = "none";
    pub const SECRET: &str = "secret";
    pub const PRIVATE_USER: &str = "private:user";
    #[allow(dead_code)]
    pub const WRITER_ORG: &str = "approved:org";
    #[allow(dead_code)]
    pub const READER_USER: &str = "unapproved:user";
    #[allow(dead_code)]
    pub const READER_PREFIX: &str = "unapproved:";
    #[allow(dead_code)]
    pub const WRITER_PREFIX: &str = "approved:";
    #[allow(dead_code)]
    pub const MERGED_PREFIX: &str = "merged:";
    pub const NONE_PREFIX: &str = "none:";
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
    #[allow(dead_code)]
    pub const NUMBER: &str = "number";
    #[allow(dead_code)]
    pub const PRIVATE: &str = "private";
    #[allow(dead_code)]
    pub const FULL_NAME: &str = "full_name";
    #[allow(dead_code)]
    pub const USER: &str = "user";
    #[allow(dead_code)]
    pub const LOGIN: &str = "login";
    pub const MERGED_AT: &str = "merged_at";
    pub const MERGED: &str = "merged";
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
#[allow(dead_code)]
pub const SMALL_BUFFER_SIZE: usize = 16 * 1024; // 16KB
pub const MEDIUM_BUFFER_SIZE: usize = 64 * 1024; // 64KB

/// Maximum items to process per response to prevent WASM memory exhaustion
pub const MAX_ITEMS_PER_RESPONSE: usize = 100;
