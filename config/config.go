package config

import (
	"os"
	"strconv"

	_ "github.com/joho/godotenv/autoload"
)

type StorageBackend string

const (
	StorageLocal StorageBackend = "local"
	StorageS3    StorageBackend = "s3"
)

type Config struct {
	Port int

	RegistryMode string

	StoragePath string

	RegistryURL      string
	RegistryUsername string
	RegistryPassword string

	StorageBackend StorageBackend

	S3Endpoint  string
	S3AccessKey string
	S3SecretKey string
	S3Bucket    string
	S3Region    string
	S3Secure    bool

	AuthUsername      string
	AuthPassword      string
	JWTSecret         string
	JWTSecretPrevious string
	V2AuthEnabled     bool
}

func Load() *Config {
	port, _ := strconv.Atoi(getEnv("PORT", "8080"))
	return &Config{
		Port:             port,
		RegistryMode:     getEnv("REGISTRY_MODE", "embedded"),
		StoragePath:      getEnv("REGISTRY_STORAGE_PATH", "./data/registry"),
		RegistryURL:      getEnv("REGISTRY_URL", ""),
		RegistryUsername: getEnv("REGISTRY_USERNAME", ""),
		RegistryPassword: getEnv("REGISTRY_PASSWORD", ""),
		StorageBackend:   StorageBackend(getEnv("REGISTRY_STORAGE_BACKEND", "local")),
		S3Endpoint:       getEnv("S3_ENDPOINT", ""),
		S3AccessKey:      getEnv("S3_ACCESS_KEY", ""),
		S3SecretKey:      getEnv("S3_SECRET_KEY", ""),
		S3Bucket:         getEnv("S3_BUCKET", "dockyard-registry"),
		S3Region:         getEnv("S3_REGION", "us-east-1"),
		S3Secure:         getEnv("S3_SECURE", "false") == "true",
		AuthUsername: getEnv("AUTH_USERNAME", ""),
		AuthPassword: getEnv("AUTH_PASSWORD", ""),
		JWTSecret:    getEnv("JWT_SECRET", ""),
		// Rotation: set JWT_SECRET to the new value and JWT_SECRET_PREVIOUS to
		// the old one; tokens signed with either verify until the grace window
		// ends (remove JWT_SECRET_PREVIOUS afterwards).
		JWTSecretPrevious: getEnv("JWT_SECRET_PREVIOUS", ""),
		V2AuthEnabled:     getEnv("V2_AUTH_ENABLED", "false") == "true",
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
