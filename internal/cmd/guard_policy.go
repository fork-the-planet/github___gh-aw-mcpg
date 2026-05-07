package cmd

import (
	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/spf13/cobra"
)

func resolveGuardPolicyOverride(cmd *cobra.Command) (*config.GuardPolicy, string, error) {
	cliGuardPolicyChanged := cmd.Flags().Changed("guard-policy-json")
	cliChanged := cliGuardPolicyChanged ||
		cmd.Flags().Changed("allowonly-scope-public") ||
		cmd.Flags().Changed("allowonly-scope-owner") ||
		cmd.Flags().Changed("allowonly-scope-repo") ||
		cmd.Flags().Changed("allowonly-min-integrity")

	cliPolicyJSON := ""
	if cliGuardPolicyChanged {
		cliPolicyJSON = guardPolicyJSON
	}

	return config.ResolveGuardPolicyOverride(
		cliChanged,
		cliPolicyJSON,
		allowOnlyPublic,
		allowOnlyOwner,
		allowOnlyRepo,
		allowOnlyMinInt,
	)
}
