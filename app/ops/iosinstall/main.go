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

// Command iosinstall installs Pluma on iPhones from the site, without the
// App Store (task 029): it learns a new iPhone's UDID from a Profile Service
// profile, registers it with Apple, issues the ad hoc profiles anew and
// re-signs the build CI uploaded, then serves the install link. It holds
// the team's signing certificate and App Store Connect key, so it runs in
// a container of its own with only its own secrets.
//
// Environment (files are mounted secrets):
//
//	PUBLIC_URL                 the site, https://pluma.chat
//	PAGE_URL                   the page a device returns to, default PUBLIC_URL/iphone/install
//	ASC_KEY_ID, ASC_ISSUER     the App Store Connect API key (Admin)
//	ASC_KEY_FILE               its .p8
//	SIGN_CERTIFICATE_ID        the Apple Distribution certificate's id at Apple
//	SIGN_P12_FILE              that certificate and its key
//	SIGN_P12_PASSWORD_FILE     the .p12's password
//	UPLOAD_TOKEN_FILE          the token CI uploads builds with
//	DATA_DIR                   state and IPAs, default /data
//	RCODESIGN                  default /usr/local/bin/rcodesign
//	LISTEN                     default :9098
//	LIMIT_ADDRESS_DAY          profiles per address a day, default 5
//	LIMIT_DEVICES_DAY          new iPhones a day, default 10
package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	// The container's health check: the image has no shell or wget.
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck(or(os.Getenv("LISTEN"), ":9098")))
	}
	cfg, err := configFromEnv(os.Getenv)
	if err != nil {
		log.Fatalf("iosinstall: %v", err)
	}
	keyPEM, err := os.ReadFile(cfg.ascKeyFile)
	if err != nil {
		log.Fatalf("iosinstall: ASC_KEY_FILE: %v", err)
	}
	asc, err := newASCClient(keyPEM, cfg.ascKeyID, cfg.ascIssuer)
	if err != nil {
		log.Fatalf("iosinstall: %v", err)
	}
	for _, dir := range []string{"builds", "signed", "work"} {
		if err := os.MkdirAll(cfg.dataDir+"/"+dir, 0o700); err != nil {
			log.Fatalf("iosinstall: %v", err)
		}
	}
	st, err := openStore(cfg.dataDir)
	if err != nil {
		log.Fatalf("iosinstall: %v", err)
	}
	sg := newSigner(st, asc, cfg)
	srv := &http.Server{
		Addr:              cfg.listen,
		Handler:           newServer(cfg, st, sg).routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Minute, // a build upload
		WriteTimeout:      30 * time.Minute, // an IPA to a slow phone
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go sg.loop(ctx)
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()

	log.Printf("iosinstall: listening on %s for %s", cfg.listen, cfg.publicURL)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("iosinstall: %v", err)
	}
}

func healthcheck(listen string) int {
	_, port, err := net.SplitHostPort(listen)
	if err != nil {
		return 1
	}
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://127.0.0.1:" + port + "/healthz") //nolint:gosec // G704: our own port on loopback
	if err != nil {
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return 1
	}
	return 0
}
