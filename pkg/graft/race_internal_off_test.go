//go:build !race

package graft

// raceDetectorOn reports whether the test binary was built with the race
// detector.
const raceDetectorOn = false
