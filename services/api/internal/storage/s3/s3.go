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

func New(ctx context.Context, cfg Config) (*Store, error) {
	loaded, err := awscfg.LoadDefaultConfig(ctx,
		awscfg.WithRegion(cfg.Region),
		awscfg.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, "")),
	)
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
	var size int64
	if out.ContentLength != nil {
		size = *out.ContentLength
	}
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
	var size int64
	if out.ContentLength != nil {
		size = *out.ContentLength
	}
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
