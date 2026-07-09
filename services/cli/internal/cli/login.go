package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/nasraldin/turbo-cache-forge/services/cli/internal/config"
	"github.com/nasraldin/turbo-cache-forge/services/cli/internal/oidcdevice"
)

func newLoginCmd() *cobra.Command {
	var issuer, clientID, scope string
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate via your OIDC provider's device flow",
		RunE: func(cmd *cobra.Command, _ []string) error {
			issuer = config.Pick(issuer, os.Getenv("TURBO_CACHE_OIDC_ISSUER"), "")
			clientID = config.Pick(clientID, os.Getenv("TURBO_CACHE_OIDC_CLIENT_ID"), "")
			if issuer == "" || clientID == "" {
				return fmt.Errorf("--issuer/--client-id (or TURBO_CACHE_OIDC_ISSUER/TURBO_CACHE_OIDC_CLIENT_ID) are required")
			}

			token, err := oidcdevice.Login(cmd.Context(), issuer, clientID, scope,
				func(da oidcdevice.DeviceAuthResponse) {
					fmt.Fprintf(cmd.OutOrStdout(), "Go to %s and enter code: %s\n", da.VerificationURI, da.UserCode)
					if da.VerificationURIComplete != "" {
						fmt.Fprintf(cmd.OutOrStdout(), "(or open directly: %s)\n", da.VerificationURIComplete)
					}
				}, time.Sleep)
			if err != nil {
				return fmt.Errorf("login: %w", err)
			}

			f, err := config.Load()
			if err != nil {
				return err
			}
			f.Token = token
			if api, _ := cmd.Flags().GetString("api"); api != "" {
				f.APIURL = api // persist the server the user just logged into
			}
			if err := config.Save(f); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Logged in — token saved to your turbo-cache config.")
			return nil
		},
	}
	cmd.Flags().StringVar(&issuer, "issuer", "", "OIDC issuer URL (matches the server's OIDC_ISSUER)")
	cmd.Flags().StringVar(&clientID, "client-id", "", "public OIDC client ID registered for device flow")
	cmd.Flags().StringVar(&scope, "scope", "openid profile email offline_access", "OIDC scopes to request")
	return cmd
}
