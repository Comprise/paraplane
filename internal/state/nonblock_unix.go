//go:build unix

package state

import "syscall"

// nonblockFlag keeps the open of a FIFO planted at closed.json from blocking.
const nonblockFlag = syscall.O_NONBLOCK
