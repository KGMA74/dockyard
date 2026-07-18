//go:build !windows

package server

import "golang.org/x/sys/unix"

// diskFreeBytes returns the free bytes available on the volume holding path,
// or 0 when it cannot be determined.
func diskFreeBytes(path string) uint64 {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0
	}
	return st.Bavail * uint64(st.Bsize) //nolint:unconvert // Bsize is int64 on some platforms
}
