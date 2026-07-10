package main

import (
	"context"
	"crypto/rand"
	"log"
	"net/http"
	"os"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/cleanup"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/config"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/db"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/localauth"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/obs"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/oidcauth"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/server"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/storage"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/storage/filesystem"
	s3store "github.com/nasraldin/turbo-cache-forge/services/api/internal/storage/s3"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/usage"
)

func main() {
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	shutdownTracer, err := obs.InitTracer(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownTracer(shutCtx)
	}()

	flushSentry, err := obs.InitSentry()
	if err != nil {
		log.Fatal(err)
	}
	defer flushSentry()

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
	store = storage.WithTracing(store) // applies to whichever backend was selected above

	acc := usage.New()

	var authn server.Authenticator
	var loginHandler http.HandlerFunc
	switch cfg.AuthMode {
	case "builtin":
		hash := []byte(cfg.AuthRootPasswordHash)
		if len(hash) == 0 {
			hash, err = bcrypt.GenerateFromPassword([]byte(cfg.AuthRootPassword), bcrypt.DefaultCost)
			if err != nil {
				log.Fatalf("bcrypt root password: %v", err)
			}
		}
		secret := []byte(cfg.AuthSecret)
		if len(secret) == 0 {
			secret = randomSecret()
			log.Printf("AUTH_SECRET unset — generated an ephemeral signing secret; " +
				"sessions will not survive a restart or span replicas. Set AUTH_SECRET for stable sessions.")
		}
		la, lerr := localauth.New(localauth.Config{
			RootUsername: cfg.AuthRootUsername, PasswordHash: hash,
			Secret: secret, TTL: cfg.AuthTokenTTL,
		}, repo)
		if lerr != nil {
			log.Fatalf("localauth init: %v", lerr)
		}
		authn, loginHandler = la, localauth.LoginHandler(la)
		log.Printf("management API enabled at /api/v1 — BUILTIN MODE: root user %q (TTL=%s)",
			cfg.AuthRootUsername, cfg.AuthTokenTTL)
	default: // "oidc"
		if cfg.OIDCIssuer != "" {
			oa, oerr := oidcauth.New(ctx, oidcauth.Config{
				Issuer:     cfg.OIDCIssuer,
				JWKSURL:    cfg.OIDCJWKSURL,
				Audience:   cfg.OIDCAudience,
				OrgClaim:   cfg.OIDCOrgClaim,
				OrgEnabled: cfg.OIDCOrgEnabled,
			}, repo)
			if oerr != nil {
				log.Fatalf("oidc init: %v", oerr)
			}
			authn = oa
			if cfg.OIDCOrgEnabled {
				log.Printf("management API enabled at /api/v1 (issuer=%s)", cfg.OIDCIssuer)
			} else {
				log.Printf("management API enabled at /api/v1 (issuer=%s) — PERSONAL MODE: audience check skipped, tenant=sub. "+
					"Only safe when OIDC_ISSUER is dedicated to this app; a shared multi-app issuer lets any of its tokens in.", cfg.OIDCIssuer)
			}
		}
	}

	// background jobs share a context cancelled on shutdown
	bg, cancel := context.WithCancel(context.Background())
	defer cancel()
	go usage.Run(bg, acc, repo, time.Duration(cfg.RollupIntervalSec)*time.Second)
	go cleanup.Run(bg, repo, store,
		time.Duration(cfg.RetentionDays)*24*time.Hour,
		time.Duration(cfg.CleanupIntervalSec)*time.Second)

	srv := server.New(server.Deps{
		Store: store, Repo: repo, MaxUploadBytes: cfg.MaxUploadBytes,
		Usage: acc, Auth: authn, CORSOrigins: cfg.CORSAllowedOrigins,
		AuthMode:   cfg.AuthMode,
		OrgEnabled: cfg.AuthMode == "oidc" && cfg.OIDCOrgEnabled,
		Login:      loginHandler,
	})
	log.Printf("turbo-cache-forge listening on %s (backend=%s)", cfg.Addr, cfg.StorageBackend)
	if err := http.ListenAndServe(cfg.Addr, srv); err != nil {
		log.Fatal(err)
	}
}

func getenv(k string) string { return os.Getenv(k) }

func randomSecret() []byte {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		log.Fatalf("generate secret: %v", err)
	}
	return b
}
