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
	"github.com/teamgram/teamgram-server/app/service/biz/biz/internal/config"
	chat_helper "github.com/teamgram/teamgram-server/app/service/biz/chat"
	"github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	code_helper "github.com/teamgram/teamgram-server/app/service/biz/code"
	"github.com/teamgram/teamgram-server/app/service/biz/code/code"
	dialog_helper "github.com/teamgram/teamgram-server/app/service/biz/dialog"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	message_helper "github.com/teamgram/teamgram-server/app/service/biz/message"
	"github.com/teamgram/teamgram-server/app/service/biz/message/message"
	updates_helper "github.com/teamgram/teamgram-server/app/service/biz/updates"
	"github.com/teamgram/teamgram-server/app/service/biz/updates/updates"
	user_helper "github.com/teamgram/teamgram-server/app/service/biz/user"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/conf"
	"github.com/teamgram/teamgram-server/pkg/userbox"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
)

var configFile = flag.String("f", "etc/biz.yaml", "the config file")

type Server struct {
	grpcSrv *zrpc.RpcServer
	workers *user_helper.Workers
}

func New() *Server {
	return new(Server)
}

func (s *Server) Initialize() error {
	var c config.Config
	conf.MustLoad(*configFile, &c)

	// The config carries secrets from the environment (passwords, keys): never log it whole.
	logx.Infof("loaded config %s", *configFile)

	// dialog changes several dialog rows of a user in box transactions and
	// updates reads the boxes' state (pkg/userbox): MySQL must be set up as
	// they need (docs/ops/user-state.md).
	if err := checkBoxSettings(c.Mysql.DSN); err != nil {
		return err
	}
	// A request of dialog or user waiting for a busy user's box must end
	// with the box operation's own result, not the server's deadline.
	if err := userbox.CheckRPCTimeout("Timeout", c.Timeout, max(dialog.MaxBoxOpTime, user_helper.MaxBoxOpTime)); err != nil {
		return err
	}

	// user writes the sign-ups of imported numbers into the importers'
	// boxes as events (ADR 0012, 0015); its worker delivers them.
	userService, workers := user_helper.New(user_helper.Config{
		RpcServerConf: c.RpcServerConf,
		Mysql:         c.Mysql,
		Cache:         c.Cache,
		MediaClient:   c.MediaClient,
		IdgenClient:   c.IdgenClient,
		Topics:        c.Topics,
	})
	s.workers = workers

	s.grpcSrv = zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		// chat_helper
		chat.RegisterRPCChatServer(
			grpcServer,
			chat_helper.New(
				chat_helper.Config{
					RpcServerConf: c.RpcServerConf,
					Mysql:         c.Mysql,
					Cache:         c.Cache,
					MediaClient:   c.MediaClient,
				},
				nil))

		// code_helper
		code.RegisterRPCCodeServer(
			grpcServer,
			code_helper.New(code_helper.Config{
				RpcServerConf: c.RpcServerConf,
				Mysql:         c.Mysql,
				Cache:         c.Cache,
				KV:            c.KV,
			}))

		// dialog_helper
		dialog.RegisterRPCDialogServer(
			grpcServer,
			dialog_helper.New(dialog_helper.Config{
				RpcServerConf: c.RpcServerConf,
				Mysql:         c.Mysql,
				Cache:         c.Cache,
			}))

		// message_helper
		message.RegisterRPCMessageServer(
			grpcServer,
			message_helper.New(
				message_helper.Config{
					RpcServerConf:   c.RpcServerConf,
					Mysql:           c.Mysql,
					Cache:           c.Cache,
					MessageSharding: c.MessageSharding,
				},
				nil))

		// updates_helper
		updates.RegisterRPCUpdatesServer(
			grpcServer,
			updates_helper.New(updates_helper.Config{
				RpcServerConf: c.RpcServerConf,
				Mysql:         c.Mysql,
			}))

		// user_helper
		user.RegisterRPCUserServer(grpcServer, userService)
	})

	// logx.Must(err)
	go func() {
		s.grpcSrv.Start()
	}()
	s.workers.Start()
	return nil
}

func (s *Server) RunLoop() {
}

// Destroy stops the workers of the user service, which finish the work in
// progress (user_helper.SignUpStopWait, PremiumStopWait), while the server
// stops.
func (s *Server) Destroy() {
	done := make(chan struct{})
	go func() {
		defer close(done)
		if s.workers != nil {
			s.workers.Stop()
		}
	}()
	s.grpcSrv.Stop()
	<-done
}

// checkBoxSettings verifies the MySQL settings box transactions rely on
// (userbox.Check) on the pool of dsn.
func checkBoxSettings(dsn string) error {
	db, err := userbox.Open(dsn)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return userbox.Check(ctx, db)
}
