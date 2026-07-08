package s3

import (
	"context"
	"os"
	"testing"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/storage"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/storage/storagetest"
)

// Set S3_TEST_ENDPOINT (e.g. http://localhost:9000) + creds to run against MinIO.
func TestS3Conformance(t *testing.T) {
	endpoint := os.Getenv("S3_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("set S3_TEST_ENDPOINT to run S3 conformance tests")
	}
	storagetest.Run(t, func() storage.Storage {
		s, err := New(context.Background(), Config{
			Bucket:    os.Getenv("S3_TEST_BUCKET"),
			Endpoint:  endpoint,
			Region:    "auto",
			AccessKey: os.Getenv("S3_TEST_ACCESS_KEY"),
			SecretKey: os.Getenv("S3_TEST_SECRET_KEY"),
		})
		if err != nil {
			t.Fatal(err)
		}
		return s
	})
}
