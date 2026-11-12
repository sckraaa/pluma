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
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestDecoderArguments(t *testing.T) {
	for _, format := range []string{"wav", "s16le"} {
		args := Decoder{}.args("/tmp/in", format)
		line := " " + strings.Join(args, " ") + " "
		for _, want := range []string{
			" -nostdin ",
			" -protocol_whitelist file -i /tmp/in ", // before the input: an option of it
			" -map 0:a:0 ", " -vn ",
			" -ac 1 ", " -ar 16000 ", " -c:a pcm_s16le ",
			// To the pipe, never to a file: the decoded sound is not stored.
			" -f " + format + " pipe:1 ",
		} {
			if !strings.Contains(line, want) {
				t.Errorf("no %q in %q", want, line)
			}
		}
		// No length is given: a record is decoded to its end, however long.
		for _, cut := range []string{"-t", "-to", "-fs", "-frames:a"} {
			if slices.Contains(args, cut) {
				t.Errorf("%s cuts the record: %q", cut, line)
			}
		}
	}
}

// needFFmpeg skips the test when ffmpeg is not installed; the server image
// and the CI have it.
func needFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skipf("ffmpeg is not installed: %v", err)
	}
}

// render makes a test input with ffmpeg's lavfi sources.
func render(t *testing.T, name string, args ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	out, err := exec.Command("ffmpeg", append(append([]string{"-v", "error", "-y"}, args...), path)...).CombinedOutput()
	if err != nil {
		t.Fatalf("ffmpeg: %v\n%s", err, out)
	}
	return path
}

// checkStream reads the whole sound Open gives for src and checks it is the
// format every engine reads, of about want.
func checkStream(t *testing.T, src string, want time.Duration) {
	t.Helper()
	sound := Decoder{}.Open(context.Background(), src)
	defer sound.Close()
	head := make([]byte, wavHeader)
	if _, err := io.ReadFull(sound, head); err != nil {
		t.Fatal(err)
	}
	samples, err := io.Copy(io.Discard, sound)
	if err != nil {
		t.Fatal(err)
	}
	// The plain 44-byte header: RIFF, fmt of 16 bytes, data. No LIST chunk
	// and no sizes: the samples run to the end of the stream, however
	// long, and no 32-bit size wraps past 4 GiB.
	if string(head[0:4]) != "RIFF" || string(head[8:16]) != "WAVEfmt " || string(head[36:40]) != "data" {
		t.Fatalf("header %q", head)
	}
	le16 := func(o int) uint16 { return binary.LittleEndian.Uint16(head[o:]) }
	le32 := func(o int) uint32 { return binary.LittleEndian.Uint32(head[o:]) }
	if le16(20) != 1 || le16(22) != 1 || le32(24) != SampleRate || le16(34) != 16 {
		t.Fatalf("format %d, %d channels, %d Hz, %d bits", le16(20), le16(22), le32(24), le16(34))
	}
	if le32(4) != sizeUnknown || le32(40) != sizeUnknown {
		t.Fatalf("sizes %#x and %#x, want none", le32(4), le32(40))
	}
	got := time.Duration(samples) * time.Second / wavBytesPerSecond
	if d := got - want; d < -150*time.Millisecond || d > 150*time.Millisecond {
		t.Fatalf("%s of sound, want about %s", got, want)
	}
}

