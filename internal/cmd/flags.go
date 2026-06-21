// Package cmd provides CLI flag registration using a modular pattern.
//
// To add a new flag without causing merge conflicts:
// 1. Create a new file (e.g., flags_myfeature.go)
// 2. Define your flag variable and default at the top
// 3. Create an init() function that calls RegisterFlag()
//
// Example (flags_myfeature.go):
//
//	package cmd
//
//	var myFeatureEnabled bool
//
//	func init() {
//		RegisterFlag(func(cmd *cobra.Command) {
//			cmd.Flags().BoolVar(&myFeatureEnabled, "my-feature", false, "Enable my feature")
//		})
//	}
//
// # Flag Defaults with Environment Variable Overrides
//
// Flags whose defaults can be overridden by an environment variable use inline
// envutil.GetEnv* calls directly in the RegisterFlag block:
//
//	cmd.Flags().StringVar(&myDir, "my-dir", envutil.GetEnvString("MY_DIR_ENV", config.DefaultMyDir), "...")
//
// This keeps the env-var name co-located with the flag declaration.
//
// Exception: difc.DefaultEnforcementMode() is kept as a named helper because
// it contains validation logic beyond a simple env lookup.
//
// When adding a new flag with an environment variable override:
//  1. Use envutil.GetEnv* directly in the RegisterFlag call.
//  2. Document the environment variable in AGENTS.md and README.md.
package cmd

import (
	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/github/gh-aw-mcpg/internal/difc"
	"github.com/spf13/cobra"
)

// FlagRegistrar is a function that registers flags on a command
type FlagRegistrar func(cmd *cobra.Command)

// flagRegistrars holds all flag registration functions
var flagRegistrars []FlagRegistrar

// RegisterFlag adds a flag registrar to be called during init
// This allows each feature to register its own flags without modifying root.go
func RegisterFlag(fn FlagRegistrar) {
	flagRegistrars = append(flagRegistrars, fn)
}

// registerAllFlags calls all registered flag registrars
func registerAllFlags(cmd *cobra.Command) {
	debugLog.Printf("Registering %d flag groups", len(flagRegistrars))
	for _, fn := range flagRegistrars {
		fn(cmd)
	}
	debugLog.Print("Flag group registration complete")
}

// applyFlagOrEnv sets *field to val when the named flag was explicitly set
// via the CLI, or when val differs from defaultVal (env-var override).
func applyFlagOrEnv[T comparable](cmd *cobra.Command, flagName string, field *T, val T, defaultVal T) {
	if cmd.Flags().Changed(flagName) || val != defaultVal {
		*field = val
	}
}

// registerFlagCompletions registers custom completion functions for flags
func registerFlagCompletions(cmd *cobra.Command) {
	debugLog.Print("Registering flag completion functions")
	// File and directory completions
	if err := cmd.MarkFlagFilename("config", "toml"); err != nil {
		debugLog.Printf("Failed to register --config filename completion: %v", err)
	}
	cmd.RegisterFlagCompletionFunc("log-dir", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return nil, cobra.ShellCompDirectiveFilterDirs
	})
	cmd.RegisterFlagCompletionFunc("payload-dir", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return nil, cobra.ShellCompDirectiveFilterDirs
	})
	cmd.RegisterFlagCompletionFunc("wasm-cache-dir", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return nil, cobra.ShellCompDirectiveFilterDirs
	})
	// Show all files for --env — the canonical .env file has no extension.
	if err := cmd.MarkFlagFilename("env"); err != nil {
		debugLog.Printf("Failed to register --env filename completion: %v", err)
	}

	// Enum completions for DIFC flags.
	// Note: the proxy subcommand registers its own guards-mode completion for its
	// separately-declared flag; keep both registrations in place.
	cmd.RegisterFlagCompletionFunc("guards-mode", cobra.FixedCompletions(
		difc.ValidModes, cobra.ShellCompDirectiveNoFileComp))
	cmd.RegisterFlagCompletionFunc("allowonly-min-integrity", cobra.FixedCompletions(
		config.AllIntegrityLevels(), cobra.ShellCompDirectiveNoFileComp))

	// Add ActiveHelp for --config and --config-stdin flags
	cmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		// Provide helpful tips when completing the command
		return cobra.AppendActiveHelp(nil,
				"Tip: Use --config <file> for file-based config or --config-stdin for piped JSON config"),
			cobra.ShellCompDirectiveNoFileComp
	}
}
