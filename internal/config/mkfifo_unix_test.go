//go:build unix && !solaris && !aix

package config

import "syscall"

func mkfifo(p string) error { return syscall.Mkfifo(p, 0o644) }
