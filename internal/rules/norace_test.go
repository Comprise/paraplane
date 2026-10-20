//go:build !race

package rules

// raceEnabled turns off timing assertions under the race detector.
const raceEnabled = false
