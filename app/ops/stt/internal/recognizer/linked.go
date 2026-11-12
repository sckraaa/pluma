//go:build sherpa

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

package recognizer

import (
	"context"
	"errors"
	"fmt"
	"io"

	sherpa "github.com/k2-fsa/sherpa-onnx-go-linux"

	"github.com/teamgram/teamgram-server/app/ops/stt/internal/model"
	"github.com/teamgram/teamgram-server/app/ops/stt/internal/wav"
)

const (
	// vadWindow is the window Silero VAD takes at 16 kHz, samples.
	vadWindow = 512
	// vadBuffer is how much sound the detector keeps while a piece of
	// speech is open, seconds: more than the longest piece. The pieces are
	// taken out and recognized as soon as they close, so this and one
	// piece are all the sound in memory, whatever the length of the record.
	vadBuffer = 60
)

type sherpaRecognizer struct {
	rec *sherpa.OfflineRecognizer
	vad sherpa.VadModelConfig
}

// Open loads the model with threads threads of computation.
func Open(m *model.Model, threads int) (Recognizer, error) {
	c := sherpa.OfflineRecognizerConfig{
		FeatConfig:     sherpa.FeatureConfig{SampleRate: wav.SampleRate, FeatureDim: 80},
		DecodingMethod: "greedy_search",
		ModelConfig: sherpa.OfflineModelConfig{
			Tokens:     m.Tokens,
			NumThreads: threads,
			Provider:   "cpu",
			ModelType:  string(m.Kind),
		},
	}
	switch m.Kind {
	case model.NemoTransducer:
		c.ModelConfig.Transducer = sherpa.OfflineTransducerModelConfig{Encoder: m.Encoder, Decoder: m.Decoder, Joiner: m.Joiner}
	case model.NemoCTC:
		c.ModelConfig.NemoCTC = sherpa.OfflineNemoEncDecCtcModelConfig{Model: m.Model}
	case model.Whisper:
		c.ModelConfig.Whisper = sherpa.OfflineWhisperModelConfig{
			Encoder: m.Encoder, Decoder: m.Decoder, Language: m.Language, Task: "transcribe", TailPaddings: -1,
		}
	default:
		return nil, fmt.Errorf("recognizer: kind %q", m.Kind)
	}
	rec := sherpa.NewOfflineRecognizer(&c)
	if rec == nil {
		return nil, fmt.Errorf("recognizer: sherpa-onnx did not load the model %s", m.Name)
	}
	r := &sherpaRecognizer{rec: rec, vad: sherpa.VadModelConfig{
		SileroVad: sherpa.SileroVadModelConfig{
			Model:              m.VAD,
			Threshold:          0.5,
			MinSilenceDuration: 0.25,
			MinSpeechDuration:  0.25,
			WindowSize:         vadWindow,
			MaxSpeechDuration:  m.MaxSegment,
		},
		SampleRate: wav.SampleRate,
		NumThreads: 1,
		Provider:   "cpu",
	}}
	// The detector is made for every request; make one now, so that a
	// missing or broken file stops the start and not the first request.
	vad := sherpa.NewVoiceActivityDetector(&r.vad, vadBuffer)
	if vad == nil {
		sherpa.DeleteOfflineRecognizer(rec)
		return nil, fmt.Errorf("recognizer: sherpa-onnx did not load the voice detector %s", m.VAD)
	}
	sherpa.DeleteVoiceActivityDetector(vad)
	return r, nil
}

func (r *sherpaRecognizer) Close() {
	sherpa.DeleteOfflineRecognizer(r.rec)
}

func (r *sherpaRecognizer) Transcribe(ctx context.Context, src Samples, watch Watch) (string, error) {
	vad := sherpa.NewVoiceActivityDetector(&r.vad, vadBuffer)
	if vad == nil {
		return "", errors.New("recognizer: no voice detector")
	}
	defer sherpa.DeleteVoiceActivityDetector(vad)

	var pieces []string
	drain := func() error {
		for !vad.IsEmpty() {
			if err := ctx.Err(); err != nil {
				return err
			}
			segment := vad.Front()
			vad.Pop()
			if len(segment.Samples) == 0 {
				continue
			}
			stop := watch(PieceLimit(len(segment.Samples)))
			pieces = append(pieces, r.decode(segment.Samples))
			stop()
		}
		return nil
	}
	window := make([]float32, vadWindow)
	for {
		n, err := src.Read(window)
		if n > 0 {
			// The last window is padded with silence.
			clear(window[n:])
			vad.AcceptWaveform(window)
			if derr := drain(); derr != nil {
				return "", derr
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
	}
	vad.Flush()
	if err := drain(); err != nil {
		return "", err
	}
	return join(pieces), nil
}

func (r *sherpaRecognizer) decode(samples []float32) string {
	stream := sherpa.NewOfflineStream(r.rec)
	defer sherpa.DeleteOfflineStream(stream)
	stream.AcceptWaveform(wav.SampleRate, samples)
	r.rec.Decode(stream)
	return stream.GetResult().Text
}
