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
	Issuer   string
	JWKSURL  string
	Audience string
	OrgClaim string // JWT claim holding the IdP org id; default "org_id"
}

type OrgProvisioner interface {
	EnsureOrgByIdpID(ctx context.Context, idpOrgID, name string) (*db.Org, error)
}

type Authenticator struct {
	verifier *oidc.IDTokenVerifier
	orgClaim string
	repo     OrgProvisioner
}

func New(ctx context.Context, cfg Config, repo OrgProvisioner) (*Authenticator, error) {
	orgClaim := cfg.OrgClaim
	if orgClaim == "" {
		orgClaim = "org_id"
	}
	var verifier *oidc.IDTokenVerifier
	oc := &oidc.Config{ClientID: cfg.Audience} // enforces audience; signature+expiry+issuer are default-on
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
	return &Authenticator{verifier: verifier, orgClaim: orgClaim, repo: repo}, nil
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
		idpOrg, _ := claims[a.orgClaim].(string)
		if idpOrg == "" {
			http.Error(w, "missing org claim", http.StatusUnauthorized)
			return
		}
		name, _ := claims["org_name"].(string) // best-effort display name; falls back to idp id
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
