package storage_test

import (
	"fmt"
	"os"
	"testing"
	"time"

	"dockyard/internal/storage"
	"dockyard/internal/storage/storagetest"
)

func TestLocalBackendContract(t *testing.T) {
	storagetest.RunBackendContract(t, func(t *testing.T) storage.Backend {
		b, err := storage.NewLocal(t.TempDir())
		if err != nil {
			t.Fatalf("NewLocal: %v", err)
		}
		return b
	})
}

// TestS3BackendContract runs the same contract against a real S3/MinIO server.
// Skipped unless DOCKYARD_TEST_S3_ENDPOINT is set, e.g.:
//
//	DOCKYARD_TEST_S3_ENDPOINT=localhost:9000 \
//	DOCKYARD_TEST_S3_ACCESS_KEY=minioadmin \
//	DOCKYARD_TEST_S3_SECRET_KEY=minioadmin \
//	go test ./internal/storage/...
func TestS3BackendContract(t *testing.T) {
	endpoint := os.Getenv("DOCKYARD_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("DOCKYARD_TEST_S3_ENDPOINT not set")
	}
	accessKey := os.Getenv("DOCKYARD_TEST_S3_ACCESS_KEY")
	secretKey := os.Getenv("DOCKYARD_TEST_S3_SECRET_KEY")

	storagetest.RunBackendContract(t, func(t *testing.T) storage.Backend {
		bucket := fmt.Sprintf("dockyard-test-%d", time.Now().UnixNano())
		b, err := storage.NewS3(endpoint, accessKey, secretKey, bucket, "us-east-1", false)
		if err != nil {
			t.Fatalf("NewS3: %v", err)
		}
		return b
	})
}
