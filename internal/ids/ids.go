// Package ids defines the single grammar of task and wave identifiers.
package ids

import (
	"regexp"
	"strings"
)

// Pattern is the regexp source of one id.
const Pattern = "[A-Za-z][A-Za-z0-9_.-]{0,31}"

// ListPattern is the regexp source of a comma-separated list of ids.
const ListPattern = Pattern + "(," + Pattern + ")*"

var valid = regexp.MustCompile("^" + Pattern + "$")

// Valid reports whether s is exactly one id.
func Valid(s string) bool { return valid.MatchString(s) }

// ParseList splits a comma list; every part must be a valid id.
func ParseList(s string) ([]string, bool) {
	parts := strings.Split(s, ",")
	for _, p := range parts {
		if !Valid(p) {
			return nil, false
		}
	}
	return parts, true
}
