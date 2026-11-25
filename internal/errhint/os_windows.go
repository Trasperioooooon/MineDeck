//go:build windows

package errhint

import (
	"errors"
	"syscall"
)

const (
	errSharingViolation = syscall.Errno(32)
	errLockViolation    = syscall.Errno(33)
	errHandleDiskFull   = syscall.Errno(39)
	errDiskFull         = syscall.Errno(112)
	errUserMappedFile   = syscall.Errno(1224)
)

func isFileBusy(err error) bool {
	var errno syscall.Errno
	return errors.As(err, &errno) && (errno == errSharingViolation || errno == errLockViolation || errno == errUserMappedFile)
}

func isDiskFull(err error) bool {
	var errno syscall.Errno
	return errors.As(err, &errno) && (errno == errDiskFull || errno == errHandleDiskFull)
}
