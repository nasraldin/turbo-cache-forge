package openapi

import (
	"net/http/httptest"
	"testing"

	"go.yaml.in/yaml/v2"
)

func TestSpecParses(t *testing.T) {
	if len(Spec) == 0 {
		t.Fatal("Spec is empty")
	}
	var doc map[string]any
	if err := yaml.Unmarshal(Spec, &doc); err != nil {
		t.Fatalf("Spec does not parse as YAML: %v", err)
	}
	if doc["openapi"] == nil {
		t.Fatal("Spec missing openapi version field")
	}
	if doc["paths"] == nil {
		t.Fatal("Spec missing paths")
	}
}

func TestHandlerServesUI(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}
