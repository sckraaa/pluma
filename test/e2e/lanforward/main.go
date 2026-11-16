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

// lanforward makes the stand of test/e2e/run.sh reachable from the local
// network: it accepts TCP connections on an address of this machine (its
// Wi-Fi address) and relays each to the gateway's loopback port, so a phone
// on the same network can sign in with a development build for that address
// (docs/ops/clients.md). Several pairs relay at once, the n-th -listen to
// the n-th -to (the gateway and the CDN node). It runs until interrupted.
// Standard library only.
//
//	lanforward -listen 192.168.0.10:10443 -to 127.0.0.1:10443 \
//		-listen 192.168.0.10:8443 -to 127.0.0.1:23456
package main

import (
	"errors"
	"flag"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"slices"
	"strings"
	"sync"
	"syscall"
)

// addrs is a flag given once per relay.
type addrs []string

func (a *addrs) String() string     { return strings.Join(*a, ",") }
func (a *addrs) Set(v string) error { *a = append(*a, v); return nil }

func main() {
	var listen, to addrs
	flag.Var(&listen, "listen", "address to accept connections on, e.g. 192.168.0.10:10443; once per relay")
	flag.Var(&to, "to", "address to relay them to, e.g. 127.0.0.1:10443; once per relay")
	flag.Parse()
	if len(listen) == 0 || len(listen) != len(to) || slices.Contains(listen, "") || slices.Contains(to, "") {
		flag.Usage()
		os.Exit(2)
	}
	lns := make([]net.Listener, len(listen))
	for i, addr := range listen {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			log.Fatalf("lanforward: %v", err)
		}
		lns[i] = ln
	}
	go func() {
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
		<-stop
		for _, ln := range lns {
			ln.Close()
		}
	}()
	var wg sync.WaitGroup
	for i, ln := range lns {
		log.Printf("lanforward: %s -> %s, Ctrl-C stops", listen[i], to[i])
		wg.Add(1)
		go func() {
			defer wg.Done()
			serve(ln, to[i])
		}()
	}
	wg.Wait()
}

// serve relays each connection ln accepts to to, until ln is closed.
func serve(ln net.Listener, to string) {
	for {
		conn, err := ln.Accept()
		if errors.Is(err, net.ErrClosed) {
			return
		}
		if err != nil {
			log.Printf("lanforward: %v", err)
			continue
		}
		go relay(conn, to)
	}
}

// relay copies both ways until each side has finished sending.
func relay(in net.Conn, to string) {
	defer in.Close()
	out, err := net.Dial("tcp", to)
	if err != nil {
		log.Printf("lanforward: %s: %v", in.RemoteAddr(), err)
		return
	}
	defer out.Close()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(out, in)
		_ = out.(*net.TCPConn).CloseWrite()
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(in, out)
		_ = in.(*net.TCPConn).CloseWrite()
	}()
	wg.Wait()
}
