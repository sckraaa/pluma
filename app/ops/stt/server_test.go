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
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teamgram/teamgram-server/app/ops/stt/internal/recognizer"
)

const testToken = "0123456789abcdef0123456789abcdef"

// fakeModel answers text for any sound and counts the samples it was given.
type fakeModel struct {
	text    string
	err     error
	samples atomic.Int64
	// entered is closed when a recognition has started; release lets it end.
	entered, release chan struct{}
}

func (f *fakeModel) Transcribe(ctx context.Context, src recognizer.Samples, watch recognizer.Watch) (string, error) {
	if f.entered != nil {
		close(f.entered)
		select {
		case <-f.release:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	// Pieces of ten seconds, each under the watch, as the model is called.
	buf := make([]float32, 10*16000)
	for {
		n, err := src.Read(buf)
		f.samples.Add(int64(n))
		if n > 0 {
			watch(recognizer.PieceLimit(n))()
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
	}
	return f.text, f.err
}

func (f *fakeModel) Close() {}

func wavOf(seconds float64) []byte {
	n := int(seconds * 16000)
	var b bytes.Buffer
	le := func(v any) { _ = binary.Write(&b, binary.LittleEndian, v) }
	b.WriteString("RIFF")
	le(uint32(36 + 2*n))
	b.WriteString("WAVEfmt ")
	le(uint32(16))
	le(uint16(1))
	le(uint16(1))
	le(uint32(16000))
	le(uint32(32000))
	le(uint16(2))
	le(uint16(16))
	b.WriteString("data")
	le(uint32(2 * n))
	b.Write(make([]byte, 2*n))
	return b.Bytes()
}

func form(t *testing.T, fields map[string]string, file []byte) (io.Reader, string) {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	if file != nil {
		part, err := w.CreateFormFile("file", "audio.wav")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = part.Write(file); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &b, w.FormDataContentType()
}

type testService struct {
	*service
	watched []time.Duration
	stopped int
}

func newTestService(m recognizer.Recognizer, token string) *testService {
	ts := &testService{}
	ts.service = newService("the-model", "ru", token, m, func(limit time.Duration) func() {
		ts.watched = append(ts.watched, limit)
		return func() { ts.stopped++ }
	})
	return ts
}

func post(t *testing.T, s *service, token string, fields map[string]string, file []byte) *httptest.ResponseRecorder {
	t.Helper()
	body, contentType := form(t, fields, file)
	req := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", body)
	req.Header.Set("Content-Type", contentType)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	return rec
}

func TestTranscribes(t *testing.T) {
	m := &fakeModel{text: "Привет, мир."}
	s := newTestService(m, testToken)
	rec := post(t, s.service, testToken, map[string]string{"model": "the-model", "response_format": "json", "language": "ru"}, wavOf(2))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var out struct {
		Text *string `json:"text"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Text == nil || *out.Text != "Привет, мир." {
		t.Fatalf("answer %s (%v)", rec.Body, err)
	}
	if got := m.samples.Load(); got != 2*16000 {
		t.Errorf("the model got %d samples", got)
	}
	// The call into the model ran under the timer of its piece.
	if len(s.watched) != 1 || s.watched[0] != recognizer.PieceLimit(2*16000) || s.stopped != 1 {
		t.Errorf("watched %v, stopped %d", s.watched, s.stopped)
	}
}

func TestLanguageOfTheModel(t *testing.T) {
	for _, language := range []string{"", "ru", "RU", "ru-RU"} {
		rec := post(t, newTestService(&fakeModel{text: "Да."}, "").service, "", map[string]string{"model": "the-model", "language": language}, wavOf(1))
		if rec.Code != http.StatusOK {
			t.Errorf("%q: status %d: %s", language, rec.Code, rec.Body)
		}
	}
	if !sameLanguage("en", "") {
		t.Error("a model that finds the language itself refused one")
	}
}

func TestSilenceIsAnEmptyText(t *testing.T) {
	rec := post(t, newTestService(&fakeModel{}, "").service, "", map[string]string{"model": "the-model"}, wavOf(1))
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"text":""}` {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

func TestRefusals(t *testing.T) {
	ok := map[string]string{"model": "the-model"}
	cases := []struct {
		name   string
		token  string
		fields map[string]string
		file   []byte
		want   int
	}{
		{"no token", "", ok, wavOf(1), http.StatusUnauthorized},
		{"a wrong token", strings.Repeat("x", 32), ok, wavOf(1), http.StatusUnauthorized},
		// Another model is a mistake of the configuration, which the
		// caller must be able to tell from a sound it cannot read.
		{"another model", testToken, map[string]string{"model": "whisper-1"}, wavOf(1), http.StatusNotFound},
		{"no model", testToken, map[string]string{}, wavOf(1), http.StatusBadRequest},
		{"no file", testToken, ok, nil, http.StatusBadRequest},
		{"not a WAV", testToken, ok, []byte("OggS\x00\x02 an Ogg page, which the caller should have decoded"), http.StatusBadRequest},
		{"a format it does not write", testToken, map[string]string{"model": "the-model", "response_format": "srt"}, wavOf(1), http.StatusBadRequest},
		// The model speaks another language: the configuration again.
		{"another language", testToken, map[string]string{"model": "the-model", "language": "en"}, wavOf(1), http.StatusNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := &fakeModel{text: "never"}
			rec := post(t, newTestService(m, testToken).service, c.token, c.fields, c.file)
			if rec.Code != c.want {
				t.Fatalf("status %d, want %d: %s", rec.Code, c.want, rec.Body)
			}
			// Without a model field before the sound, the sound is heard
			// before the form is known to be wrong.
			if m.samples.Load() != 0 && c.name != "no model" {
				t.Error("the model was given the sound")
			}
			var out struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Error.Message == "" {
				t.Errorf("the error is not in the shape of the API: %s", rec.Body)
			}
		})
	}
}

func TestBusyWhileAnotherRuns(t *testing.T) {
	m := &fakeModel{text: "first", entered: make(chan struct{}), release: make(chan struct{})}
	s := newTestService(m, "")
	first := make(chan *httptest.ResponseRecorder, 1)
	go func() { first <- post(t, s.service, "", map[string]string{"model": "the-model"}, wavOf(1)) }()
	<-m.entered

	rec := post(t, s.service, "", map[string]string{"model": "the-model"}, wavOf(1))
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("the second request: status %d, Retry-After %q", rec.Code, rec.Header().Get("Retry-After"))
	}
	close(m.release)
	if rec := <-first; rec.Code != http.StatusOK {
		t.Fatalf("the first request: status %d", rec.Code)
	}
	// The slot is free again.
	m.entered = nil
	if rec := post(t, s.service, "", map[string]string{"model": "the-model"}, wavOf(1)); rec.Code != http.StatusOK {
		t.Fatalf("after the first ended: status %d", rec.Code)
	}
}

func TestModelFailureIsNotTheSoundsFault(t *testing.T) {
	rec := post(t, newTestService(&fakeModel{err: errors.New("boom")}, "").service, "", map[string]string{"model": "the-model"}, wavOf(1))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestHealth(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestService(&fakeModel{}, testToken).handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "the-model") {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

func TestConfigFromEnv(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	c, err := configFromEnv(env(nil))
	if err != nil || c.modelDir != "/model" || c.threads != 2 || c.listen != defaultListen || c.token != "" || c.language != "" {
		t.Fatalf("defaults: %+v, %v", c, err)
	}
	c, err = configFromEnv(env(map[string]string{"STT_THREADS": "4", "STT_API_TOKEN": testToken, "STT_MODEL_DIR": "/m", "STT_LANGUAGE": " ru "}))
	if err != nil || c.threads != 4 || c.token != testToken || c.modelDir != "/m" || c.language != "ru" {
		t.Fatalf("set: %+v, %v", c, err)
	}
	for _, bad := range []map[string]string{
		{"STT_THREADS": "0"}, {"STT_THREADS": "many"}, {"STT_API_TOKEN": "short"},
	} {
		if _, err := configFromEnv(env(bad)); err == nil {
			t.Errorf("%v was accepted", bad)
		}
	}
}

// streamedForm writes a form with the fields in the order given and a WAV
// of seconds of silence, made as it is read: the request is never whole in
// memory on this side either.
func streamedForm(t *testing.T, before, after map[string]string, seconds int) (io.Reader, string) {
	t.Helper()
	pr, pw := io.Pipe()
	w := multipart.NewWriter(pw)
	go func() {
		write := func() error {
			for k, v := range before {
				if err := w.WriteField(k, v); err != nil {
					return err
				}
			}
			part, err := w.CreateFormFile("file", "audio.wav")
			if err != nil {
				return err
			}
			head := wavOf(0)
			n := seconds * 16000
			binary.LittleEndian.PutUint32(head[4:], uint32(36+2*n))
			binary.LittleEndian.PutUint32(head[40:], uint32(2*n))
			if _, err = part.Write(head); err != nil {
				return err
			}
			second := make([]byte, 2*16000)
			for i := 0; i < seconds; i++ {
				if _, err = part.Write(second); err != nil {
					return err
				}
			}
			for k, v := range after {
				if err = w.WriteField(k, v); err != nil {
					return err
				}
			}
			return w.Close()
		}
		pw.CloseWithError(write())
	}()
	return pr, w.FormDataContentType()
}

// A record of any length is taken, and the service does not hold it: what
// it allocates does not grow with the sound (the owner's decision of
// 2026-11-12; the whole form in memory was the limit before).
func TestALongRecordIsStreamed(t *testing.T) {
	const seconds = 30 * 60 // 57.6 MB of sound
	m := &fakeModel{text: "Полчаса."}
	s := newTestService(m, "")
	body, contentType := streamedForm(t, map[string]string{"model": "the-model"}, nil, seconds)
	req := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	s.handler().ServeHTTP(rec, req)
	runtime.ReadMemStats(&after)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if got := m.samples.Load(); got != seconds*16000 {
		t.Fatalf("the model got %d samples of %d", got, seconds*16000)
	}
	// The test's own writer and the model's pieces of ten seconds are in
	// this too; the sound itself is 57.6 MB.
	if grew := after.TotalAlloc - before.TotalAlloc; grew > 16<<20 {
		t.Fatalf("%d MB allocated for a record of %d MB", grew>>20, 2*seconds*16000>>20)
	}
	if len(s.watched) != seconds/10 {
		t.Errorf("%d calls into the model under the watch, want %d", len(s.watched), seconds/10)
	}
}

// The fields may come after the sound (the order of a form is the
// caller's): they are checked once it is heard.
func TestFieldsAfterTheSound(t *testing.T) {
	for name, c := range map[string]struct {
		after map[string]string
		want  int
	}{
		"the model":        {map[string]string{"model": "the-model", "response_format": "json"}, http.StatusOK},
		"another model":    {map[string]string{"model": "whisper-1"}, http.StatusNotFound},
		"another language": {map[string]string{"model": "the-model", "language": "en"}, http.StatusNotFound},
		"no model at all":  {nil, http.StatusBadRequest},
	} {
		t.Run(name, func(t *testing.T) {
			body, contentType := streamedForm(t, nil, c.after, 3)
			req := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", body)
			req.Header.Set("Content-Type", contentType)
			rec := httptest.NewRecorder()
			newTestService(&fakeModel{text: "Да."}, "").handler().ServeHTTP(rec, req)
			if rec.Code != c.want {
				t.Fatalf("status %d, want %d: %s", rec.Code, c.want, rec.Body)
			}
		})
	}
}

// A caller that stops sending is let go, with an answer it may try again
// on: the sound is not to blame.
func TestACallerThatStopsSending(t *testing.T) {
	srv := httptest.NewServer(newTestService(&fakeModel{text: "never"}, "").handler())
	defer srv.Close()
	pr, pw := io.Pipe()
	defer pw.Close()
	w := multipart.NewWriter(pw)
	go func() {
		_ = w.WriteField("model", "the-model")
		part, _ := w.CreateFormFile("file", "audio.wav")
		_, _ = part.Write(wavOf(1)[:1000]) // and then nothing more
	}()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/audio/transcriptions", pr)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	idle := idleRead
	t.Cleanup(func() { idleRead = idle })
	idleRead = 300 * time.Millisecond
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestTimeout {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

// A sound of no samples, which bff sends to learn whether an engine that
// refused the configuration takes it again: the answer is the
// configuration's, at the cost of a header.
func TestASoundOfNoSamples(t *testing.T) {
	m := &fakeModel{}
	s := newTestService(m, "").service
	if rec := post(t, s, "", map[string]string{"model": "the-model"}, wavOf(0)); rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"text":""}` {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if rec := post(t, s, "", map[string]string{"model": "whisper-1"}, wavOf(0)); rec.Code != http.StatusNotFound {
		t.Fatalf("another model: status %d: %s", rec.Code, rec.Body)
	}
	if n := m.samples.Load(); n != 0 {
		t.Fatalf("%d samples", n)
	}
}
