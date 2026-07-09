package cli

import (
	"testing"

	"github.com/nasraldin/turbo-cache-forge/services/cli/internal/config"
)

func TestResolveClientPrecedence(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := config.Save(config.File{APIURL: "https://from-file", Token: "file-jwt"}); err != nil {
		t.Fatal(err)
	}

	t.Run("file is the fallback", func(t *testing.T) {
		t.Setenv("TURBO_CACHE_API", "")
		t.Setenv("TURBO_CACHE_TOKEN", "")
		c, err := resolveClient("")
		if err != nil {
			t.Fatal(err)
		}
		if c.BaseURL != "https://from-file" || c.Token != "file-jwt" {
			t.Fatalf("client = %+v", c)
		}
	})

	t.Run("env beats file", func(t *testing.T) {
		t.Setenv("TURBO_CACHE_API", "https://from-env")
		t.Setenv("TURBO_CACHE_TOKEN", "env-jwt")
		c, err := resolveClient("")
		if err != nil {
			t.Fatal(err)
		}
		if c.BaseURL != "https://from-env" || c.Token != "env-jwt" {
			t.Fatalf("client = %+v", c)
		}
	})

	t.Run("flag beats env and file", func(t *testing.T) {
		t.Setenv("TURBO_CACHE_API", "https://from-env")
		c, err := resolveClient("https://from-flag")
		if err != nil {
			t.Fatal(err)
		}
		if c.BaseURL != "https://from-flag" {
			t.Fatalf("client.BaseURL = %q, want https://from-flag", c.BaseURL)
		}
	})
}

func TestResolveClientErrorsWithNoAPIURL(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TURBO_CACHE_API", "")
	if _, err := resolveClient(""); err == nil {
		t.Fatal("expected an error when no API URL is configured")
	}
}

func TestResolveClientErrorsWithNoToken(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TURBO_CACHE_API", "")
	t.Setenv("TURBO_CACHE_TOKEN", "")
	if err := config.Save(config.File{APIURL: "https://has-url", Token: ""}); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveClient(""); err == nil {
		t.Fatal("expected an error when not logged in")
	}
}
