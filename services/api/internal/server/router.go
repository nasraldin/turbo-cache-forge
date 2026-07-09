package server

import (
	"context"
	"net/http"

	sentryhttp "github.com/getsentry/sentry-go/http"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/auth"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/db"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/mgmt"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/obs"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/oidcauth"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/openapi"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/storage"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/turbo"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/usage"
)

// Deps holds everything the router needs. Fields are added as tasks land.
type Deps struct {
	Store          storage.Storage
	Repo           *db.Repo
	MaxUploadBytes int64
	Usage          *usage.Accumulator
	Auth           *oidcauth.Authenticator
}

func New(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer) // outer: always produces the actual 500 response
	// inner: catches the panic first as it unwinds, reports to Sentry, then
	// repanics outward (Repanic: true) for Recoverer above to still handle.
	r.Use(sentryhttp.New(sentryhttp.Options{Repanic: true}).Handle)

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
		th := turbo.NewHandler(d.Store, d.Repo, d.MaxUploadBytes, m, d.Usage)
		r.Group(func(pr chi.Router) {
			pr.Use(auth.RequireToken(d.Repo))
			th.Mount(pr)
		})
	}

	// Management API (OIDC/JWT) + docs — mounted only when OIDC is configured.
	if d.Auth != nil && d.Repo != nil {
		mh := mgmt.NewHandler(d.Repo)
		r.Route("/api/v1", func(ar chi.Router) {
			// public docs
			ar.Get("/openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/yaml")
				_, _ = w.Write(openapi.Spec)
			})
			ar.Handle("/docs/*", http.StripPrefix("/api/v1/docs", openapi.Handler()))
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
