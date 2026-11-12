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

package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os/exec"
	"strconv"
	"time"
)

// The waveform of a voice message: the clients draw it, and an export does
// not hold it (Data::Document, tdesktop export_data_types.h:495-519), so it
// is counted again from the file, the way Telegram Desktop counts it for a
// voice message it sends (FFMpegWaveformCounter, media/audio/
// media_audio.cpp:1552-1636): the sound cut into waveformSamples equal
// spans, the peak of each, scaled to 0..31 against 1.8 times the mean peak
// (at least 2500 of 32767), then packed 5 bits a sample
// (documentWaveformEncode5bit, data/data_document.cpp:1444-1462).

const (
	// waveformSamples is Media::Player::kWaveformSamplesCount.
	waveformSamples = 100
	// waveformRate is the rate the sound is decoded at: enough for the
	// peaks of speech, small enough for an hour of it.
	waveformRate = 16000
	// waveformWindow is how many decoded samples make one stored peak
	// (10 ms): the spans are made of whole windows.
	waveformWindow = waveformRate / 100
	// waveformTimeout bounds the decoding of one file.
	waveformTimeout = 2 * time.Minute
)

// voiceWaveform returns the waveform of the audio file at path, nil when
// ffmpeg is not there, cannot decode the file, or the file is too short for
// one: a voice message without a waveform is drawn flat by the clients.
func voiceWaveform(ctx context.Context, path string) []byte {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, waveformTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-nostdin", "-i", path, "-vn", "-ac", "1", "-ar", strconv.Itoa(waveformRate), "-f", "s16le", "-")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil
	}
	if err = cmd.Start(); err != nil {
		return nil
	}
	peaks, err := windowPeaks(bufio.NewReaderSize(out, 64<<10))
	if werr := cmd.Wait(); werr != nil || err != nil {
		return nil
	}
	return encodeWaveform(waveformOf(peaks))
}

// windowPeaks reads signed 16-bit little-endian samples and returns the
// peak of each window of waveformWindow of them.
func windowPeaks(r io.Reader) ([]uint16, error) {
	var peaks []uint16
	buf := make([]byte, 2*waveformWindow)
	for {
		n, err := io.ReadFull(r, buf)
		if n >= 2 {
			peak := uint16(0)
			for i := 0; i+1 < n; i += 2 {
				s := int32(int16(binary.LittleEndian.Uint16(buf[i:])))
				if s < 0 {
					s = -s
				}
				peak = max(peak, uint16(min(s, 32767)))
			}
			peaks = append(peaks, peak)
		}
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return peaks, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

// waveformOf makes the waveformSamples values 0..31 of the peaks of a
// sound, nil for a sound shorter than that many windows.
func waveformOf(peaks []uint16) []byte {
	if len(peaks) < waveformSamples {
		return nil
	}
	spans := make([]uint16, waveformSamples)
	sum := int64(0)
	for i := range spans {
		from, to := i*len(peaks)/waveformSamples, (i+1)*len(peaks)/waveformSamples
		for _, p := range peaks[from:to] {
			spans[i] = max(spans[i], p)
		}
		sum += int64(spans[i])
	}
	top := max(int64(float64(sum)*1.8/waveformSamples), 2500)
	out := make([]byte, waveformSamples)
	for i, p := range spans {
		out[i] = byte(min(31, min(int64(p), top)*31/top))
	}
	return out
}

// encodeWaveform packs values 0..31 five bits each, the first in the low
// bits of the first byte, as documentAttributeAudio.waveform holds them.
func encodeWaveform(values []byte) []byte {
	if len(values) == 0 {
		return nil
	}
	out := make([]byte, (len(values)*5+7)/8+1)
	for i, v := range values {
		at, shift := i*5/8, uint(i*5%8)
		w := uint16(v&0x1F) << shift
		out[at] |= byte(w)
		out[at+1] |= byte(w >> 8)
	}
	return out[:len(out)-1]
}
