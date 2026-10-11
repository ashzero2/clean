package volume

import "errors"

type Usage struct {
	Path      string
	Total     int64
	Used      int64
	Free      int64 // blocks free to the superuser
	Available int64 // blocks available to non-privileged users
}

var ErrUnsupported = errors.New("volume usage not supported on this platform")
