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

// Package sfuclient is the signalling side's client of the SFU nodes
// (app/service/sfu, ADR 0059 §3): the control API over mutual TLS, the
// choice of a node for a call and the stream of each node's events. The
// nodes are named in the configuration by their addresses, without etcd:
// a node may run on another machine, and the core's etcd is not reachable
// from outside.
package sfuclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/teamgram/teamgram-server/app/bff/groupcalls/internal/config"
	pb "github.com/teamgram/teamgram-server/app/service/sfu/sfu"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
)

// Errors of the client.
var (
	// ErrNoNode: no node is configured, or none other than the one to skip.
	ErrNoNode = errors.New("sfuclient: no node for the call")
	// ErrUnknownNode: a call names a node that is no longer configured.
	ErrUnknownNode = errors.New("sfuclient: unknown node")
)

// API is what the signalling side does with the nodes; Client implements
// it, the tests fake it.
type API interface {
	// Enabled reports whether any node is configured.
	Enabled() bool
	// Has reports whether a node of that name is configured.
	Has(node string) bool
	// Pick chooses a node for a call: the first of the configuration that
	// is not known to be down, other than skip; a node known to be down
	// when no other is left.
	Pick(skip string) (string, error)
	CreateRoom(ctx context.Context, node string, callID int64, e2e bool) error
	CloseRoom(ctx context.Context, node string, callID int64) error
	Join(ctx context.Context, node string, req *pb.JoinRequest) (*pb.JoinResponse, error)
	Leave(ctx context.Context, node string, callID, participant int64, kind pb.ConnectionKind) error
	SetMuted(ctx context.Context, node string, callID, participant int64, muted bool) error
	// Sources lists the connections of a room; NOT_FOUND without the room.
	Sources(ctx context.Context, node string, callID int64) ([]*pb.ConnectionSources, error)
}

type node struct {
	cfg  config.Node
	conn *grpc.ClientConn
	api  pb.SFUClient
}

// Client holds a connection to every configured node.
type Client struct {
	nodes   []*node
	byName  map[string]*node
	timeout time.Duration
}

// New connects to the nodes of c (lazily: a node that is down does not
// stop the service). A node without an endpoint is left out, so that a
// configuration can name one from an environment variable that is empty
// where group calls are off; without nodes the client is disabled.
func New(c config.GroupCalls) (*Client, error) {
	cl := &Client{byName: map[string]*node{}, timeout: c.RequestTimeout}
	if cl.timeout <= 0 {
		cl.timeout = 5 * time.Second
	}
	var nodes []config.Node
	for _, n := range c.Nodes {
		if n.Endpoint != "" {
			nodes = append(nodes, n)
		}
	}
	if len(nodes) == 0 {
		return cl, nil
	}
	base, err := clientTLS(c.CertFile, c.KeyFile, c.CAFile)
	if err != nil {
		return nil, err
	}
	for _, n := range nodes {
		if n.Name == "" {
			return nil, fmt.Errorf("sfuclient: the node at %s needs a Name", n.Endpoint)
		}
		if _, dup := cl.byName[n.Name]; dup {
			return nil, fmt.Errorf("sfuclient: node %q twice", n.Name)
		}
		tc := base.Clone()
		tc.ServerName = n.ServerName
		if tc.ServerName == "" {
			host, _, err := net.SplitHostPort(n.Endpoint)
			if err != nil {
				return nil, fmt.Errorf("sfuclient: node %q: endpoint: %w", n.Name, err)
			}
			tc.ServerName = host
		}
		// The endpoint's name is resolved anew on every connection
		// attempt (passthrough: the dialer resolves it). A node in Docker
		// comes back at another address when its container is made again,
		// and gRPC's own DNS resolver would keep the dead address for up to
		// 30 s (grpc internal/resolver/dns MinResolutionInterval), failing
		// every join meanwhile.
		conn, err := grpc.NewClient("passthrough:///"+n.Endpoint, grpc.WithTransportCredentials(credentials.NewTLS(tc)))
		if err != nil {
			return nil, fmt.Errorf("sfuclient: node %q: %w", n.Name, err)
		}
		nd := &node{cfg: n, conn: conn, api: pb.NewSFUClient(conn)}
		cl.nodes = append(cl.nodes, nd)
		cl.byName[n.Name] = nd
	}
	return cl, nil
}

