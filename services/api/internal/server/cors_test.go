package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORSPreflightAndActual(t *testing.T) {
	mw := corsMiddleware([]string{"http://localhost:3000"})
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := mw(next)

	// preflight from an allowed origin: 204 with ACAO, next handler not run
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/stats", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight code = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Fatalf("ACAO = %q, want the request origin", got)
	}

	// actual GET from allowed origin: passes through, ACAO present
	req = httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Fatalf("actual request: code=%d ACAO=%q", rec.Code, rec.Header().Get("Access-Control-Allow-Origin"))
	}

	// disallowed origin: no ACAO, and OPTIONS is NOT short-circuited (falls through)
	req = httptest.NewRequest(http.MethodOptions, "/api/v1/stats", nil)
	req.Header.Set("Origin", "http://evil.example")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("disallowed origin should not receive ACAO")
	}
	if rec.Code != http.StatusOK { // next handler ran (200), not 204
		t.Fatalf("disallowed preflight code = %d, want 200 (passed through)", rec.Code)
	}
}
