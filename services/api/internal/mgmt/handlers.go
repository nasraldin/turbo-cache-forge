package mgmt

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/auth"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/db"
)

var slugRe = regexp.MustCompile(`^[a-z0-9-]+$`)

type Repo interface {
	CreateToken(ctx context.Context, orgID int64, name, tokenHash string) (int64, error)
	ListTokens(ctx context.Context, orgID int64) ([]db.APIKey, error)
	RevokeToken(ctx context.Context, orgID, tokenID int64) (bool, error)
	CreateProject(ctx context.Context, orgID int64, slug, name string) (db.Project, error)
	ListProjects(ctx context.Context, orgID int64) ([]db.Project, error)
	Stats(ctx context.Context, orgID int64) (db.Stats, error)
	StatsSeries(ctx context.Context, orgID int64, days int) ([]db.StatsPoint, error)
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
	r.Get("/stats/timeseries", h.statsTimeseries)
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
	org, ok := auth.OrgFromContext(r.Context())
	if !ok {
		http.Error(w, "no org", http.StatusUnauthorized)
		return
	}
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
	org, ok := auth.OrgFromContext(r.Context())
	if !ok {
		http.Error(w, "no org", http.StatusUnauthorized)
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	revoked, err := h.repo.RevokeToken(r.Context(), org.ID, id)
	if err != nil {
		http.Error(w, "revoke failed", http.StatusInternalServerError)
		return
	}
	if !revoked {
		http.Error(w, "not found", http.StatusNotFound) // unknown or another org's token
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) createProject(w http.ResponseWriter, r *http.Request) {
	org, ok := auth.OrgFromContext(r.Context())
	if !ok {
		http.Error(w, "no org", http.StatusUnauthorized)
		return
	}
	var in struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Name == "" || !slugRe.MatchString(in.Slug) {
		http.Error(w, "slug must match ^[a-z0-9-]+$ and name is required", http.StatusBadRequest)
		return
	}
	p, err := h.repo.CreateProject(r.Context(), org.ID, in.Slug, in.Name)
	if err != nil {
		http.Error(w, "create failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (h *Handler) listProjects(w http.ResponseWriter, r *http.Request) {
	org, ok := auth.OrgFromContext(r.Context())
	if !ok {
		http.Error(w, "no org", http.StatusUnauthorized)
		return
	}
	ps, err := h.repo.ListProjects(r.Context(), org.ID)
	if err != nil {
		http.Error(w, "list failed", http.StatusInternalServerError)
		return
	}
	if ps == nil {
		ps = []db.Project{} // serialize [] not null
	}
	writeJSON(w, http.StatusOK, ps)
}

func (h *Handler) stats(w http.ResponseWriter, r *http.Request) {
	org, ok := auth.OrgFromContext(r.Context())
	if !ok {
		http.Error(w, "no org", http.StatusUnauthorized)
		return
	}
	s, err := h.repo.Stats(r.Context(), org.ID)
	if err != nil {
		http.Error(w, "stats failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"storage_bytes": s.StorageBytes, "artifact_count": s.ArtifactCount,
		"hits": s.Hits, "misses": s.Misses, "requests": s.Requests,
		"bytes_up": s.BytesUp, "bytes_down": s.BytesDown,
	})
}

func (h *Handler) statsTimeseries(w http.ResponseWriter, r *http.Request) {
	org, ok := auth.OrgFromContext(r.Context())
	if !ok {
		http.Error(w, "no org", http.StatusUnauthorized)
		return
	}
	days, err := parseClampedInt(r.URL.Query().Get("days"), 30, 1, 365)
	if err != nil {
		http.Error(w, "invalid days", http.StatusBadRequest)
		return
	}
	pts, err := h.repo.StatsSeries(r.Context(), org.ID, days)
	if err != nil {
		http.Error(w, "stats series failed", http.StatusInternalServerError)
		return
	}
	if pts == nil {
		pts = []db.StatsPoint{} // serialize [] not null
	}
	writeJSON(w, http.StatusOK, pts)
}

func (h *Handler) listArtifacts(w http.ResponseWriter, r *http.Request) {
	org, ok := auth.OrgFromContext(r.Context())
	if !ok {
		http.Error(w, "no org", http.StatusUnauthorized)
		return
	}
	limit, err := parseClampedInt(r.URL.Query().Get("limit"), 50, 1, 200)
	if err != nil {
		http.Error(w, "invalid limit", http.StatusBadRequest)
		return
	}
	offset, err := parseClampedInt(r.URL.Query().Get("offset"), 0, 0, 1<<31-1)
	if err != nil {
		http.Error(w, "invalid offset", http.StatusBadRequest)
		return
	}
	arts, err := h.repo.ListArtifacts(r.Context(), org.ID, limit, offset)
	if err != nil {
		http.Error(w, "list failed", http.StatusInternalServerError)
		return
	}
	if arts == nil {
		arts = []db.Artifact{} // serialize [] not null
	}
	writeJSON(w, http.StatusOK, map[string]any{"limit": limit, "offset": offset, "artifacts": arts})
}

// parseClampedInt parses s (empty => def). A present-but-non-numeric value is
// rejected with an error rather than silently clamped, so bad input surfaces
// as 400 instead of being coerced into something the caller didn't ask for.
func parseClampedInt(s string, def, lo, hi int) (int, error) {
	if s == "" {
		return def, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}
	if n < lo {
		n = lo
	}
	if n > hi {
		n = hi
	}
	return n, nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
