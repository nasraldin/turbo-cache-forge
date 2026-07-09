package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Addr               string
	DatabaseURL        string
	StorageBackend     string // "fs" | "s3"
	StoragePath        string
	S3Bucket           string
	S3Endpoint         string
	S3Region           string
	MaxUploadBytes     int64
	OIDCIssuer         string
	OIDCJWKSURL        string
	OIDCAudience       string
	OIDCOrgClaim       string
	OIDCOrgEnabled     bool     // false = personal/single-tenant mode (tenant from `sub`, audience check skipped)
	CORSAllowedOrigins []string // browser origins allowed to call /api/v1; empty = CORS off
	RetentionDays      int
	RollupIntervalSec  int
	CleanupIntervalSec int
}

func Load() (Config, error) {
	c := Config{
		Addr:           env("ADDR", ":8080"),
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		StorageBackend: env("STORAGE_BACKEND", "fs"),
		StoragePath:    env("STORAGE_PATH", "/var/lib/turbo-cache-forge"),
		S3Bucket:       os.Getenv("STORAGE_S3_BUCKET"),
		S3Endpoint:     os.Getenv("STORAGE_S3_ENDPOINT"),
		S3Region:       env("STORAGE_S3_REGION", "auto"),
		MaxUploadBytes: envInt("MAX_UPLOAD_BYTES", 1<<30), // 1 GiB
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("DATABASE_URL is required")
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

	// Audience only pins a tenant when orgs are on. In personal mode the tenant
	// is the user's `sub`, so a self-host trusting its own issuer needs no audience.
	if c.OIDCIssuer != "" && c.OIDCOrgEnabled && c.OIDCAudience == "" {
		return c, fmt.Errorf("OIDC_AUDIENCE is required when OIDC_ISSUER is set (unless OIDC_ORG_ENABLED=false)")
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
