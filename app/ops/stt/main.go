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

// Command stt turns speech into text for voice transcription (task 090,
// ADR 0056, docs/ops/transcription.md). A small separate service: it holds
// one speech model in memory and has one endpoint, the transcription
// endpoint of the OpenAI API, which bff calls. The model is a directory
// with model.yaml; another model is another directory or another image, and
// any other service with the same endpoint can stand in for this one.
//
// Usage:
//
//	stt              serve
//	stt healthcheck  for the container's health check
//
// Environment:
//
//	STT_MODEL_DIR    the directory of the model, default /model
//	STT_THREADS      threads of computation, default 2
//	STT_LISTEN       listen address, default :9379
//	STT_API_TOKEN    bearer token of /v1/*, at least 32 characters; none if empty
//	STT_LANGUAGE     the language of the speech (the same as bff's): what a
//	                 Whisper model is told; a model of one language
//	                 (model.yaml) refuses to start with another one
//
// A record of any length is taken: the sound is read and recognized a
// piece at a time, and the memory does not grow with it.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/teamgram/teamgram-server/app/ops/stt/internal/model"
	"github.com/teamgram/teamgram-server/app/ops/stt/internal/recognizer"
)

const defaultListen = ":9379"

func main() {
	args := os.Args[1:]
	var err error
	switch {
	case len(args) == 0:
		err = serve()
	case len(args) == 1 && args[0] == "healthcheck":
		err = healthcheck()
	default:
		err = errors.New("usage: stt [healthcheck]")
	}
	if err != nil {
		log.Fatalf("stt: %v", err)
	}
}

type config struct {
	modelDir string
	threads  int
	listen   string
	token    string
	language string
}

func configFromEnv(getenv func(string) string) (config, error) {
	c := config{
		modelDir: getenv("STT_MODEL_DIR"),
		threads:  2,
		listen:   getenv("STT_LISTEN"),
		token:    getenv("STT_API_TOKEN"),
		language: strings.TrimSpace(getenv("STT_LANGUAGE")),
	}
	if c.modelDir == "" {
		c.modelDir = "/model"
	}
	if c.listen == "" {
		c.listen = defaultListen
	}
	var err error
	if v := getenv("STT_THREADS"); v != "" {
		if c.threads, err = strconv.Atoi(v); err != nil || c.threads < 1 || c.threads > 64 {
			return c, errors.New("STT_THREADS must be a number from 1 to 64")
		}
	}
	if c.token != "" && len(c.token) < 32 {
		return c, errors.New("STT_API_TOKEN must be at least 32 characters (openssl rand -hex 32)")
	}
	return c, nil
}

func serve() error {
	cfg, err := configFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	m, err := model.Load(cfg.modelDir)
	if err != nil {
		return err
	}
	if err = m.Speak(cfg.language); err != nil {
		return err
	}
	start := time.Now()
	rec, err := recognizer.Open(m, cfg.threads)
	if err != nil {
		return err
	}
	defer rec.Close()
	metricModel.WithLabelValues(m.Name, string(m.Kind)).Set(1)
	log.Printf("stt: model %s (%s) loaded in %.1f s, %d threads", m.Name, m.Kind, time.Since(start).Seconds(), cfg.threads)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	s := newService(m.Name, m.Language, cfg.token, rec, exitWhenStuck)
	// No ReadTimeout or WriteTimeout: they would bound the whole request,
	// and a record of any length is read while it is recognized. A caller
	// that stops sending is let go by idleRead, a model that hangs by
	// exitWhenStuck, a caller that gives up by its own deadline.
	srv := &http.Server{
		Addr:              cfg.listen,
		Handler:           s.handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	go shutdown(ctx, srv, 20*time.Second)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// exitWhenStuck ends the process when a call into the model takes longer
// than limit: it cannot be interrupted, and the container is started again
// (restart: unless-stopped).
func exitWhenStuck(limit time.Duration) func() {
	t := time.AfterFunc(limit, func() {
		log.Printf("stt: a piece of speech did not end in the model in %s, exiting", limit)
		os.Exit(3)
	})
	return func() { t.Stop() }
}

func healthcheck() error {
	listen := os.Getenv("STT_LISTEN")
	if listen == "" {
		listen = defaultListen
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1" + listen + "/healthz") //nolint:gosec // G704: an address of the configuration, not of a request
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}
