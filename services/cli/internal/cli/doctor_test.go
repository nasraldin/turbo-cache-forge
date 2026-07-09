package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRunDoctorHealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v8/artifacts/status":
			// hashed-token protected; a 401 still proves the process is up
			w.WriteHeader(http.StatusUnauthorized)
		case "/api/v1/stats":
			if r.Header.Get("Authorization") != "Bearer good-jwt" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]int64{"storage_bytes": 1, "hits": 1, "misses": 0})
		}
	}))
	defer srv.Close()

	results := runDoctor(context.Background(), srv.Client(), srv.URL, "good-jwt")
	assertCheck(t, results, "server reachable", true)
	assertCheck(t, results, "auth", true)
}

func TestRunDoctorStaleToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	results := runDoctor(context.Background(), srv.Client(), srv.URL, "stale-jwt")
	assertCheck(t, results, "server reachable", true)
	assertCheck(t, results, "auth", false)
}

func TestRunDoctorServerDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	base := srv.URL
	srv.Close() // nothing is listening at base anymore

	results := runDoctor(context.Background(), http.DefaultClient, base, "any-token")
	assertCheck(t, results, "server reachable", false)
}

func TestRunDoctorNoAPIURLConfigured(t *testing.T) {
	results := runDoctor(context.Background(), http.DefaultClient, "", "")
	assertCheck(t, results, "API URL", false)
}

func TestRunDoctorNotLoggedIn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	results := runDoctor(context.Background(), srv.Client(), srv.URL, "")
	assertCheck(t, results, "auth", false)
}

func assertCheck(t *testing.T, results []checkResult, name string, wantOK bool) {
	t.Helper()
	for _, r := range results {
		if r.Name == name {
			if r.OK != wantOK {
				t.Fatalf("check %q OK = %v, want %v (detail: %s)", name, r.OK, wantOK, r.Detail)
			}
			return
		}
	}
	t.Fatalf("check %q not found in results: %+v", name, results)
}
