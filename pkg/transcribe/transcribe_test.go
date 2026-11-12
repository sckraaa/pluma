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
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testToken = "0123456789abcdef0123456789abcdef"

// got is what the engine of a test received.
type got struct {
	auth, model, language, format, fileName string
	file                                    []byte
	fields                                  []string // in the order they came
}

func engineAt(t *testing.T, c Config, answer func(w http.ResponseWriter, g got)) (Engine, *got) {
	t.Helper()
	var seen got
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/audio/transcriptions" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		g := got{auth: r.Header.Get("Authorization")}
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			t.Errorf("content type: %v", err)
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Errorf("form: %v", err)
				break
			}
			body, _ := io.ReadAll(part)
			g.fields = append(g.fields, part.FormName())
			switch part.FormName() {
			case "model":
				g.model = string(body)
			case "language":
				g.language = string(body)
			case "response_format":
				g.format = string(body)
			case "file":
				g.file, g.fileName = body, part.FileName()
			}
		}
		seen = g
		answer(w, g)
	}))
	t.Cleanup(srv.Close)
	c.URL = srv.URL + "/" // a trailing slash in the configuration is fine
	return NewHTTPEngine(c), &seen
}

func TestHTTPEngineSendsTheFormAndReadsTheText(t *testing.T) {
	e, seen := engineAt(t, Config{Token: testToken, Model: "gigaam-v3-e2e-rnnt", Language: "ru"}, func(w http.ResponseWriter, _ got) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"text":"  Привет, мир.\n","duration":2.0}`)
	})
	text, err := e.Transcribe(context.Background(), strings.NewReader("RIFF....WAVE"), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if text != "Привет, мир." {
		t.Errorf("text %q", text)
	}
	if seen.auth != "Bearer "+testToken || seen.model != "gigaam-v3-e2e-rnnt" || seen.language != "ru" || seen.format != "json" {
		t.Errorf("sent %+v", seen)
	}
	if string(seen.file) != "RIFF....WAVE" || !strings.HasSuffix(seen.fileName, ".wav") {
		t.Errorf("file %q named %q", seen.file, seen.fileName)
	}
	// The sound goes last: an engine that reads the form as a stream knows
	// the model before the first sample.
	if seen.fields[len(seen.fields)-1] != "file" {
		t.Errorf("order %v", seen.fields)
	}
}

func TestHTTPEngineWithoutTokenAndLanguage(t *testing.T) {
	e, seen := engineAt(t, Config{Model: "m"}, func(w http.ResponseWriter, _ got) {
		_, _ = io.WriteString(w, `{"text":""}`)
	})
	text, err := e.Transcribe(context.Background(), strings.NewReader("x"), time.Second)
	if err != nil || text != "" {
		t.Fatalf("an empty text is no speech, not an error: %q, %v", text, err)
	}
	if seen.auth != "" {
		t.Errorf("Authorization %q without a token", seen.auth)
	}
	for _, f := range seen.fields {
		if f == "language" {
			t.Error("a language field without a language")
		}
	}
}

func TestHTTPEngineErrors(t *testing.T) {
	status := func(code int) func(http.ResponseWriter, got) {
		return func(w http.ResponseWriter, _ got) { http.Error(w, `{"error":{"message":"x"}}`, code) }
	}
	body := func(s string) func(http.ResponseWriter, got) {
		return func(w http.ResponseWriter, _ got) { _, _ = io.WriteString(w, s) }
	}
	cases := []struct {
		name   string
		answer func(http.ResponseWriter, got)
		want   error
		// waits: the engine cannot take the sound now, which is not an
		// attempt on the record (ErrBusy or ErrDown).
		busy, down bool
	}{
		// The sound cannot be read: final.
		{"400", status(400), ErrRejected, false, false},
		{"415", status(415), ErrRejected, false, false},
		{"422", status(422), ErrRejected, false, false},
		// The configuration, until it is fixed: the record is not to blame.
		{"401 a wrong token", status(401), ErrUnavailable, false, true},
		{"403", status(403), ErrUnavailable, false, true},
		{"404 an unknown model", status(404), ErrUnavailable, false, true},
		// Busy with others.
		{"429", status(429), ErrUnavailable, true, false},
		{"503 busy", status(503), ErrUnavailable, true, false},
		// The engine took the sound and came to nothing: an attempt.
		{"413 longer than the engine takes", status(413), ErrUnavailable, false, false},
		{"500", status(500), ErrUnavailable, false, false},
		{"502", status(502), ErrUnavailable, false, false},
		{"200 that is not JSON", body("<html>proxy</html>"), ErrUnavailable, false, false},
		{"200 without a text", body(`{"result":"ok"}`), ErrUnavailable, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e, _ := engineAt(t, Config{Model: "m"}, c.answer)
			_, err := e.Transcribe(context.Background(), strings.NewReader("x"), time.Second)
			if !errors.Is(err, c.want) {
				t.Fatalf("got %v, want %v", err, c.want)
			}
			if errors.Is(err, ErrBusy) != c.busy || errors.Is(err, ErrDown) != c.down {
				t.Errorf("busy %v, down %v: %v", c.busy, c.down, err)
			}
			if c.want == ErrUnavailable && errors.Is(err, ErrRejected) {
				t.Errorf("a failure that may pass reads as final: %v", err)
			}
		})
	}
}

// zeros is a long sound that counts how much of it was read.
type zeros struct{ left, read int64 }

func (z *zeros) Read(p []byte) (int, error) {
	if z.left == 0 {
		return 0, io.EOF
	}
	n := int64(len(p))
	if n > z.left {
		n = z.left
	}
	clear(p[:n])
	z.left -= n
	z.read += n
	return int(n), nil
}

// A busy engine answers before the sound is sent: an hour of it is not put
// on the wire for every 503, and the answer is "busy", never a broken
// write that would count as an attempt.
func TestBusyEngineIsAnsweredBeforeTheSound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Expect") != "100-continue" {
			t.Errorf("Expect %q", r.Header.Get("Expect"))
		}
		http.Error(w, `{"error":{"message":"busy"}}`, http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	e := NewHTTPEngine(Config{URL: srv.URL, Model: "m"})
	for i := 0; i < 3; i++ {
		hour := &zeros{left: 3600 * SampleRate * 2}
		_, err := e.Transcribe(context.Background(), hour, time.Hour)
		if !errors.Is(err, ErrBusy) {
			t.Fatalf("got %v", err)
		}
		if hour.read > 1<<20 {
			t.Fatalf("%d bytes of the sound were read for a 503", hour.read)
		}
	}
}

func TestHTTPEngineUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	_, err := NewHTTPEngine(Config{URL: url, Model: "m"}).Transcribe(context.Background(), strings.NewReader("x"), time.Second)
	// Nothing listens: the engine is down, which is no attempt on the record.
	if !errors.Is(err, ErrDown) || errors.Is(err, ErrBusy) {
		t.Fatalf("got %v", err)
	}
}

func TestHTTPEngineGivesUpWithItsContext(t *testing.T) {
	release := make(chan struct{})
	e, _ := engineAt(t, Config{Model: "m"}, func(_ http.ResponseWriter, _ got) { <-release })
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := e.Transcribe(ctx, strings.NewReader("x"), time.Second)
	// An engine that took the sound and did not answer in time: an attempt.
	if !errors.Is(err, ErrUnavailable) || errors.Is(err, ErrDown) || errors.Is(err, ErrBusy) {
		t.Fatalf("got %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("gave up after %s", time.Since(start))
	}
}

func TestEngineTimeoutGrowsWithTheRecord(t *testing.T) {
	d := Config{}.EngineTimeout()
	if got := d.For(0); got != 30*time.Second {
		t.Errorf("an empty record: %s", got)
	}
	if got := d.For(10 * time.Minute); got != 5*time.Minute+30*time.Second {
		t.Errorf("ten minutes: %s", got)
	}
	// A slower engine is given its time by the configuration alone.
	slow := Config{Timeout: "2m+3x"}.EngineTimeout()
	if got := slow.For(10 * time.Minute); got != 32*time.Minute {
		t.Errorf("a slow engine, ten minutes: %s", got)
	}
	for _, ok := range []string{"30s+0.5x", " 1m + 2x ", "1s+0x", "1h+20x"} {
		if _, err := ParseTimeout(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"30s", "0.5x", "30s+0.5", "x+0.5x", "500ms+1x", "2h+1x", "30s+-1x", "30s+21x", "30s+NaNx", "30s+Infx"} {
		if _, err := ParseTimeout(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestHTTPEngineGivesTheConfiguredTime(t *testing.T) {
	cfg := Config{URL: "http://stt:9379", Model: "m", Timeout: "1s+20x"}.WithDefaults()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	e, ok := NewHTTPEngine(cfg).(*httpEngine)
	if !ok {
		t.Fatal("not the HTTP engine")
	}
	if got := e.timeout.For(2 * time.Second); got != 41*time.Second {
		t.Fatalf("the engine of %q has %s for two seconds", cfg.Timeout, got)
	}
}

func TestAfterFailure(t *testing.T) {
	cases := []struct {
		attempts  int
		permanent bool
		retryIn   time.Duration
		final     bool
	}{
		{1, false, 5 * time.Second, false},
		{2, false, 30 * time.Second, false},
		{3, false, 2 * time.Minute, false},
		{4, false, 0, true},
		{5, false, 0, true},
		// A record that cannot be read is never tried again.
		{1, true, 0, true},
	}
	for _, c := range cases {
		retryIn, final := AfterFailure(c.attempts, 4, c.permanent)
		if retryIn != c.retryIn || final != c.final {
			t.Errorf("attempt %d (permanent %v): %s, final %v", c.attempts, c.permanent, retryIn, final)
		}
	}
	if _, final := AfterFailure(2, 2, false); !final {
		t.Error("MaxAttempts 2 allows a third attempt")
	}
}

func TestConfig(t *testing.T) {
	var off Config
	if off.Enabled() || off.WithDefaults().Validate() != nil {
		t.Fatal("an empty configuration is transcription off, not an error")
	}
	d := off.WithDefaults()
	if d.SyncWait != 3*time.Second || d.DeliverDelay != time.Second ||
		d.Workers != 1 || d.MaxAttempts != 4 || d.FailedTTL != 10*time.Minute {
		t.Fatalf("defaults %+v", d)
	}
	ok := Config{URL: "http://stt:9379", Model: "gigaam-v3-e2e-rnnt", Token: testToken}.WithDefaults()
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	set := Config{URL: "http://stt:9379", Model: "m", MaxAttempts: 2, Workers: 2}.WithDefaults()
	if set.MaxAttempts != 2 || set.Workers != 2 || set.Validate() != nil {
		t.Fatalf("set values are kept: %+v", set)
	}
	for name, bad := range map[string]Config{
		"a URL without a model": {URL: "http://stt:9379"},
		"a short token":         {URL: "http://stt:9379", Model: "m", Token: "short"},
		"not a URL":             {URL: "stt:9379", Model: "m"},
		"a long model name":     {URL: "http://stt:9379", Model: strings.Repeat("m", 65)},
		"too many attempts":     {URL: "http://stt:9379", Model: "m", MaxAttempts: 9},
		"a timeout without x":   {URL: "http://stt:9379", Model: "m", Timeout: "30s+1"},
	} {
		if err := bad.WithDefaults().Validate(); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// Probe sends the configuration with a sound of no samples and gives the
// engine's answer: ErrDown while it refuses the model.
func TestProbe(t *testing.T) {
	var fixed atomic.Bool
	e, seen := engineAt(t, Config{Model: "the-model"}, func(w http.ResponseWriter, g got) {
		if !fixed.Load() || g.model != "the-model" {
			http.Error(w, "unknown model", http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, `{"text":""}`)
	})
	if err := Probe(context.Background(), e); !errors.Is(err, ErrDown) {
		t.Fatalf("a refused model: %v", err)
	}
	if string(seen.file) != string(emptyWAV()) {
		t.Fatalf("sent %q", seen.file)
	}
	fixed.Store(true)
	if err := Probe(context.Background(), e); err != nil {
		t.Fatalf("a model it runs: %v", err)
	}
}
