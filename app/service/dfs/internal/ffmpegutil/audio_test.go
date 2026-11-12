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

package ffmpegutil

import (
	"bytes"
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// A notification sound is probed from a file: an MP3 and an Ogg sound give
// their container and duration; a picture, a video, a text and a playlist
// that names another file are no sound.
func TestProbeAudio(t *testing.T) {
	needFFmpeg(t)
	u := NewFFmpegUtil()
	ctx := context.Background()
	tone := []string{"-f", "lavfi", "-i", "sine=frequency=440:duration=2"}
	for _, k := range []struct {
		name, file, format string
		args               []string
	}{
		{"an MP3", "a.mp3", "mp3", append(tone[:len(tone):len(tone)], "-c:a", "libmp3lame", "-b:a", "64k")},
		{"an Ogg Opus sound", "a.ogg", "ogg", append(tone[:len(tone):len(tone)], "-c:a", "libopus", "-b:a", "32k")},
		// The name does not decide: iOS says audio/mpeg for every sound.
		{"an Ogg sound named as an MP3", "b.mp3", "ogg", append(tone[:len(tone):len(tone)], "-c:a", "libopus", "-f", "ogg")},
	} {
		info, err := u.ProbeAudio(ctx, makeInput(t, k.file, k.args...))
		if err != nil {
			t.Errorf("%s: %v", k.name, err)
			continue
		}
		if info.Format != k.format || math.Abs(info.Duration-2) > 0.2 {
			t.Errorf("%s: %+v, want %s of 2 seconds", k.name, info, k.format)
		}
	}

	secret := filepath.Join(t.TempDir(), "secret.mp3")
	if err := os.WriteFile(secret, []byte("not for the playlist"), 0o600); err != nil {
		t.Fatal(err)
	}
	playlist := filepath.Join(t.TempDir(), "list.mp3")
	if err := os.WriteFile(playlist, []byte("#EXTM3U\n#EXT-X-TARGETDURATION:2\n#EXTINF:2,\nfile://"+secret+"\n#EXT-X-ENDLIST\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	text := filepath.Join(t.TempDir(), "text.mp3")
	if err := os.WriteFile(text, bytes.Repeat([]byte("hello, this is no sound. "), 200), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{
		"a picture":  makeInput(t, "p.png", "-f", "lavfi", "-i", "color=c=red:s=16x16", "-frames:v", "1"),
		"a video":    makeInput(t, "v.mp4", "-f", "lavfi", "-i", "color=c=red:s=16x16:d=1", "-pix_fmt", "yuv420p"),
		"a WAV":      makeInput(t, "w.wav", tone...),
		"a playlist": playlist,
		"a text":     text,
		"no file":    filepath.Join(t.TempDir(), "missing.mp3"),
	} {
		if info, err := u.ProbeAudio(ctx, path); !errors.Is(err, ErrNotAudio) {
			t.Errorf("%s: %+v, %v; want ErrNotAudio", name, info, err)
		}
	}
}

// An Ogg sound becomes an MP3 of the same length that fits the limit of a
// notification sound: five seconds at 128 kbit/s are 80 KB.
func TestToMP3(t *testing.T) {
	needFFmpeg(t)
	u := NewFFmpegUtil()
	ctx := context.Background()
	ogg := makeInput(t, "long.ogg", "-f", "lavfi", "-i", "sine=frequency=440:duration=5", "-c:a", "libopus", "-b:a", "96k")
	data, err := u.ToMP3(ctx, ogg)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 || len(data) > 100*1024 {
		t.Fatalf("%d bytes of MP3, want at most 100 KB", len(data))
	}
	out := filepath.Join(t.TempDir(), "out.mp3")
	if err = os.WriteFile(out, data, 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := u.ProbeAudio(ctx, out)
	if err != nil || info.Format != "mp3" || math.Abs(info.Duration-5) > 0.2 {
		t.Errorf("the converted sound: %+v, %v; want an MP3 of 5 seconds", info, err)
	}

	text := filepath.Join(t.TempDir(), "text.ogg")
	if err = os.WriteFile(text, []byte("no sound"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = u.ToMP3(ctx, text); !errors.Is(err, ErrNotAudio) {
		t.Errorf("a text: %v, want ErrNotAudio", err)
	}
}
