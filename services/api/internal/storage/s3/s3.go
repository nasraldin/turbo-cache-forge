package s3

import (
	"context"
	"errors"
	"io"

	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/storage"
)

type Config struct {
	Bucket, Endpoint, Region, AccessKey, SecretKey string
}

type Store struct {
	bucket   string
	client   *awss3.Client
	uploader *manager.Uploader
}

// credentialOptions returns a static-credentials load option only when at
// least one key is non-empty. When both are empty, New falls back to the
// SDK's default credential chain (env vars, shared config, EC2/ECS IAM
// role) — required for AWS deployments that don't hand out static keys.
func credentialOptions(accessKey, secretKey string) []func(*awscfg.LoadOptions) error {
	if accessKey == "" && secretKey == "" {
		return nil
	}
	return []func(*awscfg.LoadOptions) error{
		awscfg.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
	}
}

func New(ctx context.Context, cfg Config) (*Store, error) {
	opts := append([]func(*awscfg.LoadOptions) error{awscfg.WithRegion(cfg.Region)},
		credentialOptions(cfg.AccessKey, cfg.SecretKey)...)
	loaded, err := awscfg.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, err
	}
	client := awss3.NewFromConfig(loaded, func(o *awss3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = &cfg.Endpoint // R2 / MinIO
			o.UsePathStyle = true
		}
	})
	return &Store{bucket: cfg.Bucket, client: client, uploader: manager.NewUploader(client)}, nil
}

func contentLength(cl *int64) int64 {
	if cl == nil {
		return 0
	}
	return *cl
}

func (s *Store) Put(ctx context.Context, key string, r io.Reader) error {
	_, err := s.uploader.Upload(ctx, &awss3.PutObjectInput{
		Bucket: &s.bucket, Key: &key, Body: r, // streaming multipart, no full buffer
	})
	return err
}

func (s *Store) Get(ctx context.Context, key string) (io.ReadCloser, *storage.ObjectInfo, error) {
	out, err := s.client.GetObject(ctx, &awss3.GetObjectInput{Bucket: &s.bucket, Key: &key})
	if isNotFound(err) {
		return nil, nil, storage.ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	size := contentLength(out.ContentLength)
	return out.Body, &storage.ObjectInfo{Size: size}, nil
}

func (s *Store) Head(ctx context.Context, key string) (*storage.ObjectInfo, error) {
	out, err := s.client.HeadObject(ctx, &awss3.HeadObjectInput{Bucket: &s.bucket, Key: &key})
	if isNotFound(err) {
		return nil, storage.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	size := contentLength(out.ContentLength)
	return &storage.ObjectInfo{Size: size}, nil
}

func (s *Store) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &awss3.DeleteObjectInput{Bucket: &s.bucket, Key: &key})
	return err
}

func isNotFound(err error) bool {
	var nsk *types.NoSuchKey
	var nf *types.NotFound
	return errors.As(err, &nsk) || errors.As(err, &nf)
}
