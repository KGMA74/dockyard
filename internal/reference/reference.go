// Package reference validates the identifiers a registry client controls:
// repository names, tags, digests and upload session IDs.
//
// Every one of them ends up in a filesystem path or an object key
// (repositories/<name>/tags/<tag>, blobs/sha256/<digest>, uploads/<id>), and
// repository names are also what RBAC patterns are matched against. Accepting
// them unchecked let "team-a/../team-b/app" satisfy the pattern "team-a/*"
// while resolving to another team's repository, and let ".." segments escape
// the storage root altogether. Validation therefore happens twice: at the
// HTTP edge (clean 4xx) and again inside the storage backends (defense in
// depth for internal callers such as import, replication and the mirror).
//
// The package has no dependencies so auth, v2, admin and storage can all
// import it without cycles.
package reference

import (
	"errors"
	"regexp"
	"strings"
)

var (
	ErrInvalidName     = errors.New("invalid repository name")
	ErrInvalidTag      = errors.New("invalid tag")
	ErrInvalidDigest   = errors.New("invalid digest")
	ErrInvalidUploadID = errors.New("invalid upload id")
)

const maxNameLength = 255

var (
	// One path component: alphanumerics separated by '.', '_' or '-' runs.
	// This is the distribution-spec grammar, widened to accept uppercase so
	// repositories created by older, laxer versions stay reachable.
	reComponent = regexp.MustCompile(`^[A-Za-z0-9]+(?:[._-]+[A-Za-z0-9]+)*$`)
	reTag       = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$`)
	reDigest    = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	reUploadID  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
)

// ValidName reports whether name is a well-formed repository name: one or
// more slash-separated components, no empty, "." or ".." component, no
// backslash, no leading or trailing slash.
func ValidName(name string) bool {
	if name == "" || len(name) > maxNameLength {
		return false
	}
	for component := range strings.SplitSeq(name, "/") {
		if !reComponent.MatchString(component) {
			return false
		}
	}
	return true
}

// ValidTag reports whether tag is a well-formed tag (max 128 chars).
func ValidTag(tag string) bool { return reTag.MatchString(tag) }

// ValidDigest reports whether digest is a canonical sha256 digest.
func ValidDigest(digest string) bool { return reDigest.MatchString(digest) }

// IsDigest reports whether ref is shaped like a digest rather than a tag.
// Tags cannot contain ':', so the prefix alone is unambiguous.
func IsDigest(ref string) bool { return strings.HasPrefix(ref, "sha256:") }

// ValidReference reports whether ref is a valid tag or a valid digest.
func ValidReference(ref string) bool {
	if IsDigest(ref) {
		return ValidDigest(ref)
	}
	return ValidTag(ref)
}

// ValidUploadID reports whether id is a safe upload session identifier.
func ValidUploadID(id string) bool { return reUploadID.MatchString(id) }

// CheckName, CheckTag, CheckDigest, CheckReference and CheckUploadID return
// the matching sentinel error (wrapped by callers as needed) or nil.

func CheckName(name string) error {
	if !ValidName(name) {
		return ErrInvalidName
	}
	return nil
}

func CheckTag(tag string) error {
	if !ValidTag(tag) {
		return ErrInvalidTag
	}
	return nil
}

func CheckDigest(digest string) error {
	if !ValidDigest(digest) {
		return ErrInvalidDigest
	}
	return nil
}

func CheckReference(ref string) error {
	if IsDigest(ref) {
		return CheckDigest(ref)
	}
	return CheckTag(ref)
}

func CheckUploadID(id string) error {
	if !ValidUploadID(id) {
		return ErrInvalidUploadID
	}
	return nil
}
