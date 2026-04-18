package main

import (
	"github.com/github/gh-aw-mcpg/internal/cmd"
	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/github/gh-aw-mcpg/internal/version"
)

var log = logger.New("main:main")

func main() {
	log.Print("Starting MCP Gateway application")

	// Build version string with metadata
	versionStr := version.BuildVersionString(Version, GitCommit, BuildDate)
	log.Printf("Built version string: %s", versionStr)

	// Set the version for the CLI
	cmd.SetVersion(versionStr)

	// Execute the root command
	log.Print("Executing root command")
	cmd.Execute()
}
