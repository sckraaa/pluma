//go:build !tdlib

package tdjson

// Open reports that this binary has no TDLib in it.
func Open() (Library, error) {
	return nil, ErrNotLinked
}
