package storage

import "dockyard/internal/reference"

// checkManifestTarget validates the (repository, tag-or-digest) pair every
// manifest operation receives before either one is turned into a path or an
// object key.
func checkManifestTarget(name, ref string) error {
	if err := reference.CheckName(name); err != nil {
		return err
	}
	return reference.CheckReference(ref)
}
