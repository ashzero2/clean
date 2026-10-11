// Package trash moves files to the operating system trash, or removes them
// permanently when asked.
package trash

import "errors"

type Trasher interface {
	Trash(paths []string) error
	Remove(paths []string) error
}

var ErrUnsupported = errors.New("trash not supported on this platform")
