package version

import (
	"fmt"
	"runtime/debug"
	"strings"
)

const shortHashLength = 7

// vcsCommitFromBuildInfo extracts the vcs.revision setting from build info,
// truncating it to shortHashLength characters when longer.
// Returns an empty string when the setting is absent.
func vcsCommitFromBuildInfo(buildInfo *debug.BuildInfo) string {
	if buildInfo == nil {
		return ""
	}

	for _, setting := range buildInfo.Settings {
		if setting.Key == "vcs.revision" {
			commitHash := setting.Value
			if len(commitHash) > shortHashLength {
				commitHash = commitHash[:shortHashLength]
			}
			return commitHash
		}
	}
	return ""
}

// vcsTimeFromBuildInfo extracts the vcs.time setting from build info.
// Returns an empty string when the setting is absent.
func vcsTimeFromBuildInfo(buildInfo *debug.BuildInfo) string {
	if buildInfo == nil {
		return ""
	}

	for _, setting := range buildInfo.Settings {
		if setting.Key == "vcs.time" {
			return setting.Value
		}
	}
	return ""
}

// gatewayVersion stores the gateway version string, used across multiple packages
// for error reporting, health checks, and MCP client implementation info.
// It defaults to "0.0.0-dev" (a valid semantic version pre-release identifier) and
// should be set once at startup.
//
// Thread-safety note: This variable is written once at application startup
// (in SetVersion) before any concurrent access, and read-only thereafter.
// No mutex is needed as the write happens before any goroutines are spawned.
var gatewayVersion = "0.0.0-dev"

// Set updates the gateway version string if the provided version is non-empty.
// This should be called once at application startup from main.
func Set(v string) {
	if v != "" {
		gatewayVersion = v
	}
}

// Get returns the current gateway version string.
func Get() string {
	return gatewayVersion
}

// BuildVersionString constructs a detailed version string with optional build metadata.
func BuildVersionString(mainVersion, gitCommit, buildDate string) string {
	var parts []string

	if mainVersion != "" {
		parts = append(parts, mainVersion)
	} else {
		parts = append(parts, "dev")
	}

	if gitCommit != "" {
		parts = append(parts, fmt.Sprintf("commit: %s", gitCommit))
	} else if buildInfo, ok := debug.ReadBuildInfo(); ok {
		if commit := vcsCommitFromBuildInfo(buildInfo); commit != "" {
			parts = append(parts, fmt.Sprintf("commit: %s", commit))
		}
	}

	if buildDate != "" {
		parts = append(parts, fmt.Sprintf("built: %s", buildDate))
	} else if buildInfo, ok := debug.ReadBuildInfo(); ok {
		if date := vcsTimeFromBuildInfo(buildInfo); date != "" {
			parts = append(parts, fmt.Sprintf("built: %s", date))
		}
	}

	return strings.Join(parts, ", ")
}
