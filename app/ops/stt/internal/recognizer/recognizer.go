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

// Package recognizer turns speech into text with the model of the service.
// The implementation links sherpa-onnx (build tag "sherpa", as the image is
// built); without the tag the package compiles and Open says so, which
// keeps cgo and the shared libraries out of every other build.
package recognizer

import (
	"context"
	"errors"
	"strings"
	"time"
)

// ErrNotLinked: this binary has no sherpa-onnx in it.
var ErrNotLinked = errors.New("recognizer: built without sherpa-onnx (build with -tags sherpa)")

// Samples gives speech as 16 kHz mono floats in [-1, 1): Read fills out and
// returns fewer than len(out) only together with io.EOF.
type Samples interface {
	Read(out []float32) (int, error)
}

// Watch starts the timer of one call into the model, which may take up to
// limit, and returns its stop. A call into the model cannot be
// interrupted: what to do when the timer fires is the caller's.
type Watch func(limit time.Duration) (stop func())

// Recognizer is a loaded model. One Transcribe at a time.
type Recognizer interface {
	// Transcribe returns the text of the speech in src, "" when there is
	// none. The speech is read as it comes and recognized a piece at a
	// time, so the memory does not grow with its length: a record of any
	// length is taken. Every piece is recognized under watch. It stops
	// between pieces when ctx is done; a piece that is being recognized
	// cannot be interrupted.
	Transcribe(ctx context.Context, src Samples, watch Watch) (string, error)
	Close()
}

// pieceStuck is the time one piece of speech may take in the model beyond
// four times its length. A model on a CPU runs many times faster than the
// speech; this is for a hang, not for load.
const pieceStuck = time.Minute

// PieceLimit is how long the model may take for a piece of samples
// samples before it is stuck.
func PieceLimit(samples int) time.Duration {
	return pieceStuck + 4*time.Duration(samples)*time.Second/16000
}

// join makes one text of the texts of the pieces of speech.
func join(pieces []string) string {
	kept := pieces[:0:0]
	for _, p := range pieces {
		if p = strings.TrimSpace(p); p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, " ")
}
