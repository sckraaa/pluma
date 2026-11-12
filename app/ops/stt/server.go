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
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/teamgram/teamgram-server/app/ops/stt/internal/recognizer"
	"github.com/teamgram/teamgram-server/app/ops/stt/internal/wav"
)

// maxField bounds a field of the form that is not the sound.
const maxField = 4 << 10

// idleRead is how long the service waits for the next bytes of a request.
// The sound is read as it is recognized, so the request as a whole has no
// time limit: a record of any length takes as long as it takes, and a
// caller that stops sending is let go after this. A variable for the tests.
var idleRead = time.Minute

// The alerts on them are in teamgramd/deploy/prometheus/server/rules/stt.yml.
var (
	metricRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "stt_requests_total",
		Help: "Requests to transcribe, by result.",
	}, []string{"result"})
	metricAudio = promauto.NewCounter(prometheus.CounterOpts{
		Name: "stt_audio_seconds_total",
		Help: "Seconds of sound transcribed.",
	})
	metricRecognition = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "stt_recognition_seconds",
		Help:    "Time one request spent in the model.",
		Buckets: []float64{0.25, 0.5, 1, 2, 5, 10, 20, 40, 80, 160, 320},
	})
	metricModel = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "stt_model_info",
		Help: "1 for the model the service runs.",
	}, []string{"model", "kind"})
)

// service is the HTTP side of one loaded model.
type service struct {
	// model is the name a request must give.
	model string
	// language is the language of the model's speech, from its model.yaml;
	// "" for a model that finds it itself. A model is built for its
	// language, so a request for another one is refused, not ignored.
	language string
	// token is the bearer token of /v1/*; none if empty.
	token string
	rec   recognizer.Recognizer
	// slot holds the one recognition that runs: the model computes on all
	// the threads it was given, and the queue is the caller's.
	slot chan struct{}
	// watch starts the timer of one call into the model and returns its
	// stop. A call into the model cannot be interrupted, so the timer ends
	// the process and the container is started again.
	watch recognizer.Watch
}

func newService(model, language, token string, rec recognizer.Recognizer, watch recognizer.Watch) *service {
	return &service{model: model, language: language, token: token, rec: rec, slot: make(chan struct{}, 1), watch: watch}
}

// handler is the API: POST /v1/audio/transcriptions as OpenAI defines it,
// for a WAV file only (ADR 0056, p. 2). Neither the sound nor the text is
// logged or written anywhere.
func (s *service) handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "model": s.model})
	})
	mux.HandleFunc("POST /v1/audio/transcriptions", s.transcribe)
	return mux
}

func (s *service) transcribe(w http.ResponseWriter, r *http.Request) {
	if s.token != "" {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
			s.fail(w, http.StatusUnauthorized, "unauthorized", "the bearer token is wrong")
			return
		}
	}
	select {
	case s.slot <- struct{}{}:
		defer func() { <-s.slot }()
	default:
		w.Header().Set("Retry-After", "1")
		s.fail(w, http.StatusServiceUnavailable, "busy", "another recognition is running")
		return
	}

	// The form is read as a stream and the sound recognized as it comes:
	// nothing of it is kept but the piece of speech in the model, so a
	// record of any length fits the memory of the container. The fields
	// that come before the sound are checked before it is heard, those
	// after it once it is.
	r.Body = &idleBody{ReadCloser: r.Body, rc: http.NewResponseController(w)}
	// A refusal in the middle of the form still reads the rest of it (the
	// answer is sent when the handler returns): a caller that is still
	// writing the sound gets the answer, not a broken connection it would
	// take for a failure of the engine.
	defer func() { _, _ = io.Copy(io.Discard, r.Body) }()
	form, err := r.MultipartReader()
	if err != nil {
		s.fail(w, http.StatusBadRequest, "bad_request", "not a multipart form")
		return
	}
	fields := map[string]string{}
	var (
		heard   bool
		text    string
		seconds float64
		took    time.Duration
	)
	for {
		part, err := form.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			s.broken(w, r, err, "not a multipart form")
			return
		}
		switch name := part.FormName(); {
		case name == "file" && heard:
			s.fail(w, http.StatusBadRequest, "bad_request", "one file only")
			return
		case name == "file":
			if status, result, message := s.check(fields, false); status != 0 {
				s.fail(w, status, result, message)
				return
			}
			heard = true
			var ok bool
			if text, seconds, took, ok = s.hear(w, r, part); !ok {
				return
			}
		case name != "":
			value, err := io.ReadAll(io.LimitReader(part, maxField+1))
			if err != nil {
				s.broken(w, r, err, "a field of the form is cut short")
				return
			}
			if len(value) > maxField {
				s.fail(w, http.StatusBadRequest, "bad_request", "a field of the form is too long")
				return
			}
			fields[name] = string(value)
		}
	}
	if !heard {
		s.fail(w, http.StatusBadRequest, "bad_request", "the file field is required")
		return
	}
	if status, result, message := s.check(fields, true); status != 0 {
		s.fail(w, status, result, message)
		return
	}
	metricRequests.WithLabelValues("ok").Inc()
	metricAudio.Add(seconds)
	metricRecognition.Observe(took.Seconds())
	log.Printf("stt: %.1f s of sound in %.2f s", seconds, took.Seconds()) //nolint:gosec // G706: two numbers, no text of the request
	writeJSON(w, http.StatusOK, map[string]string{"text": text})
}

