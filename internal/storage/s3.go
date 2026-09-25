package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
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

// getObjectBytes reads a whole object into memory. Used for manifests, which
// are small.
func (s *S3Backend) getObjectBytes(ctx context.Context, key string) ([]byte, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = obj.Close() }()
	return io.ReadAll(obj)
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

func (s *S3Backend) DeleteManifest(name, reference string) error {
	ctx := context.Background()

	// Resolve a tag reference to its digest first — otherwise the content-hash
	// comparison below never matches and the delete is a silent no-op.
	digest := reference
	if !strings.HasPrefix(reference, "sha256:") {
		_, d, err := s.GetManifest(name, reference)
		if err != nil {
			return err
		}
		digest = d
	}

	// Read the manifest before deleting it so we can prune now-orphaned child
	// manifests if it turns out to be an index / manifest list.
	indexRaw, _, _ := s.GetManifest(name, digest)

	prefix := fmt.Sprintf("manifests/%s/", name)
	for obj := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	}) {
		if obj.Err != nil {
			return obj.Err
		}
		ref := strings.TrimPrefix(obj.Key, prefix)
		data, _, err := s.GetManifest(name, ref)
		if err != nil {
			continue
		}
		h := sha256.Sum256(data)
		if fmt.Sprintf("sha256:%x", h) == digest {
			if err := s.client.RemoveObject(ctx, s.bucket, obj.Key, minio.RemoveObjectOptions{}); err != nil {
				return err
			}
		}
	}

	if len(indexRaw) > 0 {
		s.pruneOrphanChildManifests(ctx, name, indexRaw)
	}
	return nil
}

// pruneOrphanChildManifests removes the per-platform child manifest objects of a
// just-deleted index when nothing else in the repository still references them.
// Without this, deleting a multi-arch tag leaves the child manifests behind and
// GC keeps treating every layer they list as referenced.
func (s *S3Backend) pruneOrphanChildManifests(ctx context.Context, name string, deletedIndexRaw []byte) {
	children := childManifestDigests(deletedIndexRaw)
	if len(children) == 0 {
		return
	}
	prefix := fmt.Sprintf("manifests/%s/", name)
	stillReferenced := make(map[string]struct{})
	for obj := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if obj.Err != nil {
			return // be conservative: on a list error, prune nothing
		}
		data, err := s.getObjectBytes(ctx, obj.Key)
		if err != nil {
			continue
		}
		for _, d := range childManifestDigests(data) {
			stillReferenced[d] = struct{}{}
		}
	}
	for _, child := range children {
		if _, ok := stillReferenced[child]; ok {
			continue
		}
		_ = s.client.RemoveObject(ctx, s.bucket, fmt.Sprintf("manifests/%s/%s", name, child), minio.RemoveObjectOptions{})
	}
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
			return nil, obj.Err
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
			return nil, obj.Err
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
			return obj.Err
		}
		found = true
		if err := s.client.RemoveObject(ctx, s.bucket, obj.Key, minio.RemoveObjectOptions{}); err != nil {
			return err
		}
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
			return nil, obj.Err
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
			return nil, obj.Err
		}
		// Only read digest keys (sha256:…) to avoid processing each manifest twice.
		parts := strings.Split(obj.Key, "/")
		last := parts[len(parts)-1]
		if !strings.HasPrefix(last, "sha256:") {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(obj.Key, "manifests/"), "/"+last)
		data, err := s.getObjectBytes(ctx, obj.Key)
		if err != nil {
			continue
		}
		fetchChild := func(digest string) ([]byte, error) {
			return s.getObjectBytes(ctx, fmt.Sprintf("manifests/%s/%s", name, digest))
		}
		collectBlobRefs(data, referenced, fetchChild)
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
			return StorageStats{}, obj.Err
		}
		stats.TotalSize += obj.Size
		stats.BlobCount++
	}

	repos, err := s.ListRepositories()
	if err != nil {
		return StorageStats{}, err
	}
	stats.RepoCount = len(repos)

	return stats, nil
}
