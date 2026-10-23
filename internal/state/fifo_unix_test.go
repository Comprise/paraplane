//go:build unix

package state

import (
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
)

func TestLoadFIFO(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "paraplane"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "paraplane", "closed.json"), 0o644); err != nil {
		t.Skip(err)
	}
	if s, err := Load(dir); err != nil || !reflect.DeepEqual(s, State{}) {
		t.Errorf("fifo: %+v %v", s, err)
	}
}
