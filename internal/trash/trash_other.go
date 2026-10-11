//go:build !darwin

package trash

func New() Trasher { return unsupported{} }

type unsupported struct{}

func (unsupported) Trash([]string) error  { return ErrUnsupported }
func (unsupported) Remove([]string) error { return ErrUnsupported }
