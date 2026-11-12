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

// Command sfu is the selective forwarding unit of group calls (ADR 0059):
// it connects the clients of a call and forwards their sound and video.
// The signalling side drives it over the gRPC control API.
package main

import (
	"context"
	"errors"
	"flag"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/zeromicro/go-zero/core/logx"

	"github.com/teamgram/teamgram-server/app/service/sfu/internal/config"
	"github.com/teamgram/teamgram-server/app/service/sfu/internal/control"
	"github.com/teamgram/teamgram-server/app/service/sfu/internal/room"
	"github.com/teamgram/teamgram-server/app/service/sfu/internal/transport"
	"github.com/teamgram/teamgram-server/pkg/conf"
)

var configFile = flag.String("f", "etc/sfu.yaml", "the config file")

func main() {
	flag.Parse()
	var c config.Config
	conf.MustLoad(*configFile, &c)
	logx.MustSetup(c.Log)
	if err := run(c); err != nil {
		logx.Errorf("sfu: %v", err)
		logx.Close()
		os.Exit(1)
	}
	logx.Close()
}

func run(c config.Config) error {
	if err := c.Validate(); err != nil {
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

	mux, err := transport.NewMux(transport.MuxConfig{UDP: c.Media.UDPListen, TCP: c.Media.TCPListen})
	if err != nil {
		return err
	}
	defer func() { _ = mux.Close() }()
	tcpPort := 0
	if addr := mux.TCPAddr(); addr != nil {
		tcpPort = addr.Port
	}
	addresses, err := c.Addresses(mux.UDPAddr().Port, tcpPort)
	if err != nil {
		return err
	}
	cert, err := transport.NewCertificate()
	if err != nil {
		return err
	}

	bus := control.NewBus()
	rooms, err := room.NewManager(room.Config{
		Addresses:       addresses,
		Plaintext:       c.Plaintext,
		E2E:             c.E2E,
		MaxParticipants: c.Limits.MaxParticipants,
		MaxVideos:       c.Limits.MaxVideos,
		LastN:           c.Limits.LastN,
		AudioHold:       c.Limits.AudioHold,
		PacketCache:     c.Limits.PacketCache,
		UplinkLimit:     c.Limits.UplinkMbps * 1e6,
		Transport: transport.EndpointConfig{
			StartTimeout:        c.ICE.StartTimeout,
			ConnectTimeout:      c.ICE.ConnectTimeout,
			DisconnectedTimeout: c.ICE.DisconnectedTimeout,
			FailedTimeout:       c.ICE.FailedTimeout,
			KeepaliveInterval:   c.ICE.KeepaliveInterval,
			Renomination:        c.ICE.Renomination,
		},
	}, mux, cert, bus)
	if err != nil {
		return err
	}
	defer rooms.Close()

	lis, err := net.Listen("tcp", c.Control.ListenOn)
	if err != nil {
		return err
	}
	grpcServer := control.NewGRPCServer(tlsConfig, control.NewService(rooms, bus))
	errCh := make(chan error, 2)
	go func() { errCh <- grpcServer.Serve(lis) }()

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
	logx.Infof("sfu: control on %s, media udp %s tcp %q", c.Control.ListenOn, c.Media.UDPListen, c.Media.TCPListen)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-stop:
	case err = <-errCh:
	}
	// The event streams stay open until their clients go: give the calls
	// in flight a moment, then cut the rest.
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
	return err
}
