package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newProjectCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "project", Short: "Manage projects"}
	cmd.AddCommand(newProjectCreateCmd())
	return cmd
}

func newProjectCreateCmd() *cobra.Command {
	var slug, name string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a project",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if slug == "" || name == "" {
				return fmt.Errorf("--slug and --name are required")
			}
			flagAPI, _ := cmd.Flags().GetString("api")
			client, err := resolveClient(flagAPI)
			if err != nil {
				return err
			}
			p, err := client.CreateProject(cmd.Context(), slug, name)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created project %q (%s)\n", p.Name, p.Slug)
			return nil
		},
	}
	cmd.Flags().StringVar(&slug, "slug", "", "project slug (e.g. web)")
	cmd.Flags().StringVar(&name, "name", "", "human-readable project name")
	return cmd
}
