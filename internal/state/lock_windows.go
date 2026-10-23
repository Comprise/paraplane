//go:build windows

package state

import (
	"errors"
	"io/fs"
	"syscall"
)

// errSharingViolation is ERROR_SHARING_VIOLATION, which package syscall
// does not name.
const errSharingViolation = syscall.Errno(32)

// deletePending reports whether a failed create or Lstat of the lock may be
// passing: Windows refuses a name whose delete is pending with access denied
// until the last handle closes, and a file another program (an antivirus,
// an indexer) holds without sharing with a sharing violation.
func deletePending(err error) bool {
	return errors.Is(err, fs.ErrPermission) || errors.Is(err, errSharingViolation)
}
