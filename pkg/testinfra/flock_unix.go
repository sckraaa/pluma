//go:build unix

package testinfra

import (
	"context"
	"errors"
	"os"
	"syscall"
	"time"
)

// lockFile takes an exclusive lock of f (flock), waiting until ctx ends. The
// lock belongs to the open file: closing f, or the end of the process,
// releases it.
func lockFile(ctx context.Context, f *os.File) error {
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}
