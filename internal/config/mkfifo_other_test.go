//go:build !unix || solaris || aix

package config

import "errors"

func mkfifo(string) error { return errors.New("unsupported") }
