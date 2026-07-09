package turbo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/auth"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/obs"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/storage"
)

type ArtifactStore interface {
	Put(ctx context.Context, key string, r io.Reader) error
	Get(ctx context.Context, key string) (io.ReadCloser, *storage.ObjectInfo, error)
	Head(ctx context.Context, key string) (*storage.ObjectInfo, error)
	Delete(ctx context.Context, key string) error
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
	metrics  *obs.Metrics
}

func NewHandler(store ArtifactStore, repo MetaRepo, maxBytes int64, metrics *obs.Metrics) *Handler {
	return &Handler{store: store, repo: repo, maxBytes: maxBytes, metrics: metrics}
}

func (h *Handler) Mount(r chi.Router) {
	r.Get("/v8/artifacts/status", h.status)
	r.Head("/v8/artifacts/{hash}", h.head)
	r.Put("/v8/artifacts/{hash}", h.put)
	r.Get("/v8/artifacts/{hash}", h.get)
	r.Post("/v8/artifacts/events", h.events) // telemetry sink
	r.Post("/v8/artifacts", h.batchExists)
}

const (
	maxBatchHashes    = 1000
	batchBodyMaxBytes = 1 << 20 // 1 MiB — a hash list never needs an artifact-sized body
)

type batchRequest struct {
	Hashes []string `json:"hashes"`
}

type batchArtifact struct {
	Exists bool `json:"exists"`
}

type batchResponse struct {
	Hashes map[string]batchArtifact `json:"hashes"`
}

// batchExists lets a client ask which of many hashes are already cached in
// one round trip instead of one HEAD per hash — the intended consumer of
// MetaRepo.ArtifactExists, reserved for exactly this in Phase 1.
func (h *Handler) batchExists(w http.ResponseWriter, r *http.Request) {
	org, _ := auth.OrgFromContext(r.Context())

	body := http.MaxBytesReader(w, r.Body, batchBodyMaxBytes)
	defer body.Close()

	var req batchRequest
	if err := json.NewDecoder(body).Decode(&req); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if len(req.Hashes) == 0 || len(req.Hashes) > maxBatchHashes {
		http.Error(w, fmt.Sprintf("hashes must contain 1..%d entries", maxBatchHashes), http.StatusBadRequest)
		return
	}

	out := make(map[string]batchArtifact, len(req.Hashes))
	for _, hash := range req.Hashes {
		if !validHash(hash) {
			http.Error(w, "invalid hash: "+hash, http.StatusBadRequest)
			return
		}
		exists, err := h.repo.ArtifactExists(r.Context(), org.ID, hash)
		if err != nil {
			obs.CaptureError(err)
			http.Error(w, "lookup failed", http.StatusInternalServerError)
			return
		}
		out[hash] = batchArtifact{Exists: exists}
	}
	writeJSON(w, http.StatusOK, batchResponse{Hashes: out})
}

func (h *Handler) status(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "enabled"})
}

func (h *Handler) events(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK) // no-op sink
}

func (h *Handler) head(w http.ResponseWriter, r *http.Request) {
	hash := chi.URLParam(r, "hash")
	if !validHash(hash) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	org, _ := auth.OrgFromContext(r.Context())
	key := storageKey(org.Slug, hash)
	if _, err := h.store.Head(r.Context(), key); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) put(w http.ResponseWriter, r *http.Request) {
	hash := chi.URLParam(r, "hash")
	if !validHash(hash) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	org, _ := auth.OrgFromContext(r.Context())
	key := storageKey(org.Slug, hash)

	body := http.MaxBytesReader(w, r.Body, h.maxBytes)
	if err := h.store.Put(r.Context(), key, body); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			http.Error(w, "artifact too large", http.StatusRequestEntityTooLarge)
			return
		}
		obs.CaptureError(err)
		http.Error(w, "upload failed", http.StatusInternalServerError)
		return
	}
	info, err := h.store.Head(r.Context(), key)
	if err != nil {
		obs.CaptureError(err)
		http.Error(w, "upload verify failed", http.StatusInternalServerError)
		return
	}
	tag := r.Header.Get("x-artifact-tag")
	if err := h.repo.UpsertArtifact(r.Context(), org.ID, hash, info.Size, tag); err != nil {
		// Best-effort compensating delete — see Task 5's Decision note in the
		// Phase 2 plan for why this is eager rather than a repair-sweep TODO.
		if delErr := h.store.Delete(r.Context(), key); delErr != nil {
			obs.CaptureError(delErr)
			log.Printf("turbo: put %s: compensating delete after metadata failure also failed: %v", key, delErr)
		}
		obs.CaptureError(err)
		http.Error(w, "metadata write failed", http.StatusInternalServerError)
		return
	}
	h.metrics.UploadBytes.Add(float64(info.Size))
	writeJSON(w, http.StatusAccepted, map[string][]string{"urls": {key}})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	hash := chi.URLParam(r, "hash")
	if !validHash(hash) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	org, _ := auth.OrgFromContext(r.Context())
	key := storageKey(org.Slug, hash)

	rc, info, err := h.store.Get(r.Context(), key)
	if errors.Is(err, storage.ErrNotFound) {
		h.metrics.CacheMiss.Inc()
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if err != nil {
		obs.CaptureError(err)
		http.Error(w, "download failed", http.StatusInternalServerError)
		return
	}
	defer rc.Close()
	h.metrics.CacheHit.Inc()
	h.metrics.DownloadBytes.Add(float64(info.Size))

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
