package server

import (
	"context"
	"encoding/json"
	"net/http"

	sentryhttp "github.com/getsentry/sentry-go/http"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/auth"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/db"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/mgmt"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/obs"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/openapi"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/storage"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/turbo"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/usage"
)

// Authenticator is satisfied by both oidcauth.Authenticator and
// localauth.Authenticator — the router does not care which world is active.
type Authenticator interface {
	Middleware(next http.Handler) http.Handler
}

// Deps holds everything the router needs. Fields are added as tasks land.
type Deps struct {
	Store          storage.Storage
	Repo           *db.Repo
	MaxUploadBytes int64
	Usage          *usage.Accumulator
	Auth           Authenticator    // oidcauth (oidc) or localauth (builtin)
	AuthMode       string           // "oidc" | "builtin" — reported at /api/v1/auth/config
	OrgEnabled     bool             // reported at /api/v1/auth/config
	Login          http.HandlerFunc // non-nil only in builtin mode; serves POST /auth/login
	CORSOrigins    []string         // browser origins allowed to call /api/v1; empty = CORS off
	RequireSignature bool           // REQUIRE_ARTIFACT_SIGNATURE: enforce x-artifact-tag on the cache path
}

func New(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer) // outer: always produces the actual 500 response
	// inner: catches the panic first as it unwinds, reports to Sentry, then
	// repanics outward (Repanic: true) for Recoverer above to still handle.
	r.Use(sentryhttp.New(sentryhttp.Options{Repanic: true}).Handle)

	if len(d.CORSOrigins) > 0 {
		r.Use(corsMiddleware(d.CORSOrigins)) // before routing so preflight OPTIONS never hits chi's 405
	}

	m := obs.NewMetrics()
	r.Use(m.Middleware(func(req *http.Request) string {
		if rc := chi.RouteContext(req.Context()); rc != nil && rc.RoutePattern() != "" {
			return rc.RoutePattern()
		}
		return "unknown"
	}))

	// ops endpoints (unauthenticated)
	r.Get("/live", obs.Live)
	r.Get("/health", readyHandler(d))
	r.Get("/ready", readyHandler(d))
	r.Handle("/metrics", m.Handler())

	// authenticated Turbo protocol
	if d.Repo != nil {
		th := turbo.NewHandler(d.Store, d.Repo, d.MaxUploadBytes, m, d.Usage, d.RequireSignature)
		r.Group(func(pr chi.Router) {
			pr.Use(auth.RequireToken(d.Repo))
			th.Mount(pr)
		})
	}

	// Management API (OIDC/JWT) + docs — mounted only when OIDC is configured.
	if d.Auth != nil && d.Repo != nil {
		mh := mgmt.NewHandler(d.Repo, d.Store)
		r.Route("/api/v1", func(ar chi.Router) {
			// public docs
			ar.Get("/openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/yaml")
				_, _ = w.Write(openapi.Spec)
			})
			ar.Handle("/docs/*", http.StripPrefix("/api/v1/docs", openapi.Handler()))
			// public auth discovery — lets the dashboard pick its sign-in UI.
			ar.Get("/auth/config", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"mode":        d.AuthMode,
					"org_enabled": d.OrgEnabled,
				})
			})
			if d.Login != nil { // builtin mode only
				ar.Post("/auth/login", d.Login)
			}
			// authenticated management routes
			ar.Group(func(pr chi.Router) {
				pr.Use(d.Auth.Middleware)
				mh.Mount(pr)
			})
		})
	}
	return otelhttp.NewHandler(r, "turbo-cache-forge")
}

func readyHandler(d Deps) http.HandlerFunc {
	return obs.Ready(func(ctx context.Context) error {
		if d.Repo != nil {
			return d.Repo.Ping(ctx)
		}
		return nil
	})
}