// check answers the fields of a request that are not the sound: the status,
// the result and the message of a refusal, or 0. complete: every field is
// in, so a missing one is an error.
func (s *service) check(fields map[string]string, complete bool) (int, string, string) {
	switch name, ok := fields["model"]; {
	case !ok && complete, ok && name == "":
		return http.StatusBadRequest, "bad_request", "the model field is required"
	case ok && name != s.model:
		return http.StatusNotFound, "unknown_model", "this service runs the model " + s.model
	}
	if language := fields["language"]; !sameLanguage(language, s.language) {
		// A mistake of the configuration (STT_LANGUAGE), like another
		// model: 404, never a refusal of the sound.
		log.Printf("stt: a request for another language than the model's %q (STT_LANGUAGE)", s.language)
		return http.StatusNotFound, "unknown_language", "this service runs a model of the language " + s.language
	}
	if format := fields["response_format"]; format != "" && format != "json" {
		return http.StatusBadRequest, "bad_request", "response_format: only json"
	}
	return 0, "", ""
}

// hear recognizes the sound of part as it is read. ok is false when the
// request has been answered (or its caller is gone).
func (s *service) hear(w http.ResponseWriter, r *http.Request, part io.Reader) (text string, seconds float64, took time.Duration, ok bool) {
	body := &readErr{r: part}
	samples, err := wav.NewReader(body)
	if err != nil {
		if body.err != nil {
			s.broken(w, r, body.err, "the sound is cut short")
			return "", 0, 0, false
		}
		s.fail(w, http.StatusBadRequest, "bad_sound", err.Error())
		return "", 0, 0, false
	}
	start := time.Now()
	text, err = s.rec.Transcribe(r.Context(), samples, s.watch)
	took = time.Since(start)
	seconds = float64(samples.Samples()) / wav.SampleRate
	switch {
	case body.err != nil:
		s.broken(w, r, body.err, "the sound stopped coming")
		return "", 0, 0, false
	case err != nil && r.Context().Err() != nil:
		// The caller gave up; nobody reads the answer.
		metricRequests.WithLabelValues("canceled").Inc()
		log.Printf("stt: the caller left after %.1f s of sound in %.1f s", seconds, took.Seconds())
		return "", 0, 0, false
	case err != nil:
		log.Printf("stt: recognition of %.1f s of sound: %v", seconds, err)
		s.fail(w, http.StatusInternalServerError, "error", "the recognition failed")
		return "", 0, 0, false
	}
	return text, seconds, took, true
}

// broken answers a request whose body could not be read: 408 when the
// caller stopped sending (it may try again: the sound is not to blame), and
// nothing when it is gone.
func (s *service) broken(w http.ResponseWriter, r *http.Request, err error, message string) {
	switch {
	case errors.Is(err, os.ErrDeadlineExceeded):
		s.fail(w, http.StatusRequestTimeout, "timeout", message)
	case r.Context().Err() != nil, errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, net.ErrClosed):
		metricRequests.WithLabelValues("canceled").Inc()
		log.Printf("stt: the caller left: %s", message)
	default:
		s.fail(w, http.StatusBadRequest, "bad_request", message)
	}
}

// readErr keeps the error of reading the body, which the WAV reader takes
// for the end of the sound: a sound cut short by the network is no sound
// to answer.
type readErr struct {
	r   io.Reader
	err error
}

func (b *readErr) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	if err != nil && !errors.Is(err, io.EOF) && b.err == nil {
		b.err = err
	}
	return n, err
}

// idleBody gives every read of the request idleRead: the time the model
// spends between reads is not the caller's.
type idleBody struct {
	io.ReadCloser
	rc *http.ResponseController
}

func (b *idleBody) Read(p []byte) (int, error) {
	// A recorder of the tests has no connection to set it on.
	_ = b.rc.SetReadDeadline(time.Now().Add(idleRead))
	return b.ReadCloser.Read(p)
}

// sameLanguage reports whether the language a request asks for is the
// model's: none asked, a model of any language, or the same primary
// subtag ("ru" and "ru-RU").
func sameLanguage(asked, model string) bool {
	if asked == "" || model == "" {
		return true
	}
	primary := func(tag string) string {
		tag, _, _ = strings.Cut(strings.TrimSpace(tag), "-")
		return tag
	}
	return strings.EqualFold(primary(asked), primary(model))
}

// fail answers with an error in the shape of the OpenAI API.
func (s *service) fail(w http.ResponseWriter, status int, result, message string) {
	metricRequests.WithLabelValues(result).Inc()
	writeJSON(w, status, map[string]any{"error": map[string]string{"message": message, "type": result}})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("stt: write a response: %v", err)
	}
}

// shutdown stops srv when ctx is done, waiting for the request in flight.
func shutdown(ctx context.Context, srv *http.Server, wait time.Duration) {
	<-ctx.Done()
	c, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	if err := srv.Shutdown(c); err != nil {
		log.Printf("stt: shutdown: %v", err)
	}
}
