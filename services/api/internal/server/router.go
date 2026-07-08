package server

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/auth"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/db"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/obs"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/storage"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/turbo"
)

// Deps holds everything the router needs. Fields are added as tasks land.
type Deps struct {
	Store          storage.Storage
	Repo           *db.Repo
	MaxUploadBytes int64
}

func New(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)

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
		th := turbo.NewHandler(d.Store, d.Repo, d.MaxUploadBytes, m)
		r.Group(func(pr chi.Router) {
			pr.Use(auth.RequireToken(d.Repo))
			th.Mount(pr)
		})
	}
	return r
}

func readyHandler(d Deps) http.HandlerFunc {
	return obs.Ready(func(ctx context.Context) error {
		if d.Repo != nil {
			return d.Repo.Ping(ctx)
		}
		return nil
	})
}
