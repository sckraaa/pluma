// Copyright 2022 Teamgram Authors
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
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package server

import (
	"context"
	"flag"
	"sync"
	"sync/atomic"
	"time"

	inbox_helper "github.com/teamgram/teamgram-server/app/messenger/msg/inbox"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/config"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	msg_helper "github.com/teamgram/teamgram-server/app/messenger/msg/msg"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"github.com/teamgram/teamgram-server/pkg/conf"
	"github.com/teamgram/teamgram-server/pkg/mqconsumer"
	"github.com/teamgram/teamgram-server/pkg/outbox"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/proc"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var configFile = flag.String("f", "etc/msg.yaml", "the config file")

// drainTimeout is how long Destroy waits for running requests: a request
// that started its box operation commits within it.
const drainTimeout = dao.MaxRequestTime

type Server struct {
	grpcSrv *zrpc.RpcServer
	inbox   *mqconsumer.Consumer
	relay   *outbox.Relay

	draining atomic.Bool
	running  sync.WaitGroup
}

func New() *Server {
	return new(Server)
}

func (s *Server) Initialize() error {
	var c config.Config
	conf.MustLoad(*configFile, &c)

	// The config carries secrets from the environment (passwords, keys): never log it whole.
	logx.Infof("loaded config %s", *configFile)

	// Every request of msg may run a box operation: the server's deadline
	// must outlive it.
	if err := userbox.CheckRPCTimeout("Timeout", c.Timeout, dao.MaxRequestTime); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	d, err := dao.New(ctx, c)
	if err != nil {
		return err
	}

	// Destroy waits for running requests and the event being applied; the
	// forced exit of go-zero must not cut that short.
	proc.SetTimeToForceQuit(drainTimeout + 10*time.Second)

	s.grpcSrv = zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		msg.RegisterRPCMsgServer(grpcServer, msg_helper.New(d, nil))
	})
	s.grpcSrv.AddUnaryInterceptors(s.track)
	go s.grpcSrv.Start()

	// Neither needs Kafka to start: both connect in the background, and
	// the events wait in mq_outbox meanwhile.
	s.relay = outbox.NewRelay(d.BoxDB, c.Relay)
	s.relay.Start()
	s.inbox = inbox_helper.New(d, c.InboxConsumer)
	s.inbox.Start()

	return nil
}

// track counts the running requests and refuses new ones while draining.
func (s *Server) track(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	if s.draining.Load() {
		return nil, status.Error(codes.Unavailable, "msg is shutting down")
	}
	s.running.Add(1)
	defer s.running.Done()
	return handler(ctx, req)
}

func (s *Server) RunLoop() {
}

// Destroy stops msg: it refuses new requests and, at the same time, waits
// for the running ones and stops the inbox consumer, which lets the event
// being applied finish; then it stops the relay, which records what the
// broker answered. Nothing committed is lost on a harder stop either: the
// events are rows of mq_outbox.
func (s *Server) Destroy() {
	s.draining.Store(true)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		done := make(chan struct{})
		go func() {
			s.running.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(drainTimeout):
			logx.Errorf("msg: requests still running after %v", drainTimeout)
		}
	}()
	go func() {
		defer wg.Done()
		if s.inbox != nil {
			s.inbox.Stop()
		}
	}()
	wg.Wait()
	if s.relay != nil {
		s.relay.Stop()
	}
	s.grpcSrv.Stop()
}
