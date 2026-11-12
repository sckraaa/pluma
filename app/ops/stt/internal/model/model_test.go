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

package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func modelDir(t *testing.T, yaml string, files ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, f := range append(files, "silero_vad.onnx") {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, File), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoadFindsTheFilesOfATransducer(t *testing.T) {
	// The archive of GigaAM-v3 rnnt, as sherpa-onnx ships it.
	dir := modelDir(t, "name: gigaam-v3-e2e-rnnt\nkind: nemo_transducer\nlanguage: \"ru\"\n",
		"encoder.int8.onnx", "decoder.onnx", "joiner.onnx", "tokens.txt", "LICENSE")
	m, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "gigaam-v3-e2e-rnnt" || m.Kind != NemoTransducer || m.Language != "ru" {
		t.Fatalf("got %+v", m)
	}
	for role, got := range map[string]string{
		"encoder.int8.onnx": m.Encoder, "decoder.onnx": m.Decoder, "joiner.onnx": m.Joiner,
		"tokens.txt": m.Tokens, "silero_vad.onnx": m.VAD,
	} {
		if got != filepath.Join(dir, role) {
			t.Errorf("%s: got %q", role, got)
		}
	}
	if m.MaxSegment != defaultMaxSegment {
		t.Errorf("max segment %v", m.MaxSegment)
	}
}

func TestLoadPrefersTheQuantizedFile(t *testing.T) {
	dir := modelDir(t, "name: ctc\nkind: nemo_ctc\n", "model.onnx", "model.int8.onnx", "tokens.txt")
	m, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(m.Model) != "model.int8.onnx" {
		t.Fatalf("model %q", m.Model)
	}
}

func TestLoadWhisperWithNamedFiles(t *testing.T) {
	dir := modelDir(t, `name: whisper-tiny
kind: whisper
files:
  encoder: tiny-encoder.int8.onnx
  decoder: tiny-decoder.int8.onnx
  tokens: tiny-tokens.txt
vad:
  max_segment: 28
`, "tiny-encoder.int8.onnx", "tiny-decoder.int8.onnx", "tiny-encoder.onnx", "tiny-decoder.onnx", "tiny-tokens.txt")
	m, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.Kind != Whisper || filepath.Base(m.Encoder) != "tiny-encoder.int8.onnx" || filepath.Base(m.Tokens) != "tiny-tokens.txt" || m.MaxSegment != 28 {
		t.Fatalf("got %+v", m)
	}
}

func TestLoadRefuses(t *testing.T) {
	transducer := []string{"encoder.onnx", "decoder.onnx", "joiner.onnx", "tokens.txt"}
	cases := []struct {
		name, yaml, want string
		files            []string
	}{
		{"no name", "kind: nemo_ctc\n", "name", []string{"model.onnx", "tokens.txt"}},
		{"a name with a slash", "name: a/b\nkind: nemo_ctc\n", "name", []string{"model.onnx", "tokens.txt"}},
		{"an unknown kind", "name: m\nkind: paraformer\n", "kind", []string{"model.onnx", "tokens.txt"}},
		{"an unknown key", "name: m\nkind: nemo_ctc\nthreads: 2\n", "threads", []string{"model.onnx", "tokens.txt"}},
		{"a missing file", "name: m\nkind: nemo_transducer\n", "joiner", []string{"encoder.onnx", "decoder.onnx", "tokens.txt"}},
		{"two files for one role", "name: m\nkind: nemo_transducer\n", "several", append([]string{"encoder-2.onnx"}, transducer...)},
		{"a file outside", "name: m\nkind: nemo_ctc\nfiles:\n  model: ../model.onnx\n", "inside", []string{"tokens.txt"}},
		{"a segment too long", "name: m\nkind: nemo_ctc\nvad:\n  max_segment: 90\n", "max_segment", []string{"model.onnx", "tokens.txt"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Load(modelDir(t, c.yaml, c.files...))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want an error about %q", err, c.want)
			}
		})
	}
}

func TestLoadWithoutTheVoiceDetector(t *testing.T) {
	dir := modelDir(t, "name: m\nkind: nemo_ctc\n", "model.onnx", "tokens.txt")
	if err := os.Remove(filepath.Join(dir, "silero_vad.onnx")); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "vad.model") {
		t.Fatalf("got %v", err)
	}
}

func TestSpeak(t *testing.T) {
	cases := []struct {
		name         string
		kind         Kind
		model, asked string
		want         string
		failsToStart bool
	}{
		{"nothing asked", NemoTransducer, "ru", "", "ru", false},
		{"the language of the model", NemoTransducer, "ru", "ru-RU", "ru", false},
		// GigaAM hears Russian only: a service that would mishear every
		// record does not start.
		{"another language than the model's", NemoTransducer, "ru", "en", "", true},
		// Whisper is told the language of the configuration, whatever its
		// image was built with.
		{"Whisper of the image's language", Whisper, "ru", "en", "en", false},
		{"Whisper that detects it", Whisper, "", "kk", "kk", false},
		// A model that finds the language itself is not told one.
		{"a model of many languages", NemoTransducer, "", "en", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := &Model{Name: "m", Kind: c.kind, Language: c.model}
			err := m.Speak(c.asked)
			if (err != nil) != c.failsToStart {
				t.Fatalf("err %v", err)
			}
			if err == nil && m.Language != c.want {
				t.Fatalf("language %q, want %q", m.Language, c.want)
			}
		})
	}
}
