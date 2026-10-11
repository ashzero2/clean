//go:build !darwin

package scan

import "io/fs"

// Fallback: allocated == apparent and nlink == 1, disabling hard-link dedup.
func statSizes(fi fs.FileInfo) (apparent, allocated int64, nlink uint32) {
	sz := fi.Size()
	return sz, sz, 1
}

func fileID(fi fs.FileInfo) (dev, ino uint64, ok bool) { return 0, 0, false }
