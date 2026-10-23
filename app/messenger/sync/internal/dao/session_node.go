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

package dao

import (
	"context"
	"time"

	sessionclient "github.com/teamgram/teamgram-server/app/interface/session/client"
	"github.com/teamgram/teamgram-server/app/interface/session/session"

	"github.com/zeromicro/go-zero/core/discov"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/keepalive"
)

// rpcNode is a session node reached over one gRPC connection. Reconnects
// back off to at most 3 s (the gRPC default reaches 120 s), and go-zero's
// breaker is off: the pusher itself decides what a failed push means.
type rpcNode struct {
	conn   *grpc.ClientConn
	client sessionclient.SessionClient
}

func newRpcNode(c zrpc.RpcClientConf, addr string) (sessionNode, error) {
	c.Etcd = discov.EtcdConf{}
	c.Endpoints = []string{addr}
	c.Timeout = int64(PushCallTimeout / time.Millisecond)
	c.NonBlock = true
	c.Middlewares.Breaker = false
	cli, err := zrpc.NewClient(c,
		zrpc.WithDialOption(grpc.WithConnectParams(grpc.ConnectParams{
			Backoff:           backoff.Config{BaseDelay: 100 * time.Millisecond, Multiplier: 1.6, Jitter: 0.2, MaxDelay: 3 * time.Second},
			MinConnectTimeout: 2 * time.Second,
		})),
		zrpc.WithDialOption(grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                20 * time.Second,
			Timeout:             5 * time.Second,
			PermitWithoutStream: true,
		})),
	)
	if err != nil {
		return nil, err
	}
	return &rpcNode{conn: cli.Conn(), client: sessionclient.NewSessionClient(cli)}, nil
}

func (n *rpcNode) pushUpdates(ctx context.Context, in *session.TLSessionPushUpdatesData) error {
	_, err := n.client.SessionPushUpdatesData(ctx, in)
	return err
}

func (n *rpcNode) pushSessionUpdates(ctx context.Context, in *session.TLSessionPushSessionUpdatesData) error {
	_, err := n.client.SessionPushSessionUpdatesData(ctx, in)
	return err
}

func (n *rpcNode) pushRpcResult(ctx context.Context, in *session.TLSessionPushRpcResultData) error {
	_, err := n.client.SessionPushRpcResultData(ctx, in)
	return err
}

func (n *rpcNode) waitReady(ctx context.Context) bool {
	for {
		s := n.conn.GetState()
		switch s {
		case connectivity.Ready:
			return true
		case connectivity.Idle:
			n.conn.Connect()
		case connectivity.Shutdown:
			return false
		}
		if !n.conn.WaitForStateChange(ctx, s) {
			return false
		}
	}
}

func (n *rpcNode) resetBackoff() { n.conn.ResetConnectBackoff() }

func (n *rpcNode) close() {
	if err := n.conn.Close(); err != nil {
		logx.Errorf("sync: close session node: %v", err)
	}
}

// watchNodes keeps the pusher's nodes in step with etcd.
func watchNodes(c zrpc.RpcClientConf, p *Pusher) {
	sub, err := discov.NewSubscriber(c.Etcd.Hosts, c.Etcd.Key)
	if err != nil {
		logx.Must(err)
	}
	update := func() { p.SetNodes(sub.Values()) }
	sub.AddListener(update)
	update()
}
