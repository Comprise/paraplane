//go:build windows

package state

import (
	"errors"
	"io/fs"
)

// deletePending reports whether a failed create of the lock may be a lock
// still being removed by its holder: Windows refuses a new file at a name
// whose delete is pending with access denied until the last handle closes.
func deletePending(err error) bool {
	return errors.Is(err, fs.ErrPermission)
}
