package mgmt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/artifactview"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/auth"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/db"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/storage"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/turbo"
)

var slugRe = regexp.MustCompile(`^[a-z0-9-]+$`)

type Repo interface {
	CreateToken(ctx context.Context, orgID int64, name, tokenHash string, readOnly bool) (int64, error)
	ListTokens(ctx context.Context, orgID int64) ([]db.APIKey, error)
	RevokeToken(ctx context.Context, orgID, tokenID int64) (bool, error)
	CreateProject(ctx context.Context, orgID int64, slug, name string) (db.Project, error)
	ListProjects(ctx context.Context, orgID int64) ([]db.Project, error)
	Stats(ctx context.Context, orgID int64) (db.Stats, error)
	StatsSeries(ctx context.Context, orgID int64, days int) ([]db.StatsPoint, error)
	ListArtifacts(ctx context.Context, orgID int64, limit, offset int) ([]db.Artifact, error)
	GetArtifact(ctx context.Context, orgID int64, hash string) (db.Artifact, error)
	ListArtifactHashes(ctx context.Context, orgID int64) ([]string, error)
	DeleteAllArtifacts(ctx context.Context, orgID int64) (int64, error)
	DeleteArtifact(ctx context.Context, orgID int64, hash string) error
}

type Handler struct {
	repo  Repo
	store storage.Storage
}

func NewHandler(repo Repo, store storage.Storage) *Handler {
	return &Handler{repo: repo, store: store}
}

func (h *Handler) Mount(r chi.Router) {
	r.Post("/tokens", h.createToken)
	r.Get("/tokens", h.listTokens)
	r.Delete("/tokens/{id}", h.revokeToken)
	r.Post("/projects", h.createProject)
	r.Get("/projects", h.listProjects)
	r.Get("/stats", h.stats)
	r.Get("/stats/timeseries", h.statsTimeseries)
	r.Get("/artifacts", h.listArtifacts)
	r.Get("/artifacts/{hash}", h.getArtifact)
	r.Get("/artifacts/{hash}/download", h.downloadArtifact)
	r.Delete("/artifacts/{hash}", h.deleteArtifact)
	r.Delete("/artifacts", h.clearArtifacts)
}

func (h *Handler) createToken(w http.ResponseWriter, r *http.Request) {
	org, ok := auth.OrgFromContext(r.Context())
	if !ok {
		http.Error(w, "no org", http.StatusUnauthorized)
		return
	}
	var in struct {
		Name     string `json:"name"`
		ReadOnly bool   `json:"read_only"`
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
	id, err := h.repo.CreateToken(r.Context(), org.ID, in.Name, hash, in.ReadOnly)
	if err != nil {
		http.Error(w, "create failed", http.StatusInternalServerError)
		return
	}
	// plaintext returned exactly once; only the hash is persisted
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "name": in.Name, "read_only": in.ReadOnly, "token": token})
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
			"id": k.ID, "name": k.Name, "read_only": k.ReadOnly, "created_at": k.CreatedAt,
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

func (h *Handler) getArtifact(w http.ResponseWriter, r *http.Request) {
	org, ok := auth.OrgFromContext(r.Context())
	if !ok {
		http.Error(w, "no org", http.StatusUnauthorized)
		return
	}
	hash := chi.URLParam(r, "hash")
	if !turbo.ValidHash(hash) {
		http.Error(w, "bad hash", http.StatusBadRequest)
		return
	}
	a, err := h.repo.GetArtifact(r.Context(), org.ID, hash)
	if errors.Is(err, db.ErrArtifactNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "get failed", http.StatusInternalServerError)
		return
	}
	// Blob is optional: a row can outlive its blob (cleanup deletes blob first),
	// which degrades to an opaque/empty manifest. A real storage failure, though,
	// must surface as 500 — not masquerade as "encrypted/opaque" content.
	content := artifactview.Manifest{Format: "opaque", Entries: []artifactview.Entry{}}
	rc, _, gerr := h.store.Get(r.Context(), turbo.StorageKey(org.Slug, hash))
	switch {
	case gerr == nil:
		defer rc.Close()
		content = artifactview.Decode(rc)
	case errors.Is(gerr, storage.ErrNotFound):
		// row present, blob gone — leave the opaque/empty default
	default:
		http.Error(w, "get failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"hash": a.Hash, "size_bytes": a.SizeBytes, "tag": a.Tag,
		"created_at": a.CreatedAt, "last_accessed_at": a.LastAccessedAt,
		"content": content,
	})
}

func (h *Handler) downloadArtifact(w http.ResponseWriter, r *http.Request) {
	org, ok := auth.OrgFromContext(r.Context())
	if !ok {
		http.Error(w, "no org", http.StatusUnauthorized)
		return
	}
	hash := chi.URLParam(r, "hash")
	if !turbo.ValidHash(hash) {
		http.Error(w, "bad hash", http.StatusBadRequest)
		return
	}
	rc, info, err := h.store.Get(r.Context(), turbo.StorageKey(org.Slug, hash))
	if errors.Is(err, storage.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "get failed", http.StatusInternalServerError)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", hash+".tar.zst"))
	if info != nil {
		w.Header().Set("Content-Length", strconv.FormatInt(info.Size, 10))
	}
	_, _ = io.Copy(w, rc)
}

func (h *Handler) deleteArtifact(w http.ResponseWriter, r *http.Request) {
	org, ok := auth.OrgFromContext(r.Context())
	if !ok {
		http.Error(w, "no org", http.StatusUnauthorized)
		return
	}
	hash := chi.URLParam(r, "hash")
	if !turbo.ValidHash(hash) {
		http.Error(w, "bad hash", http.StatusBadRequest)
		return
	}
	// blob first, then row — mirrors cleanup.RunOnce (avoids orphan blobs).
	if err := h.store.Delete(r.Context(), turbo.StorageKey(org.Slug, hash)); err != nil {
		http.Error(w, "delete failed", http.StatusInternalServerError)
		return
	}
	if err := h.repo.DeleteArtifact(r.Context(), org.ID, hash); err != nil {
		http.Error(w, "delete failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) clearArtifacts(w http.ResponseWriter, r *http.Request) {
	org, ok := auth.OrgFromContext(r.Context())
	if !ok {
		http.Error(w, "no org", http.StatusUnauthorized)
		return
	}
	hashes, err := h.repo.ListArtifactHashes(r.Context(), org.ID)
	if err != nil {
		http.Error(w, "clear failed", http.StatusInternalServerError)
		return
	}
	for _, hh := range hashes {
		// Best-effort per blob; a failed blob delete leaves its row for the
		// cleanup cron, but we still clear the rows below.
		_ = h.store.Delete(r.Context(), turbo.StorageKey(org.Slug, hh))
	}
	n, err := h.repo.DeleteAllArtifacts(r.Context(), org.ID)
	if err != nil {
		http.Error(w, "clear failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": n})
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
