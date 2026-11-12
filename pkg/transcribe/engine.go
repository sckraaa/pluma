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
	"errors"
	"fmt"
	"io"
	"time"
)

// Engine turns speech into text. It is the seam for the tests and for an
// engine with another protocol; the one implementation is NewHTTPEngine.
type Engine interface {
	// Transcribe returns the text of wav: RIFF/WAVE, PCM signed 16 bit
	// little endian, 16 kHz, mono, duration long. "" means no speech. An
	// error wraps ErrUnavailable (try again later) or ErrRejected (this
	// sound will never be taken).
	Transcribe(ctx context.Context, wav io.Reader, duration time.Duration) (string, error)
}

var (
	// ErrUnavailable: the engine did not answer, or answered that it
	// cannot now. The same sound may be sent again.
	ErrUnavailable = errors.New("transcribe: the engine is unavailable")
	// ErrBusy is ErrUnavailable of an engine that is transcribing
	// something else (503, 429); it says nothing about this sound.
	ErrBusy = fmt.Errorf("%w: busy", ErrUnavailable)
	// ErrDown is ErrUnavailable of an engine that cannot be used at all
	// now: nothing answers at its address, or it refuses the token, the
	// model or the language of the configuration (401, 403, 404). It says
	// nothing about this sound either; it lasts until the engine is up or
	// the configuration is fixed.
	ErrDown = fmt.Errorf("%w: down", ErrUnavailable)
	// ErrRejected: the engine cannot read this sound.
	ErrRejected = errors.New("transcribe: the engine rejected the sound")
)

// Probe asks e to transcribe a sound of no samples: whether it takes the
// configuration (the token, the model, the language) and has a slot, for
// the cost of a header. After an engine was ErrDown, the record is sent
// again only once a probe is not: an engine that refuses the model or the
// language may read the fields only as the sound arrives and the sound to
// its end before it answers (app/ops/stt does), and an hour of it is 115
// MB on the wire for every refusal.
func Probe(ctx context.Context, e Engine) error {
	_, err := e.Transcribe(ctx, bytes.NewReader(emptyWAV()), 0)
	return err
}
