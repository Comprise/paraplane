//go:build race

package hooks

// raceEnabled reports whether the tests run with -race, which slows them
// too much for wall-clock checks.
const raceEnabled = true
