//go:build !darwin

package scan

import (
	"errors"
	"io/fs"
)

// walk is the traversal driver on platforms without getattrlistbulk.
func walk(root string, opts Options, fn fs.WalkDirFunc) error {
	return fastwalkWalk(root, opts, fn)
}

// batchSupported reports that this platform has no folded per-directory driver.
func batchSupported() bool { return false }

// walkBatched is never reached where batchSupported is false.
func walkBatched(root string, opts Options, send func(event) error) error {
	return errors.New("batched walk unsupported")
}
