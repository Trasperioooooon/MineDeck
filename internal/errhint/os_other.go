//go:build !windows

package errhint

import (
	"errors"
	"syscall"
)

func isFileBusy(error) bool { return false }

func isDiskFull(err error) bool {
	var errno syscall.Errno
	return errors.As(err, &errno) && (errno == syscall.ENOSPC || errno == syscall.EDQUOT)
}
