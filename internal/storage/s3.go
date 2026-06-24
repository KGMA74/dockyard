package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type S3Backend struct {
	client *minio.Client
	bucket string
}

func NewS3(endpoint, accessKey, secretKey, bucket, region string, secure bool) (*S3Backend, error) {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: secure,
		Region: region,
	})
	if err != nil {
		return nil, fmt.Errorf("s3 client init: %w", err)
	}

	ctx := context.Background()
	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		return nil, fmt.Errorf("check bucket: %w", err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: region}); err != nil {
			return nil, fmt.Errorf("create bucket: %w", err)
		}
	}

	return &S3Backend{client: client, bucket: bucket}, nil
}

func (s *S3Backend) PutBlob(digest string, content io.Reader, size int64) error {
	key := fmt.Sprintf("blobs/%s", digest)
	_, err := s.client.PutObject(
		context.Background(),
		s.bucket, key, content, size,
		minio.PutObjectOptions{ContentType: "application/octet-stream"},
	)
	return err
}

func (s *S3Backend) GetBlob(digest string) (io.ReadCloser, int64, error) {
	key := fmt.Sprintf("blobs/%s", digest)
	obj, err := s.client.GetObject(context.Background(), s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, 0, err
	}
	info, err := obj.Stat()
	if err != nil {
		obj.Close()
		return nil, 0, err
	}
	return obj, info.Size, nil
}

func (s *S3Backend) BlobExists(digest string) (bool, error) {
	key := fmt.Sprintf("blobs/%s", digest)
	_, err := s.client.StatObject(context.Background(), s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *S3Backend) DeleteBlob(digest string) error {
	key := fmt.Sprintf("blobs/%s", digest)
	return s.client.RemoveObject(context.Background(), s.bucket, key, minio.RemoveObjectOptions{})
}

func (s *S3Backend) InitUpload(uuid string) error {
	key := fmt.Sprintf("uploads/%s", uuid)
	_, err := s.client.PutObject(
		context.Background(),
		s.bucket, key,
		bytes.NewReader([]byte{}), 0,
		minio.PutObjectOptions{},
	)
	return err
}

// AppendUpload reads existing upload data, appends new content, and re-writes it.
// For large uploads, consider using S3 multipart uploads instead.
func (s *S3Backend) AppendUpload(uuid string, content io.Reader) error {
	key := fmt.Sprintf("uploads/%s", uuid)

	existing, err := s.client.GetObject(context.Background(), s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return err
	}
	defer existing.Close()

	existingData, err := io.ReadAll(existing)
	if err != nil {
		return err
	}

	newData, err := io.ReadAll(content)
	if err != nil {
		return err
	}

	combined := append(existingData, newData...)
	_, err = s.client.PutObject(
		context.Background(),
		s.bucket, key,
		bytes.NewReader(combined), int64(len(combined)),
		minio.PutObjectOptions{},
	)
	return err
}

func (s *S3Backend) CommitUpload(uuid, digest string) error {
	srcKey := fmt.Sprintf("uploads/%s", uuid)
	dstKey := fmt.Sprintf("blobs/%s", digest)

	src := minio.CopySrcOptions{Bucket: s.bucket, Object: srcKey}
	dst := minio.CopyDestOptions{Bucket: s.bucket, Object: dstKey}
	if _, err := s.client.CopyObject(context.Background(), dst, src); err != nil {
		return err
	}

	return s.client.RemoveObject(context.Background(), s.bucket, srcKey, minio.RemoveObjectOptions{})
}

func (s *S3Backend) DeleteUpload(uuid string) error {
	key := fmt.Sprintf("uploads/%s", uuid)
	return s.client.RemoveObject(context.Background(), s.bucket, key, minio.RemoveObjectOptions{})
}

func (s *S3Backend) GetUploadSize(uuid string) (int64, error) {
	key := fmt.Sprintf("uploads/%s", uuid)
	info, err := s.client.StatObject(context.Background(), s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return 0, err
	}
	return info.Size, nil
}

func (s *S3Backend) PutManifest(name, reference, digest string, content []byte) error {
	keys := []string{
		fmt.Sprintf("manifests/%s/%s", name, reference),
		fmt.Sprintf("manifests/%s/%s", name, digest),
	}
	for _, key := range keys {
		_, err := s.client.PutObject(
			context.Background(),
			s.bucket, key,
			bytes.NewReader(content), int64(len(content)),
			minio.PutObjectOptions{ContentType: "application/json"},
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *S3Backend) GetManifest(name, reference string) ([]byte, string, error) {
	key := fmt.Sprintf("manifests/%s/%s", name, reference)
	obj, err := s.client.GetObject(context.Background(), s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, "", err
	}
	defer obj.Close()

	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, "", err
	}

	h := sha256.Sum256(data)
	digest := fmt.Sprintf("sha256:%x", h)
	return data, digest, nil
}

func (s *S3Backend) DeleteManifest(name, digest string) error {
	ctx := context.Background()
	prefix := fmt.Sprintf("manifests/%s/", name)

	for obj := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	}) {
		if obj.Err != nil {
			continue
		}
		ref := strings.TrimPrefix(obj.Key, prefix)
		data, _, err := s.GetManifest(name, ref)
		if err != nil {
			continue
		}
		h := sha256.Sum256(data)
		if fmt.Sprintf("sha256:%x", h) == digest {
			s.client.RemoveObject(ctx, s.bucket, obj.Key, minio.RemoveObjectOptions{})
		}
	}
	return nil
}

func (s *S3Backend) ManifestExists(name, reference string) (bool, error) {
	key := fmt.Sprintf("manifests/%s/%s", name, reference)
	_, err := s.client.StatObject(context.Background(), s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *S3Backend) ListRepositories() ([]string, error) {
	ctx := context.Background()
	seen := make(map[string]bool)
	var repos []string

	for obj := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
		Prefix:    "manifests/",
		Recursive: true,
	}) {
		if obj.Err != nil {
			continue
		}
		parts := strings.SplitN(strings.TrimPrefix(obj.Key, "manifests/"), "/", 2)
		if len(parts) >= 1 && parts[0] != "" && !seen[parts[0]] {
			seen[parts[0]] = true
			repos = append(repos, parts[0])
		}
	}
	return repos, nil
}

func (s *S3Backend) ListTags(name string) ([]string, error) {
	ctx := context.Background()
	prefix := fmt.Sprintf("manifests/%s/", name)
	var tags []string

	for obj := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: false,
	}) {
		if obj.Err != nil {
			continue
		}
		tag := strings.TrimPrefix(obj.Key, prefix)
		if tag != "" && !strings.HasPrefix(tag, "sha256:") {
			tags = append(tags, tag)
		}
	}
	return tags, nil
}

func (s *S3Backend) Stats() (StorageStats, error) {
	ctx := context.Background()
	var stats StorageStats

	for obj := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
		Recursive: true,
	}) {
		if obj.Err != nil {
			continue
		}
		stats.TotalSize += obj.Size
		if strings.HasPrefix(obj.Key, "blobs/") {
			stats.BlobCount++
		}
	}

	repos, err := s.ListRepositories()
	if err == nil {
		stats.RepoCount = len(repos)
	}

	return stats, nil
}
