//go:build windows

package server

import "golang.org/x/sys/windows"

// diskFreeBytes returns the free bytes available on the volume holding path,
// or 0 when it cannot be determined.
func diskFreeBytes(path string) uint64 {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0
	}
	var free, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &free, &total, &totalFree); err != nil {
		return 0
	}
	return free
}
