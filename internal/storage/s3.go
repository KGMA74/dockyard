package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

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
	ctx := context.Background()
	key := fmt.Sprintf("blobs/%s", digest)
	h := sha256.New()
	_, err := s.client.PutObject(
		ctx,
		s.bucket, key, io.TeeReader(content, h), size,
		minio.PutObjectOptions{ContentType: "application/octet-stream", PartSize: uploadPartSize},
	)
	if err != nil {
		return err
	}
	if got := fmt.Sprintf("sha256:%x", h.Sum(nil)); got != digest {
		_ = s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
		return fmt.Errorf("digest mismatch: expected %s got %s", digest, got)
	}
	return nil
}

func (s *S3Backend) GetBlob(digest string) (io.ReadCloser, int64, error) {
	key := fmt.Sprintf("blobs/%s", digest)
	obj, err := s.client.GetObject(context.Background(), s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, 0, err
	}
	info, err := obj.Stat()
	if err != nil {
		_ = obj.Close()
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

// Chunked uploads are stored as one object per appended chunk under
// uploads/<uuid>/parts/<n>; the commit streams the parts into the final blob.
// Memory stays O(upload part size) end to end — the previous implementation
// re-read and re-uploaded the whole object on every append.

const uploadPartSize = 16 << 20 // minio-go buffer per streamed part

func (s *S3Backend) uploadMarkerKey(uuid string) string {
	return fmt.Sprintf("uploads/%s/.init", uuid)
}

func (s *S3Backend) uploadPartsPrefix(uuid string) string {
	return fmt.Sprintf("uploads/%s/parts/", uuid)
}

func (s *S3Backend) InitUpload(uuid string) error {
	_, err := s.client.PutObject(
		context.Background(),
		s.bucket, s.uploadMarkerKey(uuid),
		bytes.NewReader(nil), 0,
		minio.PutObjectOptions{},
	)
	return err
}

// listUploadParts returns the upload's part objects in append order (keys are
// zero-padded so lexicographic listing order is chronological).
func (s *S3Backend) listUploadParts(uuid string) ([]minio.ObjectInfo, error) {
	var parts []minio.ObjectInfo
	for obj := range s.client.ListObjects(context.Background(), s.bucket, minio.ListObjectsOptions{
		Prefix:    s.uploadPartsPrefix(uuid),
		Recursive: true,
	}) {
		if obj.Err != nil {
			return nil, obj.Err
		}
		parts = append(parts, obj)
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].Key < parts[j].Key })
	return parts, nil
}

func (s *S3Backend) statUpload(uuid string) ([]minio.ObjectInfo, error) {
	if _, err := s.client.StatObject(context.Background(), s.bucket, s.uploadMarkerKey(uuid), minio.StatObjectOptions{}); err != nil {
		return nil, fmt.Errorf("upload %s not found", uuid)
	}
	return s.listUploadParts(uuid)
}

// AppendUpload streams the chunk into its own part object — no read-back of
// previously uploaded data.
func (s *S3Backend) AppendUpload(uuid string, content io.Reader) error {
	parts, err := s.statUpload(uuid)
	if err != nil {
		return err
	}
	key := fmt.Sprintf("%s%08d", s.uploadPartsPrefix(uuid), len(parts)+1)
	_, err = s.client.PutObject(
		context.Background(),
		s.bucket, key,
		content, -1,
		minio.PutObjectOptions{PartSize: uploadPartSize},
	)
	return err
}

// CommitUpload streams the concatenated parts into the final blob while
// hashing them, verifies the digest, then drops the upload session.
func (s *S3Backend) CommitUpload(uuid, digest string) error {
	ctx := context.Background()
	parts, err := s.statUpload(uuid)
	if err != nil {
		return err
	}

	readers := make([]io.Reader, 0, len(parts))
	closers := make([]io.Closer, 0, len(parts))
	defer func() {
		for _, c := range closers {
			_ = c.Close()
		}
	}()
	var total int64
	for _, p := range parts {
		obj, err := s.client.GetObject(ctx, s.bucket, p.Key, minio.GetObjectOptions{})
		if err != nil {
			return err
		}
		readers = append(readers, obj)
		closers = append(closers, obj)
		total += p.Size
	}

	dstKey := fmt.Sprintf("blobs/%s", digest)
	h := sha256.New()
	_, err = s.client.PutObject(
		ctx,
		s.bucket, dstKey,
		io.TeeReader(io.MultiReader(readers...), h), total,
		minio.PutObjectOptions{PartSize: uploadPartSize},
	)
	if err != nil {
		return err
	}
	if got := fmt.Sprintf("sha256:%x", h.Sum(nil)); got != digest {
		_ = s.client.RemoveObject(ctx, s.bucket, dstKey, minio.RemoveObjectOptions{})
		return fmt.Errorf("digest mismatch: expected %s got %s", digest, got)
	}
	return s.DeleteUpload(uuid)
}

