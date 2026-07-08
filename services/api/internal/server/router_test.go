package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLiveEndpoint(t *testing.T) {
	srv := New(Deps{})
	req := httptest.NewRequest(http.MethodGet, "/live", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /live = %d, want 200", rec.Code)
	}
}

func TestMetricsEndpoint(t *testing.T) {
	srv := New(Deps{}) // no repo → Turbo routes skipped, metrics still up
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics = %d, want 200", rec.Code)
	}
}
