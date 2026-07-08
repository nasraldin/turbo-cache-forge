package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/db"
)

type OrgLookup interface {
	OrgByTokenHash(ctx context.Context, hash string) (*db.Org, error)
}

type ctxKey struct{}

func RequireToken(lookup OrgLookup) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearer(r)
			if !ok {
				http.Error(w, "missing bearer token", http.StatusUnauthorized)
				return
			}
			org, err := lookup.OrgByTokenHash(r.Context(), HashToken(token))
			if err != nil {
				http.Error(w, "invalid token", http.StatusUnauthorized)
				return
			}
			ctx := WithOrg(r.Context(), org)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// WithOrg stores org in context (used by RequireToken and by tests).
func WithOrg(ctx context.Context, org *db.Org) context.Context {
	return context.WithValue(ctx, ctxKey{}, org)
}

func bearer(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	const p = "Bearer "
	if !strings.HasPrefix(h, p) {
		return "", false
	}
	return strings.TrimPrefix(h, p), true
}

func OrgFromContext(ctx context.Context) (*db.Org, bool) {
	org, ok := ctx.Value(ctxKey{}).(*db.Org)
	return org, ok
}
