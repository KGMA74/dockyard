package storage

import (
	"fmt"

	"maestro/config"
)

func NewBackend(cfg *config.Config) (Backend, error) {
	switch cfg.StorageBackend {
	case config.StorageLocal:
		return NewLocal(cfg.StoragePath)

	case config.StorageS3:
		if cfg.S3Endpoint == "" {
			return nil, fmt.Errorf("S3_ENDPOINT is required when REGISTRY_STORAGE_BACKEND=s3")
		}
		if cfg.S3AccessKey == "" || cfg.S3SecretKey == "" {
			return nil, fmt.Errorf("S3_ACCESS_KEY and S3_SECRET_KEY are required")
		}
		if cfg.S3Bucket == "" {
			return nil, fmt.Errorf("S3_BUCKET is required")
		}
		return NewS3(cfg.S3Endpoint, cfg.S3AccessKey, cfg.S3SecretKey, cfg.S3Bucket, cfg.S3Region, cfg.S3Secure)

	default:
		return nil, fmt.Errorf("unknown storage backend: %q (valid: local, s3)", cfg.StorageBackend)
	}
}
