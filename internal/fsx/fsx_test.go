package fsx

import (
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadRegular(t *testing.T) {
	p := write(t, "a.txt", "hello")
	got, err := ReadRegular(p, MaxFile)
	if err != nil || string(got) != "hello" {
		t.Fatalf("got %q, %v", got, err)
	}
	got, err = ReadRegular(p, 5)
	if err != nil || string(got) != "hello" {
		t.Fatalf("exact max: got %q, %v", got, err)
	}
}

func TestOpenRegular(t *testing.T) {
	p := write(t, "a.txt", "hello")
	f, fi, err := OpenRegular(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if fi.Size() != 5 || !fi.Mode().IsRegular() {
		t.Fatalf("bad info %v", fi)
	}
}

func TestSymlink(t *testing.T) {
	p := write(t, "a.txt", "x")
	l := filepath.Join(filepath.Dir(p), "link")
	if err := os.Symlink(p, l); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	if _, err := ReadRegular(l, MaxFile); !errors.Is(err, ErrNotRegular) {
		t.Fatalf("want ErrNotRegular, got %v", err)
	}
}

func TestDirectory(t *testing.T) {
	if _, err := ReadRegular(t.TempDir(), MaxFile); !errors.Is(err, ErrNotRegular) {
		t.Fatalf("want ErrNotRegular, got %v", err)
	}
}

func TestTooLarge(t *testing.T) {
	p := write(t, "a.txt", strings.Repeat("x", 11))
	if _, err := ReadRegular(p, 10); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
}

func TestMissing(t *testing.T) {
	_, err := ReadRegular(filepath.Join(t.TempDir(), "nope"), MaxFile)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("want ErrNotExist, got %v", err)
	}
}

func TestFIFO(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no FIFOs on windows")
	}
	p := filepath.Join(t.TempDir(), "fifo")
	if err := mkfifo(p); err != nil {
		t.Skip("mkfifo unavailable:", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := ReadRegular(p, MaxFile)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrNotRegular) {
			t.Fatalf("want ErrNotRegular, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("blocked on FIFO")
	}
}

func TestHugeMax(t *testing.T) {
	p := write(t, "a.txt", strings.Repeat("x", 3<<20))
	got, err := ReadRegular(p, math.MaxInt64)
	if err != nil || len(got) != 3<<20 {
		t.Fatalf("got %d bytes, %v", len(got), err)
	}
}

func hook(t *testing.T, f func()) {
	t.Helper()
	afterLstat = f
	t.Cleanup(func() { afterLstat = nil })
}

func TestSwappedFIFO(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no FIFOs on windows")
	}
	p := write(t, "a.txt", "x")
	hook(t, func() {
		os.Remove(p)
		if err := mkfifo(p); err != nil {
			t.Skip("mkfifo unavailable:", err)
		}
	})
	done := make(chan error, 1)
	go func() {
		_, err := ReadRegular(p, MaxFile)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrNotRegular) {
			t.Fatalf("want ErrNotRegular, got %v", err)
		}
	case <-time.After(5 * time.Second):
		// unblock the stuck open so the goroutine can end
		if w, err := os.OpenFile(p, os.O_WRONLY, 0); err == nil {
			w.Close()
		}
		t.Fatal("blocked on swapped-in FIFO")
	}
}

func TestSwappedRegular(t *testing.T) {
	p := write(t, "a.txt", "x")
	other := write(t, "b.txt", "y")
	hook(t, func() {
		if err := os.Rename(other, p); err != nil {
			t.Skip("rename over open target unsupported:", err)
		}
	})
	if _, err := ReadRegular(p, MaxFile); !errors.Is(err, ErrNotRegular) {
		t.Fatalf("want ErrNotRegular, got %v", err)
	}
}

// TestUnreadable: a regular file that cannot be opened returns the open error,
// not ErrNotRegular.
func TestUnreadable(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("file modes do not stop this user")
	}
	p := write(t, "secret", "x")
	if err := os.Chmod(p, 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := OpenRegular(p); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("OpenRegular: want ErrPermission, got %v", err)
	}
	if _, err := ReadRegular(p, MaxFile); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("ReadRegular: want ErrPermission, got %v", err)
	}
}

// TestExactLimit: a file of exactly max bytes is read; one byte more is
// ErrTooLarge.
func TestExactLimit(t *testing.T) {
	p := write(t, "f", "abcd")
	if b, err := ReadRegular(p, 4); err != nil || string(b) != "abcd" {
		t.Fatalf("at the limit: %q %v", b, err)
	}
	if _, err := ReadRegular(p, 3); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("over the limit: %v", err)
	}
}
