package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTokenCreateCommand(t *testing.T) {
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
			t.Fatalf("name = %q, want ci", body["name"])
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "turbo_plaintext123"})
	}))
	defer srv.Close()

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TURBO_CACHE_TOKEN", "test-jwt")

	root := NewRootCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetArgs([]string{"token", "create", "--name", "ci", "--api", srv.URL})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "turbo_plaintext123") {
		t.Fatalf("output = %q, want the plaintext token", buf.String())
	}
}

func TestTokenCreateRequiresName(t *testing.T) {
	root := NewRootCmd()
	root.SetArgs([]string{"token", "create"})
	root.SetOut(&bytes.Buffer{})
	if err := root.Execute(); err == nil {
		t.Fatal("expected an error when --name is missing")
	}
}
