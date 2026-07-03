// Package version holds the build-time version string, injected via
// -ldflags "-X dockyard/internal/version.Version=vX.Y.Z" (see Makefile and Dockerfile).
package version

// Version defaults to "dev" for local builds that don't inject it.
var Version = "dev"
