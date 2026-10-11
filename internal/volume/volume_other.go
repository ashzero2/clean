//go:build !darwin

package volume

func Stat(path string) (Usage, error) { return Usage{Path: path}, ErrUnsupported }