// DeleteUpload removes the marker and every part of the session.
func (s *S3Backend) DeleteUpload(uuid string) error {
	ctx := context.Background()
	for obj := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
		Prefix:    fmt.Sprintf("uploads/%s/", uuid),
		Recursive: true,
	}) {
		if obj.Err != nil {
			continue
		}
		_ = s.client.RemoveObject(ctx, s.bucket, obj.Key, minio.RemoveObjectOptions{})
	}
	return nil
}

func (s *S3Backend) GetUploadSize(uuid string) (int64, error) {
	parts, err := s.statUpload(uuid)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, p := range parts {
		total += p.Size
	}
	return total, nil
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
	defer func() { _ = obj.Close() }()

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
			_ = s.client.RemoveObject(ctx, s.bucket, obj.Key, minio.RemoveObjectOptions{})
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
		// The key is "manifests/<name>/<reference>". <name> can itself contain
		// slashes (org/image), but <reference> (a tag or a sha256: digest) never
		// does — so the name is everything before the LAST slash, not the first.
		trimmed := strings.TrimPrefix(obj.Key, "manifests/")
		idx := strings.LastIndex(trimmed, "/")
		if idx <= 0 {
			continue
		}
		name := trimmed[:idx]
		if !seen[name] {
			seen[name] = true
			repos = append(repos, name)
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

func (s *S3Backend) TagPushedAt(name, tag string) (time.Time, error) {
	key := fmt.Sprintf("manifests/%s/%s", name, tag)
	info, err := s.client.StatObject(context.Background(), s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return time.Time{}, err
	}
	return info.LastModified, nil
}

// DeleteRepository removes every manifest and tag object under the repository.
// Blobs stay in the bucket until the next GC run, like manifest deletion.
func (s *S3Backend) DeleteRepository(name string) error {
	ctx := context.Background()
	prefix := fmt.Sprintf("manifests/%s/", name)
	found := false
	for obj := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	}) {
		if obj.Err != nil {
			continue
		}
		found = true
		_ = s.client.RemoveObject(ctx, s.bucket, obj.Key, minio.RemoveObjectOptions{})
	}
	if !found {
		return fmt.Errorf("repository %q not found", name)
	}
	return nil
}

// ── GC helpers (mirrors LocalBackend, enables GC in S3 mode) ─────────────────

func (s *S3Backend) AllBlobs() ([]string, error) {
	ctx := context.Background()
	var blobs []string
	for obj := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
		Prefix:    "blobs/",
		Recursive: true,
	}) {
		if obj.Err != nil {
			continue
		}
		digest := strings.TrimPrefix(obj.Key, "blobs/")
		if digest != "" {
			blobs = append(blobs, digest)
		}
	}
	return blobs, nil
}

func (s *S3Backend) BlobSize(digest string) (int64, error) {
	key := fmt.Sprintf("blobs/%s", digest)
	info, err := s.client.StatObject(context.Background(), s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return 0, err
	}
	return info.Size, nil
}

func (s *S3Backend) ReferencedBlobs() (map[string]struct{}, error) {
	ctx := context.Background()
	referenced := make(map[string]struct{})

	for obj := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
		Prefix:    "manifests/",
		Recursive: true,
	}) {
		if obj.Err != nil {
			continue
		}
		// Only read digest keys (sha256:…) to avoid processing each manifest twice
		parts := strings.Split(obj.Key, "/")
		if !strings.HasPrefix(parts[len(parts)-1], "sha256:") {
			continue
		}
		reader, err := s.client.GetObject(ctx, s.bucket, obj.Key, minio.GetObjectOptions{})
		if err != nil {
			continue
		}
		data, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			continue
		}
		var m struct {
			Config struct {
				Digest string `json:"digest"`
			} `json:"config"`
			Layers []struct {
				Digest string `json:"digest"`
			} `json:"layers"`
		}
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		if m.Config.Digest != "" {
			referenced[m.Config.Digest] = struct{}{}
		}
		for _, l := range m.Layers {
			if l.Digest != "" {
				referenced[l.Digest] = struct{}{}
			}
		}
	}
	return referenced, nil
}

func (s *S3Backend) RemoveBlob(digest string) error {
	key := fmt.Sprintf("blobs/%s", digest)
	return s.client.RemoveObject(context.Background(), s.bucket, key, minio.RemoveObjectOptions{})
}

func (s *S3Backend) Stats() (StorageStats, error) {
	ctx := context.Background()
	var stats StorageStats

	// Only blobs count toward storage stats — manifests are tiny and upload
	// sessions are transient (this also matches LocalBackend's behavior).
	for obj := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
		Prefix:    "blobs/",
		Recursive: true,
	}) {
		if obj.Err != nil {
			continue
		}
		stats.TotalSize += obj.Size
		stats.BlobCount++
	}

	repos, err := s.ListRepositories()
	if err == nil {
		stats.RepoCount = len(repos)
	}

	return stats, nil
}
