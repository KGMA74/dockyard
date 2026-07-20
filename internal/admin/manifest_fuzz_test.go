package admin

import (
	"errors"
	"fmt"
	"testing"
)

// FuzzParseManifestDetails feeds arbitrary bytes through the single-manifest
// parsing path (parseManifestDetails with no manifest-list references) —
// this is exactly what a PUT /v2/.../manifests/<ref> body looks like from
// the server's point of view before it's ever validated. The only
// requirement is "never panic"; malformed input should just come back as an
// error.
func FuzzParseManifestDetails(f *testing.F) {
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"mediaType":"application/vnd.docker.distribution.manifest.v2+json","config":{"digest":"sha256:abc","size":10},"layers":[{"digest":"sha256:def","size":20}]}`))
	f.Add([]byte(`{"mediaType":"application/vnd.docker.distribution.manifest.list.v2+json","manifests":[{"digest":"sha256:abc","platform":{"architecture":"amd64","os":"linux"}}]}`))
	f.Add([]byte(`not json at all`))
	f.Add([]byte(`{"config":{"size":-1},"layers":[{"size":-1}]}`))
	f.Add([]byte(`null`))

	getBlob := func(string) ([]byte, error) { return nil, errors.New("blob not found") }
	getManifest := func(string) ([]byte, error) { return nil, errors.New("manifest not found") }

	f.Fuzz(func(t *testing.T, raw []byte) {
		_, _ = parseManifestDetails(raw, "sha256:fuzz", getBlob, getManifest)
	})
}

// FuzzDiffManifests exercises diffManifests with parsed output shapes built
// from arbitrary layer digest/size combinations, since it's normally fed
// the result of parseManifestDetails rather than raw bytes.
func FuzzDiffManifests(f *testing.F) {
	f.Add("sha256:a", int64(10), "sha256:b", int64(20))
	f.Add("", int64(0), "", int64(0))
	f.Add("sha256:same", int64(-5), "sha256:same", int64(-5))

	f.Fuzz(func(t *testing.T, digestA string, sizeA int64, digestB string, sizeB int64) {
		a := map[string]any{
			"total_size_bytes": sizeA,
			"layers":           []layerDetail{{Digest: digestA, SizeBytes: sizeA}},
		}
		b := map[string]any{
			"total_size_bytes": sizeB,
			"layers":           []layerDetail{{Digest: digestB, SizeBytes: sizeB}},
		}
		_ = diffManifests(a, b)
	})
}

// TestParseManifestDetailsCyclicManifestListsRejected reproduces the bug
// this fuzz target guards against: two manifest lists whose "manifests"
// entries reference each other by digest used to recurse into
// parseManifestDetails with no depth or visited-set bound, which is
// unbounded recursion (stack overflow) on any storage backend, since
// nothing stops a pusher from PUTting list B (referencing A's digest)
// before A exists, then PUTting A referencing B.
func TestParseManifestDetailsCyclicManifestListsRejected(t *testing.T) {
	const digestA = "sha256:" + "a000000000000000000000000000000000000000000000000000000000000"
	const digestB = "sha256:" + "b000000000000000000000000000000000000000000000000000000000000"

	manifestList := func(childDigest string) []byte {
		return fmt.Appendf(nil,
			`{"mediaType":"application/vnd.docker.distribution.manifest.list.v2+json","manifests":[{"digest":%q,"platform":{"architecture":"amd64","os":"linux"}}]}`,
			childDigest,
		)
	}
	rawA := manifestList(digestB)
	rawB := manifestList(digestA)

	getManifest := func(digest string) ([]byte, error) {
		switch digest {
		case digestA:
			return rawA, nil
		case digestB:
			return rawB, nil
		default:
			return nil, errors.New("not found")
		}
	}
	getBlob := func(string) ([]byte, error) { return nil, errors.New("not found") }

	// Must return (possibly with an error for the unresolved branch) instead
	// of recursing forever / overflowing the stack.
	result, err := parseManifestDetails(rawA, digestA, getBlob, getManifest)
	if err != nil {
		t.Fatalf("parseManifestDetails on the cyclic root: %v", err)
	}
	if result == nil {
		t.Fatal("expected a non-nil result for the root manifest list")
	}
}

func TestParseManifestDetailsDeepNestingRejected(t *testing.T) {
	// A chain of maxManifestListDepth+2 manifest lists, each pointing at the
	// next by digest — must terminate with an error, not run away.
	const depth = maxManifestListDepth + 2
	digests := make([]string, depth)
	for i := range digests {
		digests[i] = fmt.Sprintf("sha256:%064d", i)
	}
	manifests := make(map[string][]byte, depth)
	for i := 0; i < depth-1; i++ {
		manifests[digests[i]] = fmt.Appendf(nil,
			`{"mediaType":"application/vnd.docker.distribution.manifest.list.v2+json","manifests":[{"digest":%q,"platform":{"architecture":"amd64","os":"linux"}}]}`,
			digests[i+1],
		)
	}
	getManifest := func(digest string) ([]byte, error) {
		if raw, ok := manifests[digest]; ok {
			return raw, nil
		}
		return nil, errors.New("not found")
	}
	getBlob := func(string) ([]byte, error) { return nil, errors.New("not found") }

	result, err := parseManifestDetails(manifests[digests[0]], digests[0], getBlob, getManifest)
	if err != nil {
		t.Fatalf("parseManifestDetails on a deep chain: %v", err)
	}
	if result == nil {
		t.Fatal("expected a non-nil result for the root manifest list")
	}
}
