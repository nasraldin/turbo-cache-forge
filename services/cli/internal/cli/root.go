package cli

import (
	"github.com/spf13/cobra"
)

// Version is set at release build time via
// -ldflags "-X .../internal/cli.Version=v0.1.0" (see Task 9).
var Version = "dev"

// NewRootCmd builds the full command tree. Subcommands are added here as
// each later task lands.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:     "turbo-cache",
		Short:   "Self-host management CLI for turbo-cache-forge",
		Version: Version,
	}
	root.SetVersionTemplate("turbo-cache version {{.Version}}\n")
	root.PersistentFlags().String("api", "", "management API base URL (overrides TURBO_CACHE_API and the config file)")
	root.AddCommand(newLoginCmd())
	root.AddCommand(newTokenCmd())
	root.AddCommand(newProjectCmd())
	root.AddCommand(newStatsCmd())
	return root
}

// Execute is the CLI entrypoint called from main.
func Execute() error {
	return NewRootCmd().Execute()
}
