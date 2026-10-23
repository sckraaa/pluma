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

// Command alertpush turns Alertmanager webhook notifications into ntfy
// pushes on the operator's phone (ADR 0013). It depends on nothing of the
// messenger: alerts arrive while Pluma itself is down.
//
// Environment:
//
//	NTFY_TOPIC   required: the secret topic the phone subscribes to
//	NTFY_URL     ntfy server, default https://ntfy.sh
//	NTFY_TOKEN   optional access token (Authorization: Bearer)
//	LISTEN       listen address, default :9097
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	cfg, err := configFromEnv(os.Getenv)
	if err != nil {
		log.Fatalf("alertpush: %v", err)
	}
	srv := &http.Server{
		Addr:              cfg.listen,
		Handler:           newHandler(cfg, &http.Client{Timeout: 15 * time.Second}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()

	log.Printf("alertpush: listening on %s, pushing to %s", cfg.listen, cfg.server)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("alertpush: %v", err)
	}
}
