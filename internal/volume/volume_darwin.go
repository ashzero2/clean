//go:build darwin

package volume

import "syscall"

// On APFS "available" is fuzzy — the container shares free space across volumes
// and macOS counts purgeable/snapshot space as available — so Used/Total is a
// display figure, not a deletion promise.
func Stat(path string) (Usage, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return Usage{}, err
	}
	bs := int64(st.Bsize)
	total := int64(st.Blocks) * bs
	free := int64(st.Bfree) * bs
	avail := int64(st.Bavail) * bs
	return Usage{
		Path:      path,
		Total:     total,
		Used:      total - free,
		Free:      free,
		Available: avail,
	}, nil
}
