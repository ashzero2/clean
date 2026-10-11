//go:build darwin

package scan

import (
	"io/fs"
	"syscall"
)

// On macOS a Stat_t reports allocated storage in Blocks of 512 bytes regardless
// of the volume's block size, so allocated = Blocks*512 — the figure du reports.
func statSizes(fi fs.FileInfo) (apparent, allocated int64, nlink uint32) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		sz := fi.Size()
		return sz, sz, 1
	}
	return st.Size, int64(st.Blocks) * 512, uint32(st.Nlink)
}

func fileID(fi fs.FileInfo) (dev, ino uint64, ok bool) {
	st, k := fi.Sys().(*syscall.Stat_t)
	if !k {
		return 0, 0, false
	}
	return uint64(st.Dev), st.Ino, true
}
