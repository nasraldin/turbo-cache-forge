package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/obs"
)

// Deps holds everything the router needs. Fields are added as tasks land.
type Deps struct{}

func New(_ Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Get("/live", obs.Live)
	r.Get("/ready", obs.Ready(nil))
	r.Get("/health", obs.Ready(nil))
	return r
}
