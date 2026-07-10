package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr                 string
	DatabaseURL          string
	StorageBackend       string // "fs" | "s3"
	StoragePath          string
	S3Bucket             string
	S3Endpoint           string
	S3Region             string
	MaxUploadBytes       int64
	OIDCIssuer           string
	OIDCJWKSURL          string
	OIDCAudience         string
	OIDCOrgClaim         string
	OIDCOrgEnabled       bool          // false = personal/single-tenant mode (tenant from `sub`, audience check skipped)
	AuthMode             string        // "oidc" (default) | "builtin"
	AuthRootUsername     string        // builtin: the single root identity
	AuthRootPassword     string        // builtin: plaintext (bcrypt-hashed at boot); XOR with hash
	AuthRootPasswordHash string        // builtin: precomputed bcrypt hash; XOR with plaintext
	AuthSecret           string        // builtin: HS256 secret; random per-boot if empty
	AuthTokenTTL         time.Duration // builtin: session JWT lifetime (default 12h)
	CORSAllowedOrigins   []string      // browser origins allowed to call /api/v1; empty = CORS off
	RetentionDays        int
	RollupIntervalSec    int
	CleanupIntervalSec   int
	// RequireArtifactSignature enables Turbo artifact-signature support on the
	// cache path: PUTs must carry an x-artifact-tag (else 400), and GET/HEAD of a
	// tagless artifact is a miss (404). Signing/verification is client-side; the
	// server only round-trips the tag. Off by default — keeps the download hot
	// path DB-free.
	RequireArtifactSignature bool
}

func Load() (Config, error) {
	c := Config{
		Addr:           env("ADDR", ":8080"),
		// Default to a self-migrating SQLite file (zero external setup). Point
		// DATABASE_URL at postgres://… to use Postgres instead.
		DatabaseURL:    env("DATABASE_URL", "sqlite:///data/tcf.db"),
		StorageBackend: env("STORAGE_BACKEND", "fs"),
		StoragePath:    env("STORAGE_PATH", "/var/lib/turbo-cache-forge"),
		S3Bucket:       os.Getenv("STORAGE_S3_BUCKET"),
		S3Endpoint:     os.Getenv("STORAGE_S3_ENDPOINT"),
		S3Region:       env("STORAGE_S3_REGION", "auto"),
		MaxUploadBytes: envInt("MAX_UPLOAD_BYTES", 1<<30), // 1 GiB
	}
	if c.StorageBackend == "s3" && c.S3Bucket == "" {
		return c, fmt.Errorf("STORAGE_S3_BUCKET is required when STORAGE_BACKEND=s3")
	}

	// ponytail: OIDC optional — cache-only self-hosts skip it entirely; no dashboard deps forced on them.
	c.OIDCIssuer = os.Getenv("OIDC_ISSUER")
	c.OIDCJWKSURL = os.Getenv("OIDC_JWKS_URL")
	c.OIDCAudience = os.Getenv("OIDC_AUDIENCE")
	c.OIDCOrgClaim = env("OIDC_ORG_CLAIM", "org_id")
	c.OIDCOrgEnabled = envBool("OIDC_ORG_ENABLED", true)
	c.CORSAllowedOrigins = csv(os.Getenv("CORS_ALLOWED_ORIGINS")) // e.g. "http://localhost:3000"
	c.RetentionDays = int(envInt("RETENTION_DAYS", 30))
	c.RollupIntervalSec = int(envInt("USAGE_ROLLUP_INTERVAL_SEC", 300))
	c.CleanupIntervalSec = int(envInt("CLEANUP_INTERVAL_SEC", 3600))
	c.RequireArtifactSignature = envBool("REQUIRE_ARTIFACT_SIGNATURE", false)

	// Audience only pins a tenant when orgs are on. In personal mode the tenant
	// is the user's `sub`, so a self-host trusting its own issuer needs no audience.
	if c.OIDCIssuer != "" && c.OIDCOrgEnabled && c.OIDCAudience == "" {
		return c, fmt.Errorf("OIDC_AUDIENCE is required when OIDC_ISSUER is set (unless OIDC_ORG_ENABLED=false)")
	}

	c.AuthMode = env("AUTH_MODE", "oidc")
	if c.AuthMode != "oidc" && c.AuthMode != "builtin" {
		return c, fmt.Errorf("AUTH_MODE must be 'oidc' or 'builtin', got %q", c.AuthMode)
	}
	c.AuthRootUsername = os.Getenv("AUTH_ROOT_USERNAME")
	c.AuthRootPassword = os.Getenv("AUTH_ROOT_PASSWORD")
	c.AuthRootPasswordHash = os.Getenv("AUTH_ROOT_PASSWORD_HASH")
	c.AuthSecret = os.Getenv("AUTH_SECRET")
	c.AuthTokenTTL = envDuration("AUTH_TOKEN_TTL", 12*time.Hour)
	if c.AuthMode == "builtin" {
		if c.AuthRootUsername == "" {
			return c, fmt.Errorf("AUTH_ROOT_USERNAME is required when AUTH_MODE=builtin")
		}
		hasPw, hasHash := c.AuthRootPassword != "", c.AuthRootPasswordHash != ""
		if hasPw == hasHash { // neither, or both
			return c, fmt.Errorf("exactly one of AUTH_ROOT_PASSWORD or AUTH_ROOT_PASSWORD_HASH is required when AUTH_MODE=builtin")
		}
	}

	return c, nil
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int64) int64 {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}

// csv splits a comma-separated env value, trimming blanks. "" -> nil.
func csv(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func envBool(k string, def bool) bool {
	if v := os.Getenv(k); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

func envDuration(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
