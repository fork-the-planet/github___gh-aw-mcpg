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
// # getDefault*() Flag Helper Pattern
//
// Each flag whose default value can be overridden by an environment variable has a
// corresponding getDefault*() helper function that follows this pattern:
//
//	func getDefaultXxx() T {
//	    return envutil.GetEnvT("MCP_GATEWAY_XXX", defaultXxx)
//	}
//
// Current helpers and their environment variables:
//
//	flags_logging.go  getDefaultLogDir()              → MCP_GATEWAY_LOG_DIR
//	flags_logging.go  getDefaultPayloadDir()          → MCP_GATEWAY_PAYLOAD_DIR
//	flags_logging.go  getDefaultPayloadPathPrefix()   → MCP_GATEWAY_PAYLOAD_PATH_PREFIX
//	flags_logging.go  getDefaultPayloadSizeThreshold() → MCP_GATEWAY_PAYLOAD_SIZE_THRESHOLD
//	flags_difc.go     getDefaultDIFCMode()             → MCP_GATEWAY_GUARDS_MODE
//	flags_difc.go     getDefaultDIFCSinkServerIDs()    → MCP_GATEWAY_GUARDS_SINK_SERVER_IDS
//
// This pattern is intentionally kept in individual feature files because:
//   - Each helper names the specific environment variable it reads, making the
//     coupling between flag and env var explicit and discoverable.
//   - The one-liner wrappers are trivial and unlikely to diverge.
//   - Go's type system (string/int/bool) prevents a single generic helper without
//     sacrificing readability.
//
// When adding a new flag with an environment variable override:
//  1. Add a defaultXxx constant and a getDefaultXxx() function in the feature file.
//  2. Add the new helper to the table above.
package cmd

import "github.com/spf13/cobra"

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
	for _, fn := range flagRegistrars {
		fn(cmd)
	}
}

// registerFlagCompletions registers custom completion functions for flags
func registerFlagCompletions(cmd *cobra.Command) {
	// Custom completion for --config flag (complete with .toml files)
	cmd.RegisterFlagCompletionFunc("config", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"toml"}, cobra.ShellCompDirectiveFilterFileExt
	})

	// Custom completion for --log-dir flag (complete with directories)
	cmd.RegisterFlagCompletionFunc("log-dir", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return nil, cobra.ShellCompDirectiveFilterDirs
	})

	// Custom completion for --payload-dir flag (complete with directories)
	cmd.RegisterFlagCompletionFunc("payload-dir", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return nil, cobra.ShellCompDirectiveFilterDirs
	})

	// Custom completion for --env flag (complete with .env files)
	cmd.RegisterFlagCompletionFunc("env", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"env"}, cobra.ShellCompDirectiveFilterFileExt
	})

	// Add ActiveHelp for --config and --config-stdin flags
	cmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		// Provide helpful tips when completing the command
		return cobra.AppendActiveHelp(nil,
				"Tip: Use --config <file> for file-based config or --config-stdin for piped JSON config"),
			cobra.ShellCompDirectiveNoFileComp
	}
}
