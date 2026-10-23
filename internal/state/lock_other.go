//go:build !windows

package state

// deletePending is false where a removed file frees its name at once.
func deletePending(error) bool { return false }
