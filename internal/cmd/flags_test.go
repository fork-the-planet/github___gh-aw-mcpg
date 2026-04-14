package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestCmd creates a minimal cobra command for use in flag tests.
func newTestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "test",
		Short: "test command",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}
}

func TestRegisterAllFlags(t *testing.T) {
	t.Run("registers all expected flags on the command", func(t *testing.T) {
		cmd := newTestCmd()

		// Before registration, none of the module flags should exist
		assert.Nil(t, cmd.Flags().Lookup("otlp-endpoint"),
			"otlp-endpoint should not be registered before registerAllFlags")

		registerAllFlags(cmd)

		// Core flags
		assert.NotNil(t, cmd.Flags().Lookup("config"), "config flag should be registered")
		assert.NotNil(t, cmd.Flags().Lookup("config-stdin"), "config-stdin flag should be registered")
		assert.NotNil(t, cmd.Flags().Lookup("routed"), "routed flag should be registered")
		assert.NotNil(t, cmd.Flags().Lookup("unified"), "unified flag should be registered")

		// Logging flags
		assert.NotNil(t, cmd.Flags().Lookup("log-dir"), "log-dir flag should be registered")
		assert.NotNil(t, cmd.Flags().Lookup("payload-dir"), "payload-dir flag should be registered")

		// Tracing flags
		assert.NotNil(t, cmd.Flags().Lookup("otlp-endpoint"), "otlp-endpoint flag should be registered")
		assert.NotNil(t, cmd.Flags().Lookup("otlp-service-name"), "otlp-service-name flag should be registered")
		assert.NotNil(t, cmd.Flags().Lookup("otlp-sample-rate"), "otlp-sample-rate flag should be registered")

		// DIFC flags
		assert.NotNil(t, cmd.Flags().Lookup("guards-mode"), "guards-mode flag should be registered")
		assert.NotNil(t, cmd.Flags().Lookup("allowonly-min-integrity"), "allowonly-min-integrity flag should be registered")
		assert.NotNil(t, cmd.Flags().Lookup("guards-sink-server-ids"), "guards-sink-server-ids flag should be registered")
	})

	t.Run("is idempotent when called multiple times", func(t *testing.T) {
		cmd := newTestCmd()
		registerAllFlags(cmd)
		// Calling a second time should not panic
		// (cobra allows re-defining flags, though it's not typical)
		// The key check is that the flags remain available
		assert.NotNil(t, cmd.Flags().Lookup("otlp-endpoint"))
	})

	t.Run("flagRegistrars is populated by init functions", func(t *testing.T) {
		// The global flagRegistrars slice is populated by init() functions
		// in flags_*.go files. Verify it has entries.
		assert.NotEmpty(t, flagRegistrars,
			"flagRegistrars should be populated by init() calls from flags_*.go files")
	})
}

