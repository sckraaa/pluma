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

package wav

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

type header struct {
	format, channels uint16
	rate             uint32
	bits             uint16
	// dataSize is written as the size of the samples; nil: the real one.
	dataSize *uint32
	// list adds a LIST chunk before the samples, as ffmpeg does.
	list bool
}

func pcm16() header { return header{format: 1, channels: 1, rate: SampleRate, bits: 16} }

func file(h header, samples []int16) []byte {
	var b bytes.Buffer
	le := func(v any) { _ = binary.Write(&b, binary.LittleEndian, v) }
	b.WriteString("RIFF")
	le(uint32(0xffffffff))
	b.WriteString("WAVE")
	b.WriteString("fmt ")
	le(uint32(16))
	le(h.format)
	le(h.channels)
	le(h.rate)
	le(h.rate * uint32(h.channels) * uint32(h.bits) / 8)
	le(h.channels * h.bits / 8)
	le(h.bits)
	if h.list {
		b.WriteString("LIST")
		le(uint32(5))
		b.WriteString("INFOx\x00") // an odd size is padded
	}
	b.WriteString("data")
	size := uint32(len(samples) * 2)
	if h.dataSize != nil {
		size = *h.dataSize
	}
	le(size)
	le(samples)
	return b.Bytes()
}

func readAll(t *testing.T, r *Reader, window int) []float32 {
	t.Helper()
	var out []float32
	buf := make([]float32, window)
	for {
		n, err := r.Read(buf)
		out = append(out, buf[:n]...)
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		if n != window {
			t.Fatalf("a short read of %d without the end", n)
		}
	}
}

func TestReadsSamples(t *testing.T) {
	samples := []int16{0, 16384, -16384, 32767, -32768}
	unknown := uint32(sizeUnknown)
	zero := uint32(0)
	for name, h := range map[string]header{
		"a plain file":                 pcm16(),
		"a LIST chunk before the data": {format: 1, channels: 1, rate: SampleRate, bits: 16, list: true},
		"a size not known (a pipe)":    {format: 1, channels: 1, rate: SampleRate, bits: 16, dataSize: &unknown},
		"a size of zero (a pipe)":      {format: 1, channels: 1, rate: SampleRate, bits: 16, dataSize: &zero},
	} {
		t.Run(name, func(t *testing.T) {
			r, err := NewReader(bytes.NewReader(file(h, samples)))
			if err != nil {
				t.Fatal(err)
			}
			got := readAll(t, r, 2)
			want := []float32{0, 0.5, -0.5, 32767.0 / 32768, -1}
			if len(got) != len(want) {
				t.Fatalf("got %v", got)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("sample %d: got %v, want %v", i, got[i], want[i])
				}
			}
			if r.Samples() != int64(len(want)) {
				t.Errorf("counted %d samples", r.Samples())
			}
		})
	}
}

func TestStopsAtTheEndOfTheData(t *testing.T) {
	b := file(pcm16(), []int16{1, 2, 3})
	b = append(b, "tail of another chunk"...)
	r, err := NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if got := readAll(t, r, 512); len(got) != 3 {
		t.Fatalf("read %d samples past the data", len(got))
	}
}

func TestACutStreamGivesWhatItHas(t *testing.T) {
	b := file(pcm16(), []int16{1, 2, 3, 4})
	r, err := NewReader(bytes.NewReader(b[:len(b)-3])) // two samples and a half
	if err != nil {
		t.Fatal(err)
	}
	if got := readAll(t, r, 512); len(got) != 2 {
		t.Fatalf("got %d samples", len(got))
	}
}

func TestRefusesOtherFormats(t *testing.T) {
	cases := map[string][]byte{
		"not RIFF":         []byte("OggS\x00\x02 and the rest of an Ogg page"),
		"empty":            nil,
		"stereo":           file(header{format: 1, channels: 2, rate: SampleRate, bits: 16}, []int16{1, 2}),
		"44.1 kHz":         file(header{format: 1, channels: 1, rate: 44100, bits: 16}, []int16{1, 2}),
		"8 bit":            file(header{format: 1, channels: 1, rate: SampleRate, bits: 8}, []int16{1, 2}),
		"float":            file(header{format: 3, channels: 1, rate: SampleRate, bits: 16}, []int16{1, 2}),
		"no data chunk":    file(pcm16(), nil)[:36],
		"data before fmt ": append([]byte("RIFF\xff\xff\xff\xffWAVEdata\x04\x00\x00\x00"), 1, 2, 3, 4),
	}
	for name, b := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := NewReader(bytes.NewReader(b)); !errors.Is(err, ErrFormat) {
				t.Fatalf("got %v", err)
			}
		})
	}
}
