package server

import (
	"net/http"
	"strings"
)

// corsMiddleware echoes CORS headers for the configured browser origins and
// answers preflight OPTIONS directly. It runs at the top of the chain (before
// chi routing) because a preflight OPTIONS to a GET-only route would otherwise
// hit chi's 405 handler without any route middleware ever running. No-op when
// no origins are configured, so non-browser deploys behave exactly as before.
func corsMiddleware(origins []string) func(http.Handler) http.Handler {
	allowed := make(map[string]bool, len(origins))
	for _, o := range origins {
		if o = strings.TrimSpace(o); o != "" {
			allowed[o] = true
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && allowed[origin] {
				h := w.Header()
				h.Set("Access-Control-Allow-Origin", origin)
				h.Add("Vary", "Origin") // response varies per-origin; keep caches honest
				h.Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
				h.Set("Access-Control-Max-Age", "300")
				if r.Method == http.MethodOptions { // preflight — nothing else to do
					w.WriteHeader(http.StatusNoContent)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
