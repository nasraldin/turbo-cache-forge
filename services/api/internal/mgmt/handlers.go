package mgmt

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/auth"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/db"
)

type Repo interface {
	CreateToken(ctx context.Context, orgID int64, name, tokenHash string) (int64, error)
	ListTokens(ctx context.Context, orgID int64) ([]db.APIKey, error)
	RevokeToken(ctx context.Context, orgID, tokenID int64) (bool, error)
	CreateProject(ctx context.Context, orgID int64, slug, name string) (db.Project, error)
	ListProjects(ctx context.Context, orgID int64) ([]db.Project, error)
	Stats(ctx context.Context, orgID int64) (db.Stats, error)
	ListArtifacts(ctx context.Context, orgID int64, limit, offset int) ([]db.Artifact, error)
}

type Handler struct{ repo Repo }

func NewHandler(repo Repo) *Handler { return &Handler{repo: repo} }

func (h *Handler) Mount(r chi.Router) {
	r.Post("/tokens", h.createToken)
	r.Get("/tokens", h.listTokens)
	r.Delete("/tokens/{id}", h.revokeToken)
	r.Post("/projects", h.createProject)
	r.Get("/projects", h.listProjects)
	r.Get("/stats", h.stats)
	r.Get("/artifacts", h.listArtifacts)
}

func (h *Handler) createToken(w http.ResponseWriter, r *http.Request) {
	org, ok := auth.OrgFromContext(r.Context())
	if !ok {
		http.Error(w, "no org", http.StatusUnauthorized)
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	token, hash, err := auth.GenerateToken()
	if err != nil {
		http.Error(w, "token generation failed", http.StatusInternalServerError)
		return
	}
	id, err := h.repo.CreateToken(r.Context(), org.ID, in.Name, hash)
	if err != nil {
		http.Error(w, "create failed", http.StatusInternalServerError)
		return
	}
	// plaintext returned exactly once; only the hash is persisted
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "name": in.Name, "token": token})
}

func (h *Handler) listTokens(w http.ResponseWriter, r *http.Request) {
	org, _ := auth.OrgFromContext(r.Context())
	keys, err := h.repo.ListTokens(r.Context(), org.ID)
	if err != nil {
		http.Error(w, "list failed", http.StatusInternalServerError)
		return
	}
	out := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		out = append(out, map[string]any{
			"id": k.ID, "name": k.Name, "created_at": k.CreatedAt,
			"last_used_at": k.LastUsedAt, "revoked_at": k.RevokedAt,
		}) // never includes token_hash
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) revokeToken(w http.ResponseWriter, r *http.Request) {
	org, _ := auth.OrgFromContext(r.Context())
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	ok, err := h.repo.RevokeToken(r.Context(), org.ID, id)
	if err != nil {
		http.Error(w, "revoke failed", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "not found", http.StatusNotFound) // unknown or another org's token
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Task 6 implements these; stubs keep the package compiling and Mount complete.
func (h *Handler) createProject(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "todo", http.StatusNotImplemented)
}
func (h *Handler) listProjects(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "todo", http.StatusNotImplemented)
}
func (h *Handler) stats(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "todo", http.StatusNotImplemented)
}
func (h *Handler) listArtifacts(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "todo", http.StatusNotImplemented)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
