package apiclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/tokens" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-jwt" {
			t.Fatalf("Authorization = %q", got)
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "ci" {
			t.Fatalf("request name = %q, want ci", body["name"])
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "turbo_plaintext123"})
	}))
	defer srv.Close()

	c := New(srv.URL, "test-jwt")
	got, err := c.CreateToken(context.Background(), "ci")
	if err != nil {
		t.Fatal(err)
	}
	if got != "turbo_plaintext123" {
		t.Fatalf("CreateToken() = %q, want turbo_plaintext123", got)
	}
}

func TestCreateProject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/projects" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(Project{Slug: "web", Name: "Web App"})
	}))
	defer srv.Close()

	c := New(srv.URL, "test-jwt")
	got, err := c.CreateProject(context.Background(), "web", "Web App")
	if err != nil {
		t.Fatal(err)
	}
	if got.Slug != "web" || got.Name != "Web App" {
		t.Fatalf("CreateProject() = %+v", got)
	}
}

func TestStats(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/stats" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		// real /api/v1/stats shape (snake_case); no requests_24h / hit_rate exist
		_, _ = w.Write([]byte(`{"storage_bytes":2048,"artifact_count":3,"hits":30,"misses":10,"requests":40,"bytes_up":100,"bytes_down":200}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "test-jwt")
	got, err := c.Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.StorageBytes != 2048 || got.Hits != 30 || got.Misses != 10 || got.Requests != 40 {
		t.Fatalf("Stats() = %+v", got)
	}
}

func TestNon2xxReturnsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("invalid token"))
	}))
	defer srv.Close()

	c := New(srv.URL, "bad-jwt")
	_, err := c.Stats(context.Background())
	var apiErr *APIError
	if err == nil {
		t.Fatal("expected an error")
	}
	if !asAPIError(err, &apiErr) || apiErr.StatusCode != 401 || !strings.Contains(apiErr.Message, "invalid token") {
		t.Fatalf("err = %v, want *APIError{401, invalid token}", err)
	}
}

func asAPIError(err error, target **APIError) bool {
	e, ok := err.(*APIError)
	if ok {
		*target = e
	}
	return ok
}
