package reference

import "regexp"

// V2Route identifies which Registry V2 endpoint a path addresses.
type V2Route int

const (
	RouteUnknown    V2Route = iota // not a recognised V2 endpoint
	RouteBase                      // /v2/ — API version check
	RouteCatalog                   // /v2/_catalog
	RouteTags                      // /v2/<name>/tags/list
	RouteManifest                  // /v2/<name>/manifests/<tag-or-digest>
	RouteBlob                      // /v2/<name>/blobs/<digest>
	RouteUploadInit                // /v2/<name>/blobs/uploads/
	RouteUpload                    // /v2/<name>/blobs/uploads/<id>
)

// V2Path is a parsed Registry V2 request path. Ref holds the tag or digest of
// a manifest, the digest of a blob, or the id of an upload session.
type V2Path struct {
	Route V2Route
	Name  string
	Ref   string
}

// Image names may contain slashes (org/image, org/sub/image), which rules out
// a parameter router: the repository is whatever precedes the last keyword.
var (
	reV2Tags       = regexp.MustCompile(`^/v2/(.+)/tags/list$`)
	reV2Manifest   = regexp.MustCompile(`^/v2/(.+)/manifests/([^/]+)$`)
	reV2Blob       = regexp.MustCompile(`^/v2/(.+)/blobs/(sha256:[^/]+)$`)
	reV2UploadInit = regexp.MustCompile(`^/v2/(.+)/blobs/uploads/$`)
	reV2Upload     = regexp.MustCompile(`^/v2/(.+)/blobs/uploads/([^/]+)$`)
)

// ParseV2Path is the single place a V2 path is split into route, repository
// and reference. The auth middleware (which decides *whether* a request may
// touch a repository) and the handlers (which decide *which* repository is
// touched) both call it, so the two can never disagree about the repository a
// path names — a disagreement there is an authorization bypass.
func ParseV2Path(path string) V2Path {
	switch path {
	case "/v2", "/v2/":
		return V2Path{Route: RouteBase}
	case "/v2/_catalog":
		return V2Path{Route: RouteCatalog}
	}
	if m := reV2Tags.FindStringSubmatch(path); m != nil {
		return V2Path{Route: RouteTags, Name: m[1]}
	}
	if m := reV2Manifest.FindStringSubmatch(path); m != nil {
		return V2Path{Route: RouteManifest, Name: m[1], Ref: m[2]}
	}
	if m := reV2Blob.FindStringSubmatch(path); m != nil {
		return V2Path{Route: RouteBlob, Name: m[1], Ref: m[2]}
	}
	if m := reV2UploadInit.FindStringSubmatch(path); m != nil {
		return V2Path{Route: RouteUploadInit, Name: m[1]}
	}
	if m := reV2Upload.FindStringSubmatch(path); m != nil {
		return V2Path{Route: RouteUpload, Name: m[1], Ref: m[2]}
	}
	return V2Path{Route: RouteUnknown}
}

// Validate checks the client-controlled parts of the path. It returns one of
// the package's sentinel errors, or nil.
func (p V2Path) Validate() error {
	switch p.Route {
	case RouteTags, RouteUploadInit:
		return CheckName(p.Name)
	case RouteManifest:
		if err := CheckName(p.Name); err != nil {
			return err
		}
		return CheckReference(p.Ref)
	case RouteBlob:
		if err := CheckName(p.Name); err != nil {
			return err
		}
		return CheckDigest(p.Ref)
	case RouteUpload:
		if err := CheckName(p.Name); err != nil {
			return err
		}
		return CheckUploadID(p.Ref)
	}
	return nil
}

// V2ErrorCode maps a validation error to its Registry V2 error code.
func V2ErrorCode(err error) string {
	switch err {
	case ErrInvalidName:
		return "NAME_INVALID"
	case ErrInvalidTag:
		return "TAG_INVALID"
	case ErrInvalidDigest:
		return "DIGEST_INVALID"
	case ErrInvalidUploadID:
		return "BLOB_UPLOAD_INVALID"
	}
	return "UNSUPPORTED"
}
