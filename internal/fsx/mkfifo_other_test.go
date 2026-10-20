//go:build !unix || solaris || aix

package fsx

import "errors"

func mkfifo(string) error { return errors.New("unsupported") }
