package main

// Build-time variables set during release builds
var (
	// Version is the semantic version of the binary (e.g., "v1.0.0")
	// Set via -ldflags "-X main.Version=<version>" during build
	Version = "0.0.0-dev"

	// GitCommit is the git commit hash at build time
	// Set via -ldflags "-X main.GitCommit=<commit>" during build
	GitCommit = ""

	// BuildDate is the date/time of the build (RFC3339 format)
	// Set via -ldflags "-X main.BuildDate=<date>" during build
	BuildDate = ""
)
