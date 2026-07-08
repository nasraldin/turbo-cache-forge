package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/config"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/db"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/server"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/storage"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/storage/filesystem"
	s3store "github.com/nasraldin/turbo-cache-forge/services/api/internal/storage/s3"
)

func main() {
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	repo, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer repo.Close()

	var store storage.Storage
	switch cfg.StorageBackend {
	case "s3":
		store, err = s3store.New(ctx, s3store.Config{
			Bucket: cfg.S3Bucket, Endpoint: cfg.S3Endpoint, Region: cfg.S3Region,
			AccessKey: getenv("STORAGE_S3_ACCESS_KEY"), SecretKey: getenv("STORAGE_S3_SECRET_KEY"),
		})
	default:
		store = filesystem.New(cfg.StoragePath)
	}
	if err != nil {
		log.Fatal(err)
	}

	srv := server.New(server.Deps{Store: store, Repo: repo, MaxUploadBytes: cfg.MaxUploadBytes})
	log.Printf("turbo-cache-forge listening on %s (backend=%s)", cfg.Addr, cfg.StorageBackend)
	if err := http.ListenAndServe(cfg.Addr, srv); err != nil {
		log.Fatal(err)
	}
}

func getenv(k string) string { return os.Getenv(k) }
