package main

import (
	"fmt"
	"runtime/debug"
	"strings"

	"github.com/github/gh-aw-mcpg/internal/cmd"
	"github.com/github/gh-aw-mcpg/internal/logger"
)

var log = logger.New("main:main")

func main() {
	log.Print("Starting MCP Gateway application")

	// Build version string with metadata
	versionStr := buildVersionString()
	log.Printf("Built version string: %s", versionStr)

	// Set the version for the CLI
	cmd.SetVersion(versionStr)

	// Execute the root command
	log.Print("Executing root command")
	cmd.Execute()
}

const (
	shortHashLength = 7 // Length for short git commit hash
)

// buildVersionString constructs a detailed version string with build metadata
func buildVersionString() string {
	log.Print("Building version string from build metadata")
	var parts []string

	// Add main version
	if Version != "" {
		log.Printf("Using version from ldflags: %s", Version)
		parts = append(parts, Version)
	} else {
		log.Print("No version set, using 'dev'")
		parts = append(parts, "dev")
	}

	// Add git commit if available
	if GitCommit != "" {
		log.Printf("Using git commit from ldflags: %s", GitCommit)
		parts = append(parts, fmt.Sprintf("commit: %s", GitCommit))
	} else if buildInfo, ok := debug.ReadBuildInfo(); ok {
		log.Print("Extracting commit hash from build info")
		// Try to extract commit from build info if not set via ldflags
		for _, setting := range buildInfo.Settings {
			if setting.Key == "vcs.revision" {
				commitHash := setting.Value
				if len(commitHash) > shortHashLength {
					commitHash = commitHash[:shortHashLength] // Short hash
				}
				log.Printf("Found commit hash in build info: %s", commitHash)
				parts = append(parts, fmt.Sprintf("commit: %s", commitHash))
				break
			}
		}
	}

	// Add build date if available
	if BuildDate != "" {
		parts = append(parts, fmt.Sprintf("built: %s", BuildDate))
	} else if buildInfo, ok := debug.ReadBuildInfo(); ok {
		// Try to extract build time from build info if not set via ldflags
		for _, setting := range buildInfo.Settings {
			if setting.Key == "vcs.time" {
				parts = append(parts, fmt.Sprintf("built: %s", setting.Value))
				break
			}
		}
	}

	return strings.Join(parts, ", ")
}
