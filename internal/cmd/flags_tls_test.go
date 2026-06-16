package cmd

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTLSCmd builds a command with TLS flags registered, mirroring
// how rootCmd is initialized in production.
func setupTLSCmd(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{
		Use:          "test",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}
	// Register TLS flags (and the MarkFlagsRequiredTogether constraint).
	cmd.Flags().StringVar(new(string), "tls-cert", "", "Path to TLS cert")
	cmd.Flags().StringVar(new(string), "tls-key", "", "Path to TLS key")
	cmd.Flags().StringVar(new(string), "tls-ca", "", "Path to CA cert")
	cmd.MarkFlagsRequiredTogether("tls-cert", "tls-key")
	return cmd
}

// TestTLSFlagsRequiredTogether verifies that cobra's MarkFlagsRequiredTogether
// constraint on --tls-cert and --tls-key is wired up correctly.
func TestTLSFlagsRequiredTogether(t *testing.T) {
	t.Run("cert without key is rejected by cobra", func(t *testing.T) {
		cmd := setupTLSCmd(t)
		cmd.SetArgs([]string{"--tls-cert", "/tmp/cert.pem"})
		err := cmd.Execute()
		require.Error(t, err, "should fail when only --tls-cert is provided")
		assert.Contains(t, err.Error(), "tls-cert", "error should mention tls-cert")
		assert.Contains(t, err.Error(), "tls-key", "error should mention tls-key")
	})

	t.Run("key without cert is rejected by cobra", func(t *testing.T) {
		cmd := setupTLSCmd(t)
		cmd.SetArgs([]string{"--tls-key", "/tmp/key.pem"})
		err := cmd.Execute()
		require.Error(t, err, "should fail when only --tls-key is provided")
		assert.Contains(t, err.Error(), "tls-cert", "error should mention tls-cert")
		assert.Contains(t, err.Error(), "tls-key", "error should mention tls-key")
	})

	t.Run("cert and key together are accepted", func(t *testing.T) {
		cmd := setupTLSCmd(t)
		cmd.SetArgs([]string{"--tls-cert", "/tmp/cert.pem", "--tls-key", "/tmp/key.pem"})
		err := cmd.Execute()
		require.NoError(t, err, "should succeed when both --tls-cert and --tls-key are provided")
	})

	t.Run("neither cert nor key is accepted (TLS disabled)", func(t *testing.T) {
		cmd := setupTLSCmd(t)
		cmd.SetArgs([]string{})
		err := cmd.Execute()
		require.NoError(t, err, "should succeed when neither TLS flag is provided")
	})

	t.Run("cert key and ca together are accepted", func(t *testing.T) {
		cmd := setupTLSCmd(t)
		cmd.SetArgs([]string{"--tls-cert", "/tmp/cert.pem", "--tls-key", "/tmp/key.pem", "--tls-ca", "/tmp/ca.pem"})
		err := cmd.Execute()
		require.NoError(t, err, "should succeed when --tls-cert, --tls-key, and --tls-ca are all provided")
	})
}

// TestTLSFlagsRegistered verifies that the TLS flags exist on the root command.
func TestTLSFlagsRegistered(t *testing.T) {
	assert.NotNil(t, rootCmd.Flags().Lookup("tls-cert"), "tls-cert flag should be registered on rootCmd")
	assert.NotNil(t, rootCmd.Flags().Lookup("tls-key"), "tls-key flag should be registered on rootCmd")
	assert.NotNil(t, rootCmd.Flags().Lookup("tls-ca"), "tls-ca flag should be registered on rootCmd")
	assert.NotNil(t, rootCmd.Flags().Lookup("hmac-secret"), "hmac-secret flag should be registered on rootCmd")
}
