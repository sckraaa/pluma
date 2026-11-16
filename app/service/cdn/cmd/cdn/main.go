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

// Command cdn is a CDN node of Pluma (task 102, ADR 0065): it serves the
// cipher of large files to clients over MTProto (upload.getCdnFile) and
// keeps it on its disk; the home pushes, checks and drops the files over
// the control API. The node never holds the files' keys.
package main

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/zeromicro/go-zero/core/logx"

	"github.com/teamgram/teamgram-server/app/service/cdn/internal/config"
	"github.com/teamgram/teamgram-server/app/service/cdn/internal/control"
	"github.com/teamgram/teamgram-server/app/service/cdn/internal/listen"
	"github.com/teamgram/teamgram-server/app/service/cdn/internal/metrics"
	"github.com/teamgram/teamgram-server/app/service/cdn/internal/nodekey"
	"github.com/teamgram/teamgram-server/app/service/cdn/internal/session"
	"github.com/teamgram/teamgram-server/app/service/cdn/internal/store"
	"github.com/teamgram/teamgram-server/pkg/conf"
	"github.com/teamgram/teamgram-server/pkg/mtserver/handshake"
	"github.com/teamgram/teamgram-server/pkg/serverkey"
)

var configFile = flag.String("f", "etc/cdn.yaml", "the config file")

// maintainEvery is how often idle files and expired keys are dropped and
// the index and the traffic count written.
const maintainEvery = time.Minute

func main() {
	flag.Parse()
	var c config.Config
	conf.MustLoad(*configFile, &c)
	logx.MustSetup(c.Log)
	if err := run(c); err != nil {
		logx.Errorf("cdn: %v", err)
		logx.Close()
		os.Exit(1)
	}
	logx.Close()
}

func run(c config.Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	secret, err := c.ParseSecret()
	if err != nil {
		return err
	}
	tlsConfig, err := control.ServerTLS(control.TLSFiles{
		CertFile:     c.Control.CertFile,
		KeyFile:      c.Control.KeyFile,
		ClientCAFile: c.Control.ClientCAFile,
	})
	if err != nil {
		return err
	}

	key, err := nodekey.LoadOrCreate(c.Store.DataDir)
	if err != nil {
		return err
	}
	files, err := store.Open(store.Config{
		Dir:         c.Store.DataDir,
		CacheBytes:  c.Store.CacheBytes,
		IdleTTL:     c.Store.IdleTTL,
		DiskReserve: c.Store.DiskReserveBytes,
	})
	if err != nil {
		return err
	}
	metrics.CacheCap.Set(float64(c.Store.CacheBytes))
	egress, err := store.OpenEgress(c.Store.DataDir, nil)
	if err != nil {
		return err
	}
	bootID := make([]byte, 16)
	if _, err := rand.Read(bootID); err != nil {
		return err
	}

	hs, err := handshake.New([]*serverkey.Key{key.Key})
	if err != nil {
		return err
	}
	tokens := &session.RequestTokens{}
	keys := session.NewKeyStore(c.MTProto.MaxAuthKeys, nil)
	sess, err := session.New(keys, files, tokens, nil)
	if err != nil {
		return err
	}
	port, err := listen.New(listen.Config{
		Secret:                  secret,
		MaxConnsPerIP:           c.MTProto.MaxConnsPerIP,
		KeyExchangesPerIPMinute: c.MTProto.KeyExchangesPerIPMinute,
		MaxInFlight:             c.MTProto.MaxInFlight,
		IdleTimeout:             c.MTProto.IdleTimeout,
		HandshakeWorkers:        max(2, runtime.NumCPU()),
		Egress:                  egress.Add,
	}, hs, sess)
	if err != nil {
		return err
	}

	clientLn, err := net.Listen("tcp", c.MTProto.ListenOn)
	if err != nil {
		return err
	}
	controlLn, err := net.Listen("tcp", c.Control.ListenOn)
	if err != nil {
		_ = clientLn.Close()
		return err
	}
	grpcServer := control.NewGRPCServer(tlsConfig, control.NewService(c.DcId, key, bootID, files, egress, tokens))

	errCh := make(chan error, 3)
	go func() { errCh <- port.Serve(clientLn) }()
	go func() { errCh <- grpcServer.Serve(controlLn) }()
	var metricsServer *http.Server
	if c.MetricsListenOn != "" {
		h := http.NewServeMux()
		h.Handle("/metrics", promhttp.Handler())
		metricsServer = &http.Server{Addr: c.MetricsListenOn, Handler: h, ReadHeaderTimeout: 5 * time.Second}
		go func() {
			if err := metricsServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- err
			}
		}()
	}

	maintain := func() {
		keys.Sweep()
		if err := files.Maintain(); err != nil {
			logx.Errorf("cdn: %v", err)
		}
		if _, _, err := files.Disk(); err != nil {
			logx.Errorf("cdn: free space: %v", err)
		}
		if err := egress.Flush(); err != nil {
			logx.Errorf("cdn: %v", err)
		}
	}
	maintain()
	ticker := time.NewTicker(maintainEvery)
	defer ticker.Stop()

	used, n := files.Usage()
	logx.Infof("cdn: dc %d, clients on %s, control on %s, %d files (%d bytes) of %d, key fingerprint %d",
		c.DcId, c.MTProto.ListenOn, c.Control.ListenOn, n, used, c.Store.CacheBytes, int64(key.Fingerprint))

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
loop:
	for {
		select {
		case <-stop:
			break loop
		case err = <-errCh:
			break loop
		case <-ticker.C:
			maintain()
		}
	}

	// Clients go first: downloads in flight finish from the home. A Put in
	// flight is cut and leaves nothing (the home pushes it again).
	_ = port.Close()
	stopped := make(chan struct{})
	go func() {
		grpcServer.GracefulStop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		grpcServer.Stop()
	}
	if metricsServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = metricsServer.Shutdown(ctx)
	}
	maintain()
	return err
}
