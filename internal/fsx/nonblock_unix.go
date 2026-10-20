//go:build unix

package fsx

import "syscall"

// nonblockFlag keeps a swapped-in FIFO or symlink from blocking or being followed.
const nonblockFlag = syscall.O_NONBLOCK | syscall.O_NOFOLLOW | syscall.O_NOCTTY
