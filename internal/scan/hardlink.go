package scan

type fileKey struct {
	dev uint64
	ino uint64
}

// hardlinkSet records (dev, ino) pairs already counted. It is touched only from
// the single consumer goroutine, and only files with nlink > 1 are inserted.
type hardlinkSet map[fileKey]struct{}

func newHardlinkSet() hardlinkSet { return make(hardlinkSet) }

// dedupe returns count=false for an already-seen inode; dup then reports it.
func (s hardlinkSet) dedupe(dev, ino uint64, nlink uint32) (count, dup bool) {
	if nlink <= 1 {
		return true, false
	}
	k := fileKey{dev: dev, ino: ino}
	if _, seen := s[k]; seen {
		return false, true
	}
	s[k] = struct{}{}
	return true, false
}
