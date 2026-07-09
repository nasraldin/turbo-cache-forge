package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/nasraldin/turbo-cache-forge/services/cli/internal/apiclient"
	"github.com/nasraldin/turbo-cache-forge/services/cli/internal/config"
)

type checkResult struct {
	Name   string
	OK     bool
	Detail string
}

// runDoctor performs every self-host diagnostic. httpClient is injected so
// tests never depend on a real network. Checks after "API URL" and "auth"
// are skipped once those prerequisites are known to be missing — there's
// nothing more useful to say without them.
func runDoctor(ctx context.Context, httpClient *http.Client, base, token string) []checkResult {
	var results []checkResult

	// 1. config file present + private.
	if p, err := config.Path(); err != nil {
		results = append(results, checkResult{"config file", false, err.Error()})
	} else if info, statErr := os.Stat(p); statErr != nil {
		results = append(results, checkResult{"config file", false, fmt.Sprintf("not found at %s (run `turbo-cache login`)", p)})
	} else if perm := info.Mode().Perm(); perm != 0o600 {
		results = append(results, checkResult{"config file", false, fmt.Sprintf("%s has mode %o, want 0600", p, perm)})
	} else {
		results = append(results, checkResult{"config file", true, p})
	}

	// 2. API URL configured.
	if base == "" {
		results = append(results, checkResult{"API URL", false, "not set — pass --api, set TURBO_CACHE_API, or run `turbo-cache login`"})
		return results // nothing downstream is reachable without a base URL
	}
	results = append(results, checkResult{"API URL", true, base})

	// 3. server reachable — hit the Turbo protocol status endpoint. ANY HTTP
	// response (even 401, since it's hashed-bearer-protected) proves the
	// process is up; only a transport error means it's actually down.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/v8/artifacts/status", nil)
	if err != nil {
		results = append(results, checkResult{"server reachable", false, err.Error()})
	} else if resp, err := httpClient.Do(req); err != nil {
		results = append(results, checkResult{"server reachable", false, err.Error()})
	} else {
		resp.Body.Close()
		results = append(results, checkResult{"server reachable", true, fmt.Sprintf("HTTP %d from %s", resp.StatusCode, base)})
	}

	// 4. auth valid — a JWT-protected /api/v1 call must return 200.
	if token == "" {
		results = append(results, checkResult{"auth", false, "not logged in — run `turbo-cache login`"})
		return results
	}
	client := apiclient.New(base, token)
	client.HTTP = httpClient
	if _, err := client.Stats(ctx); err != nil {
		results = append(results, checkResult{"auth", false, fmt.Sprintf("%v — re-run `turbo-cache login`", err)})
	} else {
		results = append(results, checkResult{"auth", true, "token accepted by /api/v1"})
	}

	return results
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose a misconfigured self-host (config, connectivity, auth)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			flagAPI, _ := cmd.Flags().GetString("api")
			f, err := config.Load()
			if err != nil {
				return err
			}
			base := config.Pick(flagAPI, os.Getenv("TURBO_CACHE_API"), f.APIURL)
			token := config.Pick("", os.Getenv("TURBO_CACHE_TOKEN"), f.Token)

			httpClient := &http.Client{Timeout: 5 * time.Second}
			results := runDoctor(cmd.Context(), httpClient, base, token)

			out := cmd.OutOrStdout()
			failed := 0
			for _, r := range results {
				status := "OK  "
				if !r.OK {
					status = "FAIL"
					failed++
				}
				fmt.Fprintf(out, "[%s] %-16s %s\n", status, r.Name, r.Detail)
			}
			if failed > 0 {
				return fmt.Errorf("%d check(s) failed", failed)
			}
			fmt.Fprintln(out, "\nAll checks passed.")
			return nil
		},
	}
}
