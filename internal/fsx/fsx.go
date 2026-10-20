// Package fsx reads regular files without following symlinks or blocking on
// FIFOs and devices.
package fsx

import (
	"errors"
	"io"
	"math"
	"os"
)

// MaxFile is the default size cap for files read by hooks and commands.
const MaxFile = 1 << 20

var (
	// ErrNotRegular means the path is a symlink, FIFO, device, directory or
	// was swapped for another file between the check and the open.
	ErrNotRegular = errors.New("not a regular file")
	// ErrTooLarge means the file is bigger than the requested limit.
	ErrTooLarge = errors.New("file too large")
)

// afterLstat is a test hook called between Lstat and open; nil in production.
var afterLstat func()

// OpenRegular opens name only if it is a regular file (Lstat, so a symlink is
// refused) and still the same file after the open. A missing file returns the
// underlying os error.
func OpenRegular(name string) (*os.File, os.FileInfo, error) {
	lfi, err := os.Lstat(name)
	if err != nil {
		return nil, nil, err
	}
	if afterLstat != nil {
		afterLstat()
	}
	if !lfi.Mode().IsRegular() {
		return nil, nil, ErrNotRegular
	}
	f, err := os.OpenFile(name, os.O_RDONLY|nonblockFlag, 0)
	if err != nil {
		return nil, nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	if !fi.Mode().IsRegular() || !os.SameFile(lfi, fi) {
		f.Close()
		return nil, nil, ErrNotRegular
	}
	return f, fi, nil
}

// ReadRegular reads the whole regular file name; ErrTooLarge when it is
// longer than max bytes.
func ReadRegular(name string, max int64) ([]byte, error) {
	f, _, err := OpenRegular(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	limit := max
	if limit < math.MaxInt64 {
		limit++
	}
	data, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return nil, err
	}
	if max < math.MaxInt64 && int64(len(data)) > max {
		return nil, ErrTooLarge
	}
	return data, nil
}
