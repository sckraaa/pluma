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

// Package enginetest checks that a speech engine can stand behind
// STT_URL: that it speaks the protocol the server relies on (ADR 0056,
// p. 2), whatever model or software it is. It talks to a running engine
// through the server's own client and decoder, so what passes here is what
// bff will do.
//
// The suite runs against an engine named in the environment and is skipped
// without one (docs/ops/transcription.md, "Проверка движка"):
//
//	STT_ENGINE_URL=http://127.0.0.1:9379 STT_ENGINE_MODEL=gigaam-v3-e2e-rnnt \
//	STT_ENGINE_SAMPLE=/path/to/speech.ogg STT_ENGINE_EXPECT=лукоморья,дуб \
//	go test -count=1 -v ./pkg/transcribe/enginetest
package enginetest

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/teamgram/teamgram-server/pkg/transcribe"
)

// Target is an engine to check.
type Target struct {
	// Config is the Transcribe block the server would have: URL, Model,
	// and Token and Language if the engine needs them.
	Config transcribe.Config
	// Sample is a record of speech in any format ffmpeg reads: a voice
	// message as a client uploaded it will do.
	Sample string
	// Expect are words of the sample that must be in the text, compared
	// without case.
	Expect []string
	// Strict also checks what our own service promises and another engine
	// may do differently: a refusal of what is not a WAV, an empty text
	// for silence, ErrDown for a model it does not run.
	Strict bool
	// Long, if set, also sends one record at least that long, the sample
	// over and over with pauses: nothing of it may be lost, and the engine
	// must keep to the time the server gives it. Its memory is watched
	// from outside (docker stats), not here.
	Long time.Duration
}

