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

package transcribe

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

const (
	// SampleRate is the rate of the sound the engine is given, Hz.
	SampleRate = 16000
	// wavHeader is the header ffmpeg writes before the samples, with no
	// metadata.
	wavHeader = 44
	// wavBytesPerSecond: 16 bit, mono.
	wavBytesPerSecond = SampleRate * 2
	// sizeUnknown is the size ffmpeg leaves in the RIFF and data chunks of
	// a WAV it writes to a pipe: the samples go on to the end of the
	// stream. Its wav muxer never switches to RF64 by itself ("-rf64 ...
	// (default never)" in ffmpeg -h muxer=wav, ffmpeg n9.0.2).
	sizeUnknown = 0xffffffff
)

// ErrUndecodable: the file has no sound that ffmpeg can decode; the same
// file will never have.
var ErrUndecodable = errors.New("transcribe: no sound in the file that ffmpeg can decode")

// Decoder turns what a client uploaded (Ogg/Opus of a voice message, MP4
// of a round video, whatever else a client sends as one) into the one
// format every engine reads. The ffmpeg of the server image does it, so an
// engine needs no codecs.
//
// The decoded sound is never stored: it is 32 kB a second whatever the
// record, sixteen to eighty times the size of a voice message, and the disk
// of the server holds MySQL too. Measure decodes it to nowhere to learn its
// length, and Open decodes it again as the engine reads it; ffmpeg decodes
// Opus hundreds of times faster than it plays. Neither holds more than a
// few frames of it in memory.
type Decoder struct {
	// Bin is the ffmpeg binary; "ffmpeg" of PATH if empty.
	Bin string
}

func (d Decoder) bin() string {
	if d.Bin == "" {
		return "ffmpeg"
	}
	return d.Bin
}

// args is the ffmpeg command line that writes the sound of src to its
// standard output as format. The file is a user's: only local files are
// opened, so a playlist in it cannot make ffmpeg fetch a URL; only the
// first audio stream is read, to its end, whatever the duration the client
// declared.
func (d Decoder) args(src, format string) []string {
	return []string{
		"-nostdin", "-hide_banner", "-loglevel", "error",
		"-protocol_whitelist", "file",
		"-i", src,
		"-map", "0:a:0", "-vn", "-sn", "-dn",
		"-ac", "1", "-ar", strconv.Itoa(SampleRate), "-c:a", "pcm_s16le",
		"-map_metadata", "-1", "-fflags", "+bitexact",
		"-f", format, "pipe:1",
	}
}

// Measure decodes the sound of the file src to nowhere and returns how long
// it is: the length the engine is given, whatever a client declared. The
// error is ErrUndecodable for a file ffmpeg cannot read, and anything else
// (no ffmpeg, a kill, the context) for a failure that says nothing about
// the file. ffmpeg writes to a pipe that is read to its end, so its failure
// is the file's: there is no output it could fail to write.
func (d Decoder) Measure(ctx context.Context, src string) (time.Duration, error) {
	cmd := exec.CommandContext(ctx, d.bin(), d.args(src, "s16le")...) //nolint:gosec // G204: the binary is the configured ffmpeg, the file is passed as one argument of -i
	cmd.WaitDelay = 5 * time.Second
	// What ffmpeg says on stderr may quote the tags of the file: it is not
	// kept.
	out, err := cmd.StdoutPipe()
	if err != nil {
		return 0, fmt.Errorf("transcribe: ffmpeg: %w", err)
	}
	if err = cmd.Start(); err != nil {
		return 0, fmt.Errorf("transcribe: ffmpeg: %w", err)
	}
	n, readErr := io.Copy(io.Discard, out)
	err = cmd.Wait()
	switch {
	case ctx.Err() != nil:
		return 0, ctx.Err()
	case err != nil:
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.Exited() {
			return 0, fmt.Errorf("%w: exit status %d", ErrUndecodable, exit.ExitCode())
		}
		return 0, fmt.Errorf("transcribe: ffmpeg: %w", err)
	case readErr != nil:
		return 0, fmt.Errorf("transcribe: ffmpeg: %w", readErr)
	}
	return time.Duration(n) * time.Second / wavBytesPerSecond, nil
}

// Open returns the sound of the file src as RIFF/WAVE, PCM signed 16 bit
// little endian, 16 kHz, mono, decoded as it is read. Its header gives no
// sizes (sizeUnknown): the samples run to the end of the stream, so a
// record of any length is read whole, past the 4 GiB (37 hours) a RIFF size
// can say. ffmpeg starts at the first Read, so a request the engine answers
// before it takes the sound decodes nothing; Close stops it. The Read that
// ends the stream returns io.EOF only when ffmpeg has decoded the whole
// file: a decoder that failed on the way is an error, never a sound cut
// short. Read and Close may be called from different goroutines.
func (d Decoder) Open(ctx context.Context, src string) io.ReadCloser {
	return &stream{ctx: ctx, bin: d.bin(), args: d.args(src, "wav")}
}

type stream struct {
	ctx  context.Context
	bin  string
	args []string

	mu     sync.Mutex
	cmd    *exec.Cmd
	out    io.Reader
	closed bool

	once    sync.Once
	waitErr error
}

var errStreamClosed = errors.New("transcribe: the decoded sound is closed")

// started returns the output of ffmpeg, starting it on the first call.
func (s *stream) started() (io.Reader, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.closed:
		return nil, errStreamClosed
	case s.cmd != nil:
		return s.out, nil
	}
	cmd := exec.CommandContext(s.ctx, s.bin, s.args...) //nolint:gosec // G204: the binary is the configured ffmpeg, the file is passed as one argument of -i
	cmd.WaitDelay = 5 * time.Second
	out, err := cmd.StdoutPipe()
	if err == nil {
		err = cmd.Start()
	}
	if err != nil {
		s.closed = true
		return nil, fmt.Errorf("transcribe: ffmpeg: %w", err)
	}
	s.cmd, s.out = cmd, out
	return out, nil
}

func (s *stream) Read(p []byte) (int, error) {
	out, err := s.started()
	if err != nil {
		return 0, err
	}
	n, err := out.Read(p)
	if errors.Is(err, io.EOF) {
		if werr := s.wait(); werr != nil {
			return n, werr
		}
	}
	return n, err
}

// wait waits for ffmpeg, once, and returns how it ended.
func (s *stream) wait() error {
	s.once.Do(func() {
		if err := s.cmd.Wait(); err != nil {
			s.waitErr = fmt.Errorf("transcribe: ffmpeg: %w", err)
		}
	})
	return s.waitErr
}

func (s *stream) Close() error {
	s.mu.Lock()
	cmd := s.cmd
	s.closed = true
	s.mu.Unlock()
	if cmd == nil {
		return nil
	}
	// Whatever is left of the sound is not wanted. A decoder that has
	// already ended is not hurt by the kill.
	_ = cmd.Process.Kill()
	_ = s.wait()
	return nil
}

// emptyWAV is a WAV of no samples in the format of Open, with its sizes.
func emptyWAV() []byte {
	b := make([]byte, wavHeader)
	copy(b[0:], "RIFF")
	binary.LittleEndian.PutUint32(b[4:], wavHeader-8)
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1) // PCM
	binary.LittleEndian.PutUint16(b[22:], 1) // mono
	binary.LittleEndian.PutUint32(b[24:], SampleRate)
	binary.LittleEndian.PutUint32(b[28:], wavBytesPerSecond)
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	return b
}
