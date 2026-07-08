package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/db"
)

type fakeLookup struct{ hash string }

func (f fakeLookup) OrgByTokenHash(_ context.Context, hash string) (*db.Org, error) {
	if hash == f.hash {
		return &db.Org{ID: 1, Slug: "team-a"}, nil
	}
	return nil, db.ErrUnauthorized
}

func TestRequireToken(t *testing.T) {
	valid := "turbo_secret"
	mw := RequireToken(fakeLookup{hash: HashToken(valid)})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		org, ok := OrgFromContext(r.Context())
		if !ok || org.Slug != "team-a" {
			t.Error("org not in context")
		}
		w.WriteHeader(http.StatusOK)
	}))

	cases := []struct {
		name, header string
		want         int
	}{
		{"valid", "Bearer " + valid, http.StatusOK},
		{"wrong token", "Bearer turbo_nope", http.StatusUnauthorized},
		{"missing header", "", http.StatusUnauthorized},
		{"malformed", "Token xyz", http.StatusUnauthorized},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/x", nil)
			if c.header != "" {
				req.Header.Set("Authorization", c.header)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != c.want {
				t.Errorf("%s: code = %d, want %d", c.name, rec.Code, c.want)
			}
		})
	}
}
