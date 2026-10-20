//go:build !race

package layout

// raceEnabled turns off timing assertions under the race detector.
const raceEnabled = false
