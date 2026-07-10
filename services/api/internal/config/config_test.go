package config

import (
	"os"
	"testing"
	"time"
)

func TestDatabaseURLDefaultsToSQLite(t *testing.T) {
	// builtin auth is the compose default; set the minimum so Load() succeeds.
	t.Setenv("DATABASE_URL", "")
	t.Setenv("AUTH_MODE", "builtin")
	t.Setenv("AUTH_ROOT_USERNAME", "root")
	t.Setenv("AUTH_ROOT_PASSWORD", "root")
	_ = os.Unsetenv("OIDC_ISSUER")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() with empty DATABASE_URL should succeed, got %v", err)
	}
	if c.DatabaseURL != "sqlite:///data/tcf.db" {
		t.Fatalf("default DatabaseURL = %q, want sqlite:///data/tcf.db", c.DatabaseURL)
	}
}

func TestDatabaseURLRespectsExplicit(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:p@h:5432/db")
	t.Setenv("AUTH_MODE", "builtin")
	t.Setenv("AUTH_ROOT_USERNAME", "root")
	t.Setenv("AUTH_ROOT_PASSWORD", "root")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.DatabaseURL != "postgres://u:p@h:5432/db" {
		t.Fatalf("explicit DATABASE_URL not honored: %q", c.DatabaseURL)
	}
}

func TestLoadDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != ":8080" {
		t.Errorf("Addr = %q, want :8080", c.Addr)
	}
	if c.StorageBackend != "fs" {
		t.Errorf("StorageBackend = %q, want fs", c.StorageBackend)
	}
	if c.MaxUploadBytes != 1<<30 {
		t.Errorf("MaxUploadBytes = %d, want %d", c.MaxUploadBytes, 1<<30)
	}
}

func TestLoadOIDCOptional(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("OIDC_ISSUER", "")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.OIDCOrgClaim != "org_id" {
		t.Errorf("OIDCOrgClaim default = %q, want org_id", c.OIDCOrgClaim)
	}
	if c.RetentionDays != 30 {
		t.Errorf("RetentionDays default = %d, want 30", c.RetentionDays)
	}
}

func TestLoadOIDCRequiresAudience(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("OIDC_ISSUER", "https://issuer.example")
	t.Setenv("OIDC_AUDIENCE", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected error: OIDC_AUDIENCE required when OIDC_ISSUER set")
	}
}

func TestLoadOrgEnabledDefaultsTrue(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !c.OIDCOrgEnabled {
		t.Error("OIDCOrgEnabled default = false, want true")
	}
}

// Personal mode (OIDC_ORG_ENABLED=false) lifts the audience requirement.
func TestLoadPersonalModeNoAudience(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("OIDC_ISSUER", "https://issuer.example")
	t.Setenv("OIDC_AUDIENCE", "")
	t.Setenv("OIDC_ORG_ENABLED", "false")
	c, err := Load()
	if err != nil {
		t.Fatalf("personal mode should not require OIDC_AUDIENCE: %v", err)
	}
	if c.OIDCOrgEnabled {
		t.Error("OIDCOrgEnabled = true, want false")
	}
}

func TestLoadBuiltinAuth(t *testing.T) {
	// base sets the minimum env on the SUBTEST's t, so t.Setenv cleanup is
	// scoped per subtest and never leaks into the next one.
	base := func(t *testing.T) {
		t.Setenv("DATABASE_URL", "postgres://x")
		t.Setenv("AUTH_MODE", "builtin")
	}

	t.Run("defaults ttl to 12h and reads username+password", func(t *testing.T) {
		base(t)
		t.Setenv("AUTH_ROOT_USERNAME", "root")
		t.Setenv("AUTH_ROOT_PASSWORD", "hunter2")
		c, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if c.AuthMode != "builtin" || c.AuthRootUsername != "root" || c.AuthRootPassword != "hunter2" {
			t.Fatalf("unexpected config: %+v", c)
		}
		if c.AuthTokenTTL != 12*time.Hour {
			t.Fatalf("ttl = %v, want 12h", c.AuthTokenTTL)
		}
	})

	t.Run("missing username fails", func(t *testing.T) {
		base(t)
		t.Setenv("AUTH_ROOT_PASSWORD", "hunter2")
		if _, err := Load(); err == nil {
			t.Fatal("expected error for missing AUTH_ROOT_USERNAME")
		}
	})

	t.Run("missing password fails", func(t *testing.T) {
		base(t)
		t.Setenv("AUTH_ROOT_USERNAME", "root")
		if _, err := Load(); err == nil {
			t.Fatal("expected error for missing password")
		}
	})

	t.Run("both password and hash fails", func(t *testing.T) {
		base(t)
		t.Setenv("AUTH_ROOT_USERNAME", "root")
		t.Setenv("AUTH_ROOT_PASSWORD", "hunter2")
		t.Setenv("AUTH_ROOT_PASSWORD_HASH", "$2a$10$abc")
		if _, err := Load(); err == nil {
			t.Fatal("expected error when both password and hash set")
		}
	})

	t.Run("unknown mode fails", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "postgres://x")
		t.Setenv("AUTH_MODE", "ldap")
		if _, err := Load(); err == nil {
			t.Fatal("expected error for unknown AUTH_MODE")
		}
	})

	t.Run("default mode is oidc, no auth vars required", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "postgres://x")
		c, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if c.AuthMode != "oidc" {
			t.Fatalf("default AuthMode = %q, want oidc", c.AuthMode)
		}
	})
}
