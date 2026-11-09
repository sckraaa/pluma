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

// Command codes sends login codes from a real Telegram account, over TDLib
// (task 073, ADR 0042, docs/ops/login-codes.md). A small separate service: it
// holds the account's session in its own volume and has one narrow internal
// API, used by bff.
//
// Usage:
//
//	codes              serve
//	codes login        first login: a QR code on the terminal; the service must be stopped
//	codes healthcheck  for the container's health check
//
// Environment:
//
//	TELEGRAM_API_ID, TELEGRAM_API_HASH  the application at my.telegram.org
//	CODES_API_TOKEN                     bearer token of /v1/*, at least 32 characters
//	CODES_DB_KEY                        encrypts the TDLib database, at least 32 characters
//	CODES_DATA_DIR                      the volume, default /data
//	CODES_LISTEN                        listen address, default :9378
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
	"syscall"
	"time"

	"github.com/teamgram/teamgram-server/app/ops/codes/internal/account"
	"github.com/teamgram/teamgram-server/app/ops/codes/internal/sender"
	"github.com/teamgram/teamgram-server/app/ops/codes/internal/tdjson"
)

func main() {
	args := os.Args[1:]
	var err error
	switch {
	case len(args) == 0:
		err = serve()
	case len(args) == 1 && args[0] == "login":
		err = login()
	case len(args) == 1 && args[0] == "healthcheck":
		err = healthcheck()
	default:
		err = errors.New("usage: codes [login|healthcheck]")
	}
	if err != nil {
		log.Fatalf("codes: %v", err)
	}
}

type config struct {
	apiID    int32
	apiHash  string
	apiToken string
	dbKey    string
	dataDir  string
	listen   string
}

func configFromEnv(getenv func(string) string) (config, error) {
	c := config{
		apiHash:  getenv("TELEGRAM_API_HASH"),
		apiToken: getenv("CODES_API_TOKEN"),
		dbKey:    getenv("CODES_DB_KEY"),
		dataDir:  getenv("CODES_DATA_DIR"),
		listen:   getenv("CODES_LISTEN"),
	}
	if c.dataDir == "" {
		c.dataDir = "/data"
	}
	if c.listen == "" {
		c.listen = ":9378"
	}
	id, err := strconv.ParseInt(getenv("TELEGRAM_API_ID"), 10, 32)
	if err != nil || id <= 0 {
		return c, errors.New("TELEGRAM_API_ID must be the number of the application at my.telegram.org")
	}
	c.apiID = int32(id)
	if c.apiHash == "" {
		return c, errors.New("TELEGRAM_API_HASH is required")
	}
	if len(c.apiToken) < 32 {
		return c, errors.New("CODES_API_TOKEN must be at least 32 characters (openssl rand -hex 32)")
	}
	if len(c.dbKey) < 32 {
		return c, errors.New("CODES_DB_KEY must be at least 32 characters (openssl rand -hex 32)")
	}
	return c, nil
}

func (c config) params() account.Params {
	return account.Params{
		APIID:         c.apiID,
		APIHash:       c.apiHash,
		DatabaseDir:   c.dataDir + "/tdlib",
		EncryptionKey: []byte(c.dbKey),
	}
}

func serve() error {
	cfg, err := configFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	lib, err := tdjson.Open()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.dataDir, 0o700); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	acc := account.New(lib, cfg.params(), nil)
	q, err := sender.Open(acc, sender.DefaultLimits, sender.StatePath(cfg.dataDir))
	if err != nil {
		return err
	}

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		acc.Run(ctx)
	}()
	go q.Run(ctx)
	go publishLoop(ctx, q)

	srv := &http.Server{
		Addr:              cfg.listen,
		Handler:           newHandler(q, cfg.apiToken),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdown); err != nil {
			log.Printf("codes: shutdown: %v", err)
		}
	}()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	// TDLib flushes its database on close: wait for it.
	select {
	case <-stopped:
	case <-time.After(20 * time.Second):
		log.Printf("codes: TDLib did not close in time")
	}
	return nil
}

// publishLoop keeps the gauges current with no code going through.
func publishLoop(ctx context.Context, q *sender.Queue) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		q.Publish()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// login is the first login of the account. It runs instead of the service
// (TDLib holds a lock on its database) and exits when the account is
// authorized, keeping the session in the volume.
func login() error {
	cfg, err := configFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	lib, err := tdjson.Open()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.dataDir, 0o700); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	done := make(chan error, 1)
	acc := account.New(lib, cfg.params(), &account.Login{
		ShowQR:   showQR,
		Password: readPassword,
		Done: func(err error) {
			select {
			case done <- err:
			default:
			}
		},
	})
	runCtx, stopRun := context.WithCancel(context.Background())
	defer stopRun()
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		acc.Run(runCtx)
	}()

	select {
	case err = <-done:
	case <-ctx.Done():
		err = errors.New("the login was interrupted or took too long")
	}
	stopRun()
	select {
	case <-stopped:
	case <-time.After(20 * time.Second):
		return errors.New("TDLib did not close in time; check the state with docker compose logs codes")
	}
	if err != nil {
		return err
	}
	fmt.Println("Готово: аккаунт авторизован, сессия сохранена. Запустите сервис.")
	return nil
}

func healthcheck() error {
	port := os.Getenv("CODES_LISTEN")
	if port == "" {
		port = ":9378"
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1" + port + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}
