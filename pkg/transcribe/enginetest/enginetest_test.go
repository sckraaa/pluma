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

package enginetest

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/teamgram-server/pkg/transcribe"
)

// TestEngine checks the engine named in the environment:
//
//	STT_ENGINE_URL       its base URL; without it the test is skipped
//	STT_ENGINE_MODEL     the model name it answers to
//	STT_ENGINE_TOKEN     its bearer token, if it has one
//	STT_ENGINE_LANGUAGE  the language of the sample, for engines told it
//	STT_ENGINE_TIMEOUT   the STT_TIMEOUT the server would give it, if not the default
//	STT_ENGINE_SAMPLE    a record of speech, any format ffmpeg reads
//	STT_ENGINE_EXPECT    words of the sample, comma separated
//	STT_ENGINE_STRICT    1 for our own service (app/ops/stt)
//	STT_ENGINE_LONG      also one record at least that long, as 25m
func TestEngine(t *testing.T) {
	url := os.Getenv("STT_ENGINE_URL")
	if url == "" {
		t.Skip("STT_ENGINE_URL is not set: no engine to check")
	}
	sample := os.Getenv("STT_ENGINE_SAMPLE")
	if sample == "" {
		t.Fatal("STT_ENGINE_SAMPLE: a record of speech is needed")
	}
	var expect []string
	for _, w := range strings.Split(os.Getenv("STT_ENGINE_EXPECT"), ",") {
		if w = strings.TrimSpace(w); w != "" {
			expect = append(expect, w)
		}
	}
	var long time.Duration
	if v := os.Getenv("STT_ENGINE_LONG"); v != "" {
		var err error
		if long, err = time.ParseDuration(v); err != nil {
			t.Fatalf("STT_ENGINE_LONG: %v", err)
		}
	}
	Run(t, Target{
		Config: transcribe.Config{
			URL:      url,
			Model:    os.Getenv("STT_ENGINE_MODEL"),
			Token:    os.Getenv("STT_ENGINE_TOKEN"),
			Language: os.Getenv("STT_ENGINE_LANGUAGE"),
			Timeout:  os.Getenv("STT_ENGINE_TIMEOUT"),
		},
		Sample: sample,
		Expect: expect,
		Strict: os.Getenv("STT_ENGINE_STRICT") == "1",
		Long:   long,
	})
}
