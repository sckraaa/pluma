//go:build !unix

package testinfra

import (
	"context"
	"os"
)

// lockFile does not lock without flock: test binaries that need the same
// schema image at once each build it, which costs time only.
func lockFile(context.Context, *os.File) error { return nil }
