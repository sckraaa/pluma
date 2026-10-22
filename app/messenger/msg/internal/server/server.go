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

	kafka "github.com/teamgram/marmota/pkg/mq"
	inbox_helper "github.com/teamgram/teamgram-server/app/messenger/msg/inbox"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/config"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	msg_helper "github.com/teamgram/teamgram-server/app/messenger/msg/msg"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"github.com/teamgram/teamgram-server/pkg/conf"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/proc"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var configFile = flag.String("f", "etc/msg.yaml", "the config file")

// drainTimeout bounds the wait for running requests at shutdown: the
// longest box transaction and the publication of its events.
// drainTimeout is how long Destroy waits for running requests: a request
// that started its box operation commits and publishes within it.
const drainTimeout = dao.MaxRequestTime

type Server struct {
	grpcSrv *zrpc.RpcServer
	mq      *kafka.ConsumerGroup
	dao     *dao.Dao

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
	s.dao = d

	// Destroy waits for running requests and events; the forced exit of
	// go-zero must not cut that short.
	proc.SetTimeToForceQuit(drainTimeout + 10*time.Second)

	s.grpcSrv = zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		msg.RegisterRPCMsgServer(grpcServer, msg_helper.New(d, nil))
	})
	s.grpcSrv.AddUnaryInterceptors(s.track)
	go s.grpcSrv.Start()

	s.mq = inbox_helper.New(d, c.InboxConsumer)
	go s.mq.Start()

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

// Destroy stops msg without losing what is committed: it refuses new
// requests, waits for the running ones (their events are published before
// they return), closes the inbox consumer, which waits for the event being
// applied, and flushes the producers.
func (s *Server) Destroy() {
	s.draining.Store(true)
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
	if s.mq != nil {
		if err := s.mq.Close(); err != nil {
			logx.Errorf("msg: close the inbox consumer: %v", err)
		}
	}
	if s.dao != nil {
		if err := s.dao.Close(); err != nil {
			logx.Errorf("msg: close the producers: %v", err)
		}
	}
	s.grpcSrv.Stop()
}