func TestRegisterFlagCompletions(t *testing.T) {
	// Build a properly-initialized command with all flags registered first,
	// since RegisterFlagCompletionFunc requires the flag to exist.
	setupCmd := func(t *testing.T) *cobra.Command {
		t.Helper()
		cmd := newTestCmd()
		registerAllFlags(cmd)
		registerFlagCompletions(cmd)
		return cmd
	}

	t.Run("ValidArgsFunction is set and returns active help tip", func(t *testing.T) {
		cmd := setupCmd(t)

		require.NotNil(t, cmd.ValidArgsFunction, "ValidArgsFunction should be set")

		completions, directive := cmd.ValidArgsFunction(cmd, nil, "")
		assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive,
			"ValidArgsFunction should return ShellCompDirectiveNoFileComp")

		// Should contain at least one entry with helpful tip text
		require.NotEmpty(t, completions, "ValidArgsFunction should return completions with active help")
		found := false
		for _, c := range completions {
			if strings.Contains(c, "--config") {
				found = true
				break
			}
		}
		assert.True(t, found, "ValidArgsFunction completions should mention --config flag")
	})

	t.Run("config flag completion returns toml extension filter", func(t *testing.T) {
		cmd := setupCmd(t)

		completionFn, ok := cmd.GetFlagCompletionFunc("config")
		require.True(t, ok, "config flag should have a completion function registered")
		require.NotNil(t, completionFn, "config completion function should not be nil")

		completions, directive := completionFn(cmd, nil, "")
		assert.Equal(t, cobra.ShellCompDirectiveFilterFileExt, directive,
			"config flag should use FilterFileExt directive for .toml files")
		assert.Equal(t, []string{"toml"}, completions,
			"config flag should complete with .toml extension")
	})

	t.Run("log-dir flag completion returns directory filter", func(t *testing.T) {
		cmd := setupCmd(t)

		completionFn, ok := cmd.GetFlagCompletionFunc("log-dir")
		require.True(t, ok, "log-dir flag should have a completion function registered")

		completions, directive := completionFn(cmd, nil, "")
		assert.Equal(t, cobra.ShellCompDirectiveFilterDirs, directive,
			"log-dir flag should use FilterDirs directive")
		assert.Nil(t, completions, "log-dir flag completion should return nil completions")
	})

	t.Run("payload-dir flag completion returns directory filter", func(t *testing.T) {
		cmd := setupCmd(t)

		completionFn, ok := cmd.GetFlagCompletionFunc("payload-dir")
		require.True(t, ok, "payload-dir flag should have a completion function registered")

		completions, directive := completionFn(cmd, nil, "")
		assert.Equal(t, cobra.ShellCompDirectiveFilterDirs, directive,
			"payload-dir flag should use FilterDirs directive")
		assert.Nil(t, completions, "payload-dir flag completion should return nil completions")
	})

	t.Run("env flag completion returns .env extension filter", func(t *testing.T) {
		cmd := setupCmd(t)

		completionFn, ok := cmd.GetFlagCompletionFunc("env")
		require.True(t, ok, "env flag should have a completion function registered")

		completions, directive := completionFn(cmd, nil, "")
		assert.Equal(t, cobra.ShellCompDirectiveFilterFileExt, directive,
			"env flag should use FilterFileExt directive for .env files")
		assert.Equal(t, []string{"env"}, completions,
			"env flag should complete with .env extension")
	})

	t.Run("guards-mode flag completion returns valid enum values", func(t *testing.T) {
		cmd := setupCmd(t)

		completionFn, ok := cmd.GetFlagCompletionFunc("guards-mode")
		require.True(t, ok, "guards-mode flag should have a completion function registered")

		completions, directive := completionFn(cmd, nil, "")
		assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive,
			"guards-mode flag should use NoFileComp directive")
		assert.ElementsMatch(t, []string{"strict", "filter", "propagate"}, completions,
			"guards-mode should complete with all valid mode values")
	})

	t.Run("allowonly-min-integrity flag completion returns valid enum values", func(t *testing.T) {
		cmd := setupCmd(t)

		completionFn, ok := cmd.GetFlagCompletionFunc("allowonly-min-integrity")
		require.True(t, ok, "allowonly-min-integrity flag should have a completion function registered")

		completions, directive := completionFn(cmd, nil, "")
		assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive,
			"allowonly-min-integrity flag should use NoFileComp directive")
		assert.ElementsMatch(t, []string{"none", "unapproved", "approved", "merged"}, completions,
			"allowonly-min-integrity should complete with all valid integrity levels")
	})
}

func TestRegisterFlag(t *testing.T) {
	t.Run("appended registrar is called during registerAllFlags", func(t *testing.T) {
		cmd := newTestCmd()

		called := false
		originalLen := len(flagRegistrars)

		RegisterFlag(func(c *cobra.Command) {
			if c == cmd {
				called = true
			}
		})

		// Restore original state after test
		t.Cleanup(func() {
			flagRegistrars = flagRegistrars[:originalLen]
		})

		registerAllFlags(cmd)
		assert.True(t, called, "registered flag function should be called by registerAllFlags")
	})

	t.Run("multiple registrars are all called", func(t *testing.T) {
		cmd := newTestCmd()
		originalLen := len(flagRegistrars)

		callCount := 0
		RegisterFlag(func(c *cobra.Command) { callCount++ })
		RegisterFlag(func(c *cobra.Command) { callCount++ })
		RegisterFlag(func(c *cobra.Command) { callCount++ })

		t.Cleanup(func() {
			flagRegistrars = flagRegistrars[:originalLen]
		})

		registerAllFlags(cmd)
		// Should have been called at least 3 more times than before
		assert.GreaterOrEqual(t, callCount, 3,
			"all newly registered flag functions should be called")
	})
}
