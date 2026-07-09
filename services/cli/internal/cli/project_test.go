package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nasraldin/turbo-cache-forge/services/cli/internal/apiclient"
)

func TestProjectCreateCommand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/projects" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var p apiclient.Project
		_ = json.NewDecoder(r.Body).Decode(&p)
		if p.Slug != "web" || p.Name != "Web App" {
			t.Fatalf("request body = %+v", p)
		}
		_ = json.NewEncoder(w).Encode(p)
	}))
	defer srv.Close()

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TURBO_CACHE_TOKEN", "test-jwt")

	root := NewRootCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetArgs([]string{"project", "create", "--slug", "web", "--name", "Web App", "--api", srv.URL})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "web") {
		t.Fatalf("output = %q, want the created project slug", buf.String())
	}
}