// Run checks the engine of target. It needs ffmpeg, as the server does.
func Run(t *testing.T, target Target) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Fatalf("the suite decodes the sample as the server does and needs ffmpeg: %v", err)
	}
	cfg := target.Config.WithDefaults()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	engine := transcribe.NewHTTPEngine(cfg)
	dir := t.TempDir()

	// The sample as a WAV file, to be repeated into longer records. Every
	// file goes to the engine as bff sends a record: measured, and decoded
	// by ffmpeg as the engine reads it.
	speech := filepath.Join(dir, "speech.wav")
	if b, err := exec.Command("ffmpeg", "-v", "error", "-y", "-i", target.Sample, "-map", "0:a:0", //nolint:gosec // G204: the sample the operator named, one argument of -i
		"-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", "-map_metadata", "-1", "-fflags", "+bitexact", speech).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v\n%s", err, b)
	}
	length, err := transcribe.Decoder{}.Measure(context.Background(), target.Sample)
	if err != nil || length < time.Second {
		t.Fatalf("the sample %s: %s of sound, %v", target.Sample, length, err)
	}
	ask := func(t *testing.T, e transcribe.Engine, path string, length time.Duration) (string, time.Duration, error) {
		t.Helper()
		sound := transcribe.Decoder{}.Open(context.Background(), path)
		defer sound.Close()
		start := time.Now()
		text, err := e.Transcribe(context.Background(), sound, length)
		return text, time.Since(start), err
	}
	has := func(t *testing.T, text string) {
		t.Helper()
		if strings.TrimSpace(text) == "" {
			t.Fatal("no text for a record of speech")
		}
		for _, w := range target.Expect {
			if !strings.Contains(strings.ToLower(text), strings.ToLower(w)) {
				t.Errorf("the text has no %q", w)
			}
		}
	}

	t.Run("speech", func(t *testing.T) {
		text, took, err := ask(t, engine, speech, length)
		if err != nil {
			t.Fatal(err)
		}
		has(t, text)
		t.Logf("%.1f s of sound in %.2f s (%.2f s per minute of sound), %d characters", length.Seconds(), took.Seconds(), took.Seconds()*60/length.Seconds(), len([]rune(text)))
	})

	t.Run("a record longer than a model takes at once", func(t *testing.T) {
		// The sample three times over with pauses, more than a minute at
		// least: cutting it into pieces is the engine's job.
		long, loops, longLength := repeat(t, dir, speech, length, 70*time.Second, 3)
		text, took, err := ask(t, engine, long, longLength)
		if err != nil {
			t.Fatal(err)
		}
		has(t, text)
		if len(target.Expect) > 0 {
			if n := strings.Count(strings.ToLower(text), strings.ToLower(target.Expect[0])); n < loops-1 {
				t.Errorf("%q is in the text %d times, the record says it %d times: a part of the record was dropped", target.Expect[0], n, loops)
			}
		}
		if limit := cfg.EngineTimeout().For(longLength); took > limit/2 {
			t.Errorf("%s for %s of sound: the server gives up after %s", took, longLength, limit)
		}
		t.Logf("%.1f s of sound in %.2f s (%.2f s per minute of sound), %d characters", longLength.Seconds(), took.Seconds(), took.Seconds()*60/longLength.Seconds(), len([]rune(text)))
	})

	if target.Long > 0 {
		t.Run("a long record", func(t *testing.T) {
			long, loops, longLength := repeat(t, dir, speech, length, target.Long, 1)
			text, took, err := ask(t, engine, long, longLength)
			if err != nil {
				t.Fatalf("%s of sound: %v", longLength, err)
			}
			has(t, text)
			if len(target.Expect) > 0 {
				// One word in fifty may be heard otherwise; a dropped piece of
				// the record loses far more.
				if n := strings.Count(strings.ToLower(text), strings.ToLower(target.Expect[0])); n < loops-loops/50 {
					t.Errorf("%q is in the text %d times, the record says it %d times: a part of the record was dropped", target.Expect[0], n, loops)
				} else {
					t.Logf("%q is in the text %d times of %d", target.Expect[0], n, loops)
				}
			}
			if limit := cfg.EngineTimeout().For(longLength); took > limit/2 {
				t.Errorf("%s for %s of sound: the server gives up after %s", took, longLength, limit)
			}
			t.Logf("%.1f s of sound in %.2f s (%.2f s per minute of sound), %d characters", longLength.Seconds(), took.Seconds(), took.Seconds()*60/longLength.Seconds(), len([]rune(text)))
		})
	}

	t.Run("silence", func(t *testing.T) {
		quiet := filepath.Join(dir, "quiet.wav")
		if err := os.WriteFile(quiet, silence(3*time.Second), 0o600); err != nil {
			t.Fatal(err)
		}
		text, _, err := ask(t, engine, quiet, 3*time.Second)
		if err != nil {
			t.Fatalf("silence is no error, it is an empty text: %v", err)
		}
		if text != "" {
			if target.Strict {
				t.Errorf("a text of %d characters for silence", len([]rune(text)))
			} else {
				t.Logf("the engine makes up %d characters for silence", len([]rune(text)))
			}
		}
	})

	t.Run("what is not sound", func(t *testing.T) {
		junk := filepath.Join(dir, "junk.wav")
		if err := os.WriteFile(junk, bytes.Repeat([]byte("this is not a sound file\n"), 100), 0o600); err != nil {
			t.Fatal(err)
		}
		// As it is, not through ffmpeg, which would refuse it.
		f, err := os.Open(junk)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		_, err = engine.Transcribe(context.Background(), f, time.Second)
		switch {
		case errors.Is(err, transcribe.ErrRejected):
		case target.Strict:
			t.Errorf("got %v, want the sound rejected for good", err)
		default:
			// The server never sends such a body (ffmpeg wrote it); an
			// engine that answers otherwise only costs retries.
			t.Logf("the engine answers what is not sound with: %v", err)
		}
	})

	t.Run("a model it does not have", func(t *testing.T) {
		other := cfg
		other.Model = "no-such-model-" + cfg.Model
		_, _, err := ask(t, transcribe.NewHTTPEngine(other), speech, length)
		// A wrong STT_MODEL is a mistake of the configuration: it must
		// never read as a record that cannot be transcribed, or every
		// record asked about meanwhile is spoiled for good.
		if errors.Is(err, transcribe.ErrRejected) {
			t.Fatalf("an unknown model reads as a bad record: %v", err)
		}
		switch {
		case err == nil:
			t.Log("the engine takes any model name")
		case target.Strict && !errors.Is(err, transcribe.ErrDown):
			t.Errorf("got %v, want the engine unusable until the configuration is fixed (404)", err)
		}
	})

	if cfg.Token != "" {
		t.Run("a wrong token", func(t *testing.T) {
			other := cfg
			other.Token = strings.Repeat("x", transcribe.MinTokenLength)
			_, _, err := ask(t, transcribe.NewHTTPEngine(other), speech, length)
			if err == nil || errors.Is(err, transcribe.ErrRejected) {
				t.Fatalf("got %v, want a failure that is not the record's", err)
			}
		})
	}

	t.Run("two requests at once", func(t *testing.T) {
		var (
			wg    sync.WaitGroup
			texts [2]string
			errs  [2]error
		)
		for i := range errs {
			wg.Add(1)
			go func() {
				defer wg.Done()
				texts[i], _, errs[i] = ask(t, engine, speech, length)
			}()
		}
		wg.Wait()
		done := 0
		for i, err := range errs {
			switch {
			case err == nil:
				done++
				has(t, texts[i])
			case errors.Is(err, transcribe.ErrRejected):
				t.Errorf("a busy engine reads as a bad record: %v", err)
			case !errors.Is(err, transcribe.ErrBusy):
				t.Logf("the second request: %v (the server tries again)", err)
			}
		}
		if done == 0 {
			t.Fatalf("neither request was served: %v, %v", errs[0], errs[1])
		}
	})
}

