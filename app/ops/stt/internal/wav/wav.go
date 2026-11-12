// Copyright 2026 The Pluma Authors
//  All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package wav reads the one sound format the service takes: RIFF/WAVE,
// PCM signed 16 bit little endian, 16 kHz, one channel (ADR 0056, p. 2).
// Decoding of everything else is the caller's, so the image has no codecs.
package wav

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// SampleRate is the rate of the samples, Hz.
const SampleRate = 16000

const (
	formatPCM        = 1
	formatExtensible = 0xfffe
	// maxSkipped bounds the chunks before the samples (LIST and the like).
	maxSkipped = 1 << 20
	// sizeUnknown is the length a writer that cannot seek leaves (ffmpeg
	// to a pipe): the samples go on to the end of the stream.
	sizeUnknown = 0xffffffff
)

// ErrFormat is a stream that is not the format above.
var ErrFormat = errors.New("wav: not 16 kHz mono 16-bit PCM RIFF/WAVE")

func formatError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrFormat, fmt.Sprintf(format, args...))
}

// Reader gives the samples of a stream as floats in [-1, 1).
type Reader struct {
	r    io.Reader
	left int64 // bytes of samples still to read, -1 when not known
	buf  []byte
	read int64 // samples read so far
}

// NewReader reads the header of r up to the samples.
func NewReader(r io.Reader) (*Reader, error) {
	var head [12]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return nil, formatError("no header")
	}
	if string(head[0:4]) != "RIFF" || string(head[8:12]) != "WAVE" {
		return nil, formatError("no RIFF/WAVE signature")
	}
	var (
		haveFormat bool
		skipped    int64
	)
	for {
		var chunk [8]byte
		if _, err := io.ReadFull(r, chunk[:]); err != nil {
			return nil, formatError("no samples")
		}
		id, size := string(chunk[0:4]), binary.LittleEndian.Uint32(chunk[4:8])
		switch id {
		case "fmt ":
			if size < 16 || size > 64 {
				return nil, formatError("format of %d bytes", size)
			}
			body := make([]byte, size+size%2)
			if _, err := io.ReadFull(r, body); err != nil {
				return nil, formatError("short format")
			}
			if err := checkFormat(body[:size]); err != nil {
				return nil, err
			}
			haveFormat = true
		case "data":
			if !haveFormat {
				return nil, formatError("samples before the format")
			}
			left := int64(size)
			if size == sizeUnknown || size == 0 {
				left = -1
			}
			return &Reader{r: r, left: left}, nil
		default:
			n := int64(size) + int64(size%2)
			if skipped += n; skipped > maxSkipped {
				return nil, formatError("no samples in the first megabyte")
			}
			if _, err := io.CopyN(io.Discard, r, n); err != nil {
				return nil, formatError("short %q", id)
			}
		}
	}
}

func checkFormat(b []byte) error {
	format := binary.LittleEndian.Uint16(b[0:2])
	channels := binary.LittleEndian.Uint16(b[2:4])
	rate := binary.LittleEndian.Uint32(b[4:8])
	bits := binary.LittleEndian.Uint16(b[14:16])
	if format == formatExtensible && len(b) >= 26 {
		// The real format is the start of the sub-format GUID.
		format = binary.LittleEndian.Uint16(b[24:26])
	}
	if format != formatPCM || channels != 1 || rate != SampleRate || bits != 16 {
		return formatError("format %d, %d channels, %d Hz, %d bits", format, channels, rate, bits)
	}
	return nil
}

// Read fills out with the next samples and returns how many it wrote; fewer
// than len(out) only together with io.EOF, at the end of the stream.
func (d *Reader) Read(out []float32) (int, error) {
	if len(out) == 0 {
		return 0, nil
	}
	want := int64(len(out)) * 2
	if d.left >= 0 && want > d.left {
		want = d.left - d.left%2
	}
	if want == 0 {
		return 0, io.EOF
	}
	if int64(cap(d.buf)) < want {
		d.buf = make([]byte, want)
	}
	buf := d.buf[:want]
	got, err := io.ReadFull(d.r, buf)
	if d.left >= 0 {
		d.left -= int64(got)
	}
	n := got / 2
	for i := 0; i < n; i++ {
		out[i] = float32(int16(binary.LittleEndian.Uint16(buf[2*i:]))) / 32768
	}
	d.read += int64(n)
	switch {
	case err == nil && n == len(out):
		return n, nil
	case err == nil, errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		// The end: of the chunk, or of a stream cut short, whose samples
		// so far are still speech.
		return n, io.EOF
	}
	return n, err
}

// Samples is how many samples Read has given.
func (d *Reader) Samples() int64 { return d.read }
