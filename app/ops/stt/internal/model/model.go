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

// Package model reads the description of the speech model the service
// runs: model.yaml in the directory of the model (ADR 0056, p. 3). Another
// model is another directory; nothing about a model is in the code but the
// kinds of models the recognizer can load.
package model

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// File is the name of the description in the directory of a model.
const File = "model.yaml"

// Kind is how the files of a model are loaded.
type Kind string

const (
	// NemoTransducer: encoder, decoder and joiner of a NeMo transducer
	// (GigaAM-v3 rnnt, Parakeet TDT).
	NemoTransducer Kind = "nemo_transducer"
	// NemoCTC: one file of a NeMo CTC model (GigaAM-v3 ctc).
	NemoCTC Kind = "nemo_ctc"
	// Whisper: encoder and decoder of Whisper.
	Whisper Kind = "whisper"
)

const (
	defaultVAD = "silero_vad.onnx"
	// defaultMaxSegment keeps a piece of speech under the 25 seconds
	// GigaAM-v3 takes in one pass, and under the 30 of Whisper.
	defaultMaxSegment = 20
	minSegment        = 5
	maxSegment        = 30
)

var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// Model is a model ready to load: every path is absolute and exists.
type Model struct {
	// Name is what a request names in its "model" field.
	Name string
	Kind Kind
	// Language is the language of the speech, for the models that are told
	// it (Whisper); "" lets such a model detect it.
	Language string

	Encoder string // NemoTransducer, Whisper
	Decoder string // NemoTransducer, Whisper
	Joiner  string // NemoTransducer
	Model   string // NemoCTC
	Tokens  string

	// VAD is the Silero model that cuts the speech into pieces.
	VAD string
	// MaxSegment is the longest piece given to the model, seconds.
	MaxSegment float32
}

type description struct {
	Name     string `yaml:"name"`
	Kind     string `yaml:"kind"`
	Language string `yaml:"language"`
	Files    struct {
		Encoder string `yaml:"encoder"`
		Decoder string `yaml:"decoder"`
		Joiner  string `yaml:"joiner"`
		Model   string `yaml:"model"`
		Tokens  string `yaml:"tokens"`
	} `yaml:"files"`
	VAD struct {
		Model      string  `yaml:"model"`
		MaxSegment float32 `yaml:"max_segment"`
	} `yaml:"vad"`
}

// Load reads model.yaml of dir. A file the description does not name is
// found by its role in the name, as the archives of sherpa-onnx name them
// (encoder.int8.onnx, tiny-decoder.onnx, tokens.txt), the quantized one
// first.
func Load(dir string) (*Model, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, File))
	if err != nil {
		return nil, fmt.Errorf("model: %w", err)
	}
	var d description
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true)
	if err = dec.Decode(&d); err != nil {
		return nil, fmt.Errorf("model: %s: %w", File, err)
	}
	if !nameRe.MatchString(d.Name) {
		return nil, fmt.Errorf("model: %s: name %q: letters, digits, dots, dashes and underscores, up to 64", File, d.Name)
	}
	m := &Model{Name: d.Name, Kind: Kind(d.Kind), Language: d.Language, MaxSegment: d.VAD.MaxSegment}
	if m.MaxSegment == 0 {
		m.MaxSegment = defaultMaxSegment
	}
	if m.MaxSegment < minSegment || m.MaxSegment > maxSegment {
		return nil, fmt.Errorf("model: %s: vad.max_segment %v: from %d to %d seconds", File, m.MaxSegment, minSegment, maxSegment)
	}
	l := locator{dir: dir}
	switch m.Kind {
	case NemoTransducer:
		m.Encoder = l.file("encoder", d.Files.Encoder, "*encoder*.onnx")
		m.Decoder = l.file("decoder", d.Files.Decoder, "*decoder*.onnx")
		m.Joiner = l.file("joiner", d.Files.Joiner, "*joiner*.onnx")
	case NemoCTC:
		m.Model = l.file("model", d.Files.Model, "model*.onnx")
	case Whisper:
		m.Encoder = l.file("encoder", d.Files.Encoder, "*encoder*.onnx")
		m.Decoder = l.file("decoder", d.Files.Decoder, "*decoder*.onnx")
	default:
		return nil, fmt.Errorf("model: %s: kind %q: one of %s, %s, %s", File, d.Kind, NemoTransducer, NemoCTC, Whisper)
	}
	m.Tokens = l.file("tokens", d.Files.Tokens, "*tokens.txt")
	vad := d.VAD.Model
	if vad == "" {
		vad = defaultVAD
	}
	m.VAD = l.file("vad.model", vad, "")
	if l.err != nil {
		return nil, fmt.Errorf("model: %s: %w", File, l.err)
	}
	return m, nil
}

// Speak sets the language of the speech, STT_LANGUAGE of the service, the
// same as bff's: "" keeps the language of model.yaml. Whisper speaks many
// languages and is told this one, whatever its image was built with; a
// model of one language (GigaAM) cannot speak another, and the service
// does not start rather than mishear every record; a model that finds the
// language itself and is not told it (Parakeet TDT v3) takes it as it is.
func (m *Model) Speak(language string) error {
	switch {
	case language == "":
	case m.Kind == Whisper:
		m.Language = language
	case m.Language != "" && !sameLanguage(language, m.Language):
		return fmt.Errorf("model: %s speaks %q, STT_LANGUAGE is %q", m.Name, m.Language, language)
	}
	return nil
}

// sameLanguage compares the primary subtags of two languages ("ru" and
// "ru-RU" are the same).
func sameLanguage(a, b string) bool {
	primary := func(tag string) string {
		tag, _, _ = strings.Cut(strings.TrimSpace(tag), "-")
		return tag
	}
	return strings.EqualFold(primary(a), primary(b))
}

// locator resolves the files of a model and keeps the first error.
type locator struct {
	dir string
	err error
}

// file returns the path of the file with a role: the one named, or the one
// the pattern finds.
func (l *locator) file(role, named, pattern string) string {
	if l.err != nil {
		return ""
	}
	if named == "" {
		named, l.err = discover(l.dir, role, pattern)
		if l.err != nil {
			return ""
		}
	}
	if !filepath.IsLocal(named) {
		l.err = fmt.Errorf("%s: %q is not a path inside the directory of the model", role, named)
		return ""
	}
	path := filepath.Join(l.dir, named)
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() {
		l.err = fmt.Errorf("%s: no file %s", role, path)
		return ""
	}
	return path
}

func discover(dir, role, pattern string) (string, error) {
	found, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(found))
	for _, f := range found {
		names = append(names, filepath.Base(f))
	}
	if quantized := only(names, ".int8."); len(quantized) > 0 {
		names = quantized
	}
	sort.Strings(names)
	switch len(names) {
	case 0:
		return "", fmt.Errorf("%s: no %s in the directory of the model", role, pattern)
	case 1:
		return names[0], nil
	}
	return "", errors.New(role + ": several files fit (" + strings.Join(names, ", ") + "): name one under files")
}

func only(names []string, part string) []string {
	var out []string
	for _, n := range names {
		if strings.Contains(n, part) {
			out = append(out, n)
		}
	}
	return out
}