// repeat writes the decoded sample, length long, over and over with pauses
// of a second and a half until it is at least atLeast long and at least
// minLoops times, and returns the file, the number of times and its length.
func repeat(t *testing.T, dir, speech string, length, atLeast time.Duration, minLoops int) (string, int, time.Duration) {
	t.Helper()
	const gap = 1500 * time.Millisecond
	loops := max(minLoops, 1)
	for time.Duration(loops)*(length+gap) < atLeast {
		loops++
	}
	pause := filepath.Join(dir, "pause.wav")
	if err := os.WriteFile(pause, silence(gap), 0o600); err != nil {
		t.Fatal(err)
	}
	var entries strings.Builder
	for i := 0; i < loops; i++ {
		entries.WriteString("file '" + filepath.Base(speech) + "'\nfile 'pause.wav'\n")
	}
	list := filepath.Join(dir, fmt.Sprintf("list-%d.txt", loops))
	if err := os.WriteFile(list, []byte(entries.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, fmt.Sprintf("long-%d.wav", loops))
	if b, err := exec.Command("ffmpeg", "-v", "error", "-y", "-f", "concat", "-safe", "0", "-i", list, "-c", "copy", "-fflags", "+bitexact", out).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v\n%s", err, b)
	}
	st, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	return out, loops, time.Duration(st.Size()-44) * time.Second / (transcribe.SampleRate * 2)
}

// silence is a WAV of digital silence, in the format the server sends.
func silence(d time.Duration) []byte {
	n := int(d.Seconds() * transcribe.SampleRate)
	var b bytes.Buffer
	le := func(v any) { _ = binary.Write(&b, binary.LittleEndian, v) }
	b.WriteString("RIFF")
	le(uint32(36 + 2*n))
	b.WriteString("WAVEfmt ")
	le(uint32(16))
	le(uint16(1))
	le(uint16(1))
	le(uint32(transcribe.SampleRate))
	le(uint32(transcribe.SampleRate * 2))
	le(uint16(2))
	le(uint16(16))
	b.WriteString("data")
	le(uint32(2 * n))
	b.Write(make([]byte, 2*n))
	return b.Bytes()
}
