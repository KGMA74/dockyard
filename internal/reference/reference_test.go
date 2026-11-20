package reference

import (
	"strings"
	"testing"
)

func TestValidName(t *testing.T) {
	valid := []string{
		"alpine",
		"library/alpine",
		"org/sub/image",
		"my-org/my.image_name",
		"a--b/c__d",
		"MixedCase/Image", // tolerated for repositories created before validation existed
		strings.Repeat("a", 255),
	}
	for _, name := range valid {
		if !ValidName(name) {
			t.Errorf("ValidName(%q) = false, want true", name)
		}
	}

	invalid := []string{
		"",
		".",
		"..",
		"../etc",
		"team-a/../team-b/app",
		"team-a/./app",
		"/leading",
		"trailing/",
		"double//slash",
		`back\slash`,
		`team-a\..\team-b`,
		"-leading-dash",
		"trailing-dash-",
		".hidden",
		"with space",
		"with:colon",
		"null\x00byte",
		strings.Repeat("a", 256),
	}
	for _, name := range invalid {
		if ValidName(name) {
			t.Errorf("ValidName(%q) = true, want false", name)
		}
	}
}

func TestValidTag(t *testing.T) {
	valid := []string{
		"latest", "v1.2.3", "1.0", "_internal", "A-b_c.d",
		"sha256-" + strings.Repeat("a", 64) + ".sig", // cosign signature tag
		strings.Repeat("t", 128),
	}
	for _, tag := range valid {
		if !ValidTag(tag) {
			t.Errorf("ValidTag(%q) = false, want true", tag)
		}
	}

	invalid := []string{
		"", ".", "..", ".hidden", "-dash", "a/b", `a\b`, "a:b", "with space",
		strings.Repeat("t", 129),
	}
	for _, tag := range invalid {
		if ValidTag(tag) {
			t.Errorf("ValidTag(%q) = true, want false", tag)
		}
	}
}

func TestValidDigest(t *testing.T) {
	good := "sha256:" + strings.Repeat("0123456789abcdef", 4)
	if !ValidDigest(good) {
		t.Fatalf("ValidDigest(%q) = false, want true", good)
	}
	invalid := []string{
		"",
		"sha256:",
		"sha256:a", // used to panic in blobPath (d[:2] on a 1-char string)
		"sha256:" + strings.Repeat("a", 63),
		"sha256:" + strings.Repeat("a", 65),
		"sha256:" + strings.Repeat("A", 64), // digests are lowercase hex
		"sha256:../../../../etc/passwd",
		"sha512:" + strings.Repeat("a", 64),
		strings.Repeat("a", 64),
	}
	for _, d := range invalid {
		if ValidDigest(d) {
			t.Errorf("ValidDigest(%q) = true, want false", d)
		}
	}
}

func TestValidReference(t *testing.T) {
	if !ValidReference("latest") {
		t.Error("tag reference rejected")
	}
	if !ValidReference("sha256:" + strings.Repeat("a", 64)) {
		t.Error("digest reference rejected")
	}
	// Anything starting with "sha256:" must be a full digest — never a tag.
	if ValidReference("sha256:../x") {
		t.Error("malformed digest accepted as a reference")
	}
}

func TestValidUploadID(t *testing.T) {
	for _, id := range []string{"0123456789abcdef0123456789abcdef", "upload-1", "a_b"} {
		if !ValidUploadID(id) {
			t.Errorf("ValidUploadID(%q) = false, want true", id)
		}
	}
	for _, id := range []string{"", ".", "..", "a/b", `a\b`, "a.b", strings.Repeat("a", 65)} {
		if ValidUploadID(id) {
			t.Errorf("ValidUploadID(%q) = true, want false", id)
		}
	}
}