func TestDecodeVoiceAndRoundVideo(t *testing.T) {
	needFFmpeg(t)
	inputs := map[string]string{
		// What the clients upload: Ogg/Opus for a voice message, MP4 with
		// AAC next to the video for a round one.
		"voice.ogg": render(t, "voice.ogg", "-f", "lavfi", "-i", "sine=frequency=440:duration=2", "-ac", "1", "-c:a", "libopus", "-b:a", "16k"),
		"round.mp4": render(t, "round.mp4", "-f", "lavfi", "-i", "testsrc2=size=240x240:rate=15:duration=2",
			"-f", "lavfi", "-i", "sine=frequency=330:duration=2", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-ac", "2"),
	}
	for name, src := range inputs {
		t.Run(name, func(t *testing.T) {
			d, err := Decoder{}.Measure(context.Background(), src)
			if err != nil {
				t.Fatal(err)
			}
			if d < 1900*time.Millisecond || d > 2150*time.Millisecond {
				t.Fatalf("duration %s", d)
			}
			checkStream(t, src, d)
		})
	}
}

func TestDecodeTakesTheWholeRecord(t *testing.T) {
	needFFmpeg(t)
	// Twenty minutes, longer than any limit there ever was: nothing is cut,
	// and the length is the decoded one, not what a client declared.
	const long = 20 * time.Minute
	src := render(t, "long.ogg", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=16000:duration=1200", "-ac", "1", "-c:a", "libopus", "-b:a", "6k")
	d, err := Decoder{}.Measure(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	if d < long-time.Second || d > long+time.Second {
		t.Fatalf("decoded %s of %s", d, long)
	}
	checkStream(t, src, d)
}

func TestDecodeRefusesWhatIsNotSound(t *testing.T) {
	needFFmpeg(t)
	dir := t.TempDir()
	garbage := filepath.Join(dir, "garbage")
	if err := os.WriteFile(garbage, []byte(strings.Repeat("not a media file\n", 64)), 0o600); err != nil {
		t.Fatal(err)
	}
	silent := render(t, "silent.mp4", "-f", "lavfi", "-i", "testsrc2=size=64x64:rate=5:duration=1", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-an")
	// A playlist that points at a URL: ffmpeg must not go there.
	playlist := filepath.Join(dir, "list.m3u8")
	if err := os.WriteFile(playlist, []byte("#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXTINF:1,\nhttp://127.0.0.1:9/a.ts\n#EXT-X-ENDLIST\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, src := range map[string]string{"garbage": garbage, "a video without sound": silent, "a playlist of URLs": playlist} {
		t.Run(name, func(t *testing.T) {
			_, err := Decoder{}.Measure(context.Background(), src)
			if !errors.Is(err, ErrUndecodable) {
				t.Fatalf("got %v", err)
			}
			// Streamed, it is an error at the end, never a short sound.
			sound := Decoder{}.Open(context.Background(), src)
			defer sound.Close()
			if _, err = io.Copy(io.Discard, sound); err == nil {
				t.Fatal("the stream of what is not sound ended as a sound")
			}
		})
	}
}

func TestDecodeFailuresThatAreNotTheFiles(t *testing.T) {
	missing := Decoder{Bin: filepath.Join(t.TempDir(), "no-ffmpeg")}
	_, err := missing.Measure(context.Background(), "in")
	if err == nil || errors.Is(err, ErrUndecodable) {
		t.Fatalf("a missing ffmpeg must not condemn the record: %v", err)
	}
	if _, err = io.Copy(io.Discard, missing.Open(context.Background(), "in")); err == nil {
		t.Fatal("a stream without ffmpeg ended as a sound")
	}

	needFFmpeg(t)
	src := render(t, "voice.ogg", "-f", "lavfi", "-i", "sine=frequency=440:duration=2", "-ac", "1", "-c:a", "libopus", "-b:a", "16k")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Decoder{}.Measure(ctx, src)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled attempt: %v", err)
	}
}

// The stream starts ffmpeg only when it is read, and Close stops it in the
// middle; a read after that is an error.
func TestDecodeStreamStartsLateAndStops(t *testing.T) {
	needFFmpeg(t)
	// Never read: nothing to start, nothing to stop.
	if err := (Decoder{Bin: filepath.Join(t.TempDir(), "no-ffmpeg")}).Open(context.Background(), "in").Close(); err != nil {
		t.Fatal(err)
	}
	src := render(t, "long.ogg", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=16000:duration=600", "-ac", "1", "-c:a", "libopus", "-b:a", "6k")
	sound := Decoder{}.Open(context.Background(), src)
	if _, err := io.ReadFull(sound, make([]byte, 64<<10)); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, sound)
		done <- err
	}()
	if err := sound.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
		// The reader ends, with whatever it saw before the kill; it never
		// hangs.
	case <-time.After(10 * time.Second):
		t.Fatal("a read went on after Close")
	}
	if _, err := sound.Read(make([]byte, 16)); err == nil {
		t.Fatal("a read after Close")
	}
}

// A sound of no samples, for Probe: the header alone, with its sizes.
func TestEmptyWAV(t *testing.T) {
	b := emptyWAV()
	if len(b) != wavHeader || !bytes.HasPrefix(b, []byte("RIFF")) || binary.LittleEndian.Uint32(b[4:]) != 36 || binary.LittleEndian.Uint32(b[40:]) != 0 {
		t.Fatalf("% x", b)
	}
	if binary.LittleEndian.Uint32(b[24:]) != SampleRate || binary.LittleEndian.Uint16(b[22:]) != 1 || binary.LittleEndian.Uint16(b[34:]) != 16 {
		t.Fatalf("format % x", b[20:36])
	}
}
