//go:build !race

// pluma: raceEnabled tells allocation-bound tests that they run under the race
// detector (see allocLimit).

package mtproto

const raceEnabled = false
