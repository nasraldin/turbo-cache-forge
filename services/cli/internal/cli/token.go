package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newTokenCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "token", Short: "Manage cache API tokens"}
	cmd.AddCommand(newTokenCreateCmd())
	return cmd
}

func newTokenCreateCmd() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a cache API token (the plaintext value is shown once)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if name == "" {
				return fmt.Errorf("--name is required")
			}
			flagAPI, _ := cmd.Flags().GetString("api")
			client, err := resolveClient(flagAPI)
			if err != nil {
				return err
			}
			plaintext, err := client.CreateToken(cmd.Context(), name)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "Token created — save it now, it will not be shown again:")
			fmt.Fprintln(out, plaintext)
			fmt.Fprintf(out, "\nUse it with Turborepo:\n  export TURBO_TOKEN=%s\n", plaintext)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "human-readable name for the token")
	return cmd
}
