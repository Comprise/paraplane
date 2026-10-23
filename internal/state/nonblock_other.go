//go:build !unix

package state

// nonblockFlag is zero where FIFOs cannot be planted in the file system.
const nonblockFlag = 0
