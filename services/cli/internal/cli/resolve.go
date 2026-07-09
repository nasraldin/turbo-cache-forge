package cli

import (
	"fmt"
	"os"

	"github.com/nasraldin/turbo-cache-forge/services/cli/internal/apiclient"
	"github.com/nasraldin/turbo-cache-forge/services/cli/internal/config"
)

// resolveClient applies flag > env > config-file precedence and builds an
// apiclient.Client. flagAPI is the --api persistent flag value (empty if
// unset). Every command that talks to /api/v1 goes through this function —
// it is the one place precedence is resolved.
func resolveClient(flagAPI string) (*apiclient.Client, error) {
	f, err := config.Load()
	if err != nil {
		return nil, err
	}
	base := config.Pick(flagAPI, os.Getenv("TURBO_CACHE_API"), f.APIURL)
	if base == "" {
		return nil, fmt.Errorf("no API URL configured: pass --api, set TURBO_CACHE_API, or run `turbo-cache login`")
	}
	token := config.Pick("", os.Getenv("TURBO_CACHE_TOKEN"), f.Token)
	if token == "" {
		return nil, fmt.Errorf("not logged in: run `turbo-cache login` or set TURBO_CACHE_TOKEN")
	}
	return apiclient.New(base, token), nil
}