// clientTLS is the client side of mutual TLS: its certificate, and the CA
// the nodes' certificates must chain to.
func clientTLS(certFile, keyFile, caFile string) (*tls.Config, error) {
	if certFile == "" || keyFile == "" || caFile == "" {
		return nil, errors.New("sfuclient: GroupCalls needs CertFile, KeyFile and CAFile: the SFU control API is mutual TLS only")
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("sfuclient: client certificate: %w", err)
	}
	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("sfuclient: CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("sfuclient: no certificate in the CA file")
	}
	return &tls.Config{Certificates: []tls.Certificate{cert}, RootCAs: pool, MinVersion: tls.VersionTLS13}, nil
}

// Close closes the connections.
func (c *Client) Close() {
	for _, n := range c.nodes {
		_ = n.conn.Close()
	}
}

func (c *Client) Enabled() bool { return len(c.nodes) > 0 }

func (c *Client) Has(name string) bool {
	_, ok := c.byName[name]
	return ok
}

func (c *Client) Pick(skip string) (string, error) {
	var fallback string
	for _, n := range c.nodes {
		if n.cfg.Name == skip {
			continue
		}
		switch n.conn.GetState() {
		case connectivity.TransientFailure, connectivity.Shutdown:
			if fallback == "" {
				fallback = n.cfg.Name
			}
			continue
		}
		return n.cfg.Name, nil
	}
	if fallback != "" {
		return fallback, nil
	}
	return "", ErrNoNode
}

func (c *Client) api(name string) (pb.SFUClient, error) {
	n, ok := c.byName[name]
	if !ok {
		return nil, ErrUnknownNode
	}
	return n.api, nil
}

func (c *Client) CreateRoom(ctx context.Context, name string, callID int64, e2e bool) error {
	api, err := c.api(name)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	_, err = api.CreateRoom(ctx, &pb.CreateRoomRequest{CallId: callID, E2E: e2e})
	return err
}

func (c *Client) CloseRoom(ctx context.Context, name string, callID int64) error {
	api, err := c.api(name)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	_, err = api.CloseRoom(ctx, &pb.CloseRoomRequest{CallId: callID})
	return err
}

func (c *Client) Join(ctx context.Context, name string, req *pb.JoinRequest) (*pb.JoinResponse, error) {
	api, err := c.api(name)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	return api.Join(ctx, req)
}

func (c *Client) Leave(ctx context.Context, name string, callID, participant int64, kind pb.ConnectionKind) error {
	api, err := c.api(name)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	_, err = api.Leave(ctx, &pb.LeaveRequest{CallId: callID, Participant: participant, Kind: kind})
	return err
}

func (c *Client) SetMuted(ctx context.Context, name string, callID, participant int64, muted bool) error {
	api, err := c.api(name)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	_, err = api.SetMuted(ctx, &pb.SetMutedRequest{CallId: callID, Participant: participant, Muted: muted})
	return err
}

func (c *Client) Sources(ctx context.Context, name string, callID int64) ([]*pb.ConnectionSources, error) {
	api, err := c.api(name)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	r, err := api.Sources(ctx, &pb.SourcesRequest{CallId: callID})
	if err != nil {
		return nil, err
	}
	return r.GetConnections(), nil
}

// Handler takes one event of a node.
type Handler func(node string, ev *pb.Event)

// Watch follows the events of every node until ctx ends: a stream that
// breaks (the node restarts, the network) is opened again after a pause
// that grows to a minute. Events of the gap are lost; the collector
// catches what they would have told (ADR 0059 §10).
func (c *Client) Watch(ctx context.Context, h Handler) *sync.WaitGroup {
	wg := &sync.WaitGroup{}
	for _, n := range c.nodes {
		wg.Add(1)
		go func(n *node) {
			defer wg.Done()
			pause := time.Second
			for ctx.Err() == nil {
				received, err := c.follow(ctx, n, h)
				if ctx.Err() != nil {
					return
				}
				if received {
					pause = time.Second
				}
				logx.WithContext(ctx).Errorf("groupcalls: events of SFU node %s: %v; again in %s", n.cfg.Name, err, pause)
				select {
				case <-ctx.Done():
					return
				case <-time.After(pause):
				}
				pause = min(2*pause, time.Minute)
			}
		}(n)
	}
	return wg
}

// follow reads one stream of a node's events until it breaks; received
// tells whether any came.
func (c *Client) follow(ctx context.Context, n *node, h Handler) (received bool, err error) {
	stream, err := n.api.Events(ctx, &pb.EventsRequest{})
	if err != nil {
		return false, err
	}
	for {
		ev, err := stream.Recv()
		if err != nil {
			return received, err
		}
		received = true
		h(n.cfg.Name, ev)
	}
}
