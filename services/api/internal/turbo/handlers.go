package turbo

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/auth"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/storage"
)

type ArtifactStore interface {
	Put(ctx context.Context, key string, r io.Reader) error
	Get(ctx context.Context, key string) (io.ReadCloser, *storage.ObjectInfo, error)
	Head(ctx context.Context, key string) (*storage.ObjectInfo, error)
}

type MetaRepo interface {
	UpsertArtifact(ctx context.Context, orgID int64, hash string, size int64, tag string) error
	ArtifactExists(ctx context.Context, orgID int64, hash string) (bool, error)
	TouchArtifact(ctx context.Context, orgID int64, hash string) error
}

type Handler struct {
	store    ArtifactStore
	repo     MetaRepo
	maxBytes int64
}

func NewHandler(store ArtifactStore, repo MetaRepo, maxBytes int64) *Handler {
	return &Handler{store: store, repo: repo, maxBytes: maxBytes}
}

func (h *Handler) Mount(r chi.Router) {
	r.Get("/v8/artifacts/status", h.status)
	r.Head("/v8/artifacts/{hash}", h.head)
	r.Put("/v8/artifacts/{hash}", h.put)
	r.Get("/v8/artifacts/{hash}", h.get)
	r.Post("/v8/artifacts/events", h.events) // telemetry sink
}

func (h *Handler) status(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "enabled"})
}

func (h *Handler) events(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK) // no-op sink
}

func (h *Handler) head(w http.ResponseWriter, r *http.Request) {
	org, _ := auth.OrgFromContext(r.Context())
	key := storageKey(org.Slug, chi.URLParam(r, "hash"))
	if _, err := h.store.Head(r.Context(), key); err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) put(w http.ResponseWriter, r *http.Request) {
	org, _ := auth.OrgFromContext(r.Context())
	hash := chi.URLParam(r, "hash")
	key := storageKey(org.Slug, hash)

	body := http.MaxBytesReader(w, r.Body, h.maxBytes)
	if err := h.store.Put(r.Context(), key, body); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			http.Error(w, "artifact too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "upload failed", http.StatusInternalServerError)
		return
	}
	info, err := h.store.Head(r.Context(), key)
	if err != nil {
		http.Error(w, "upload verify failed", http.StatusInternalServerError)
		return
	}
	tag := r.Header.Get("x-artifact-tag")
	if err := h.repo.UpsertArtifact(r.Context(), org.ID, hash, info.Size, tag); err != nil {
		http.Error(w, "metadata write failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string][]string{"urls": {key}})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	org, _ := auth.OrgFromContext(r.Context())
	hash := chi.URLParam(r, "hash")
	key := storageKey(org.Slug, hash)

	rc, info, err := h.store.Get(r.Context(), key)
	if errors.Is(err, storage.ErrNotFound) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "download failed", http.StatusInternalServerError)
		return
	}
	defer rc.Close()

	// fire-and-forget last_accessed bump — never block the download on the DB
	// ponytail: fire-and-forget touch; batch it only if last_accessed write volume ever shows up in DB metrics
	go func(orgID int64, hash string) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = h.repo.TouchArtifact(ctx, orgID, hash)
	}(org.ID, hash)

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", itoa(info.Size))
	if tag := r.Header.Get("x-artifact-tag"); tag != "" {
		w.Header().Set("x-artifact-tag", tag)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, rc)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
