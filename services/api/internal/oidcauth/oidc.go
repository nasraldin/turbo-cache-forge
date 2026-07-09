// Package oidcauth authenticates /api/v1 (dashboard/management humans) with OIDC/JWT.
// It imports go-oidc and is mounted ONLY on /api/v1 — the cache path (internal/auth,
// internal/turbo) must never import this package. Two auth worlds, never mixed.
package oidcauth

import (
	"context"
	"net/http"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/auth"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/db"
)

type Config struct {
	Issuer     string
	JWKSURL    string
	Audience   string
	OrgClaim   string // JWT claim holding the IdP org id; default "org_id"
	OrgEnabled bool   // false = personal mode: tenant derived from `sub`, audience check skipped
}

type OrgProvisioner interface {
	EnsureOrgByIdpID(ctx context.Context, idpOrgID, name string) (*db.Org, error)
}

type Authenticator struct {
	verifier   *oidc.IDTokenVerifier
	orgClaim   string
	orgEnabled bool
	repo       OrgProvisioner
}

func New(ctx context.Context, cfg Config, repo OrgProvisioner) (*Authenticator, error) {
	orgClaim := cfg.OrgClaim
	if orgClaim == "" {
		orgClaim = "org_id"
	}
	var verifier *oidc.IDTokenVerifier
	// Org mode pins the audience (multi-tenant IdP). Personal mode is a single-tenant
	// self-host that trusts its own issuer, so it skips the audience check — the default
	// Clerk session token carries no matching `aud`.
	oc := &oidc.Config{ClientID: cfg.Audience, SkipClientIDCheck: !cfg.OrgEnabled} // signature+expiry+issuer stay default-on
	if cfg.JWKSURL != "" {
		keySet := oidc.NewRemoteKeySet(ctx, cfg.JWKSURL)
		verifier = oidc.NewVerifier(cfg.Issuer, keySet, oc)
	} else {
		provider, err := oidc.NewProvider(ctx, cfg.Issuer) // discovery finds jwks_uri
		if err != nil {
			return nil, err
		}
		verifier = provider.VerifierContext(ctx, oc)
	}
	return &Authenticator{verifier: verifier, orgClaim: orgClaim, orgEnabled: cfg.OrgEnabled, repo: repo}, nil
}

func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, ok := bearer(r)
		if !ok {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		idt, err := a.verifier.Verify(r.Context(), raw) // signature + issuer + audience + expiry
		if err != nil {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		var claims map[string]any
		if err := idt.Claims(&claims); err != nil {
			http.Error(w, "invalid claims", http.StatusUnauthorized)
			return
		}
		// Org mode: tenant = the IdP org. Personal mode: tenant = the user (`sub`),
		// so each self-host user is their own single-tenant org.
		var idpOrg, name string
		if a.orgEnabled {
			idpOrg, _ = claims[a.orgClaim].(string)
			name, _ = claims["org_name"].(string) // best-effort display name; falls back to idp id
		} else {
			idpOrg, _ = claims["sub"].(string)
		}
		if idpOrg == "" {
			http.Error(w, "missing tenant claim", http.StatusUnauthorized)
			return
		}
		org, err := a.repo.EnsureOrgByIdpID(r.Context(), idpOrg, name)
		if err != nil {
			http.Error(w, "org provisioning failed", http.StatusInternalServerError)
			return
		}
		next.ServeHTTP(w, r.WithContext(auth.WithOrg(r.Context(), org)))
	})
}

func bearer(r *http.Request) (string, bool) {
	const p = "Bearer "
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, p) {
		return "", false
	}
	return strings.TrimPrefix(h, p), true
}
