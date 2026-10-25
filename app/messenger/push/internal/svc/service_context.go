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

package svc

import (
	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/app/messenger/push/internal/config"
	"github.com/teamgram/teamgram-server/app/messenger/push/internal/core"
	"github.com/teamgram/teamgram-server/app/messenger/push/internal/dao"
	"github.com/teamgram/teamgram-server/app/messenger/push/internal/provider"
	authsession_client "github.com/teamgram/teamgram-server/app/service/authsession/client"
	message_client "github.com/teamgram/teamgram-server/app/service/biz/message/client"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	status_client "github.com/teamgram/teamgram-server/app/service/status/client"
	"github.com/teamgram/teamgram-server/pkg/pushdevices"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/zrpc"
)

type ServiceContext struct {
	Config config.Config
	Core   *core.Core
}

// NewServiceContext wires the core. A wrong key file stops the start; a
// provider without a key is off, which the log and push_sent_total{result=
// "disabled"} show.
func NewServiceContext(c config.Config) *ServiceContext {
	sealer, err := pushdevices.NewSealer(c.PushSecretEncryptionKey)
	logx.Must(err)
	apns, err := provider.NewAPNs(c.APNs, nil)
	logx.Must(err)
	fcm, err := provider.NewFCM(c.FCM, nil)
	logx.Must(err)
	deps := core.Deps{
		Devices:        pushdevices.NewMySQLStore(sqlx.NewMySQL(&c.Mysql), sealer),
		Online:         &dao.Online{Client: status_client.NewStatusClient(zrpc.MustNewClient(c.StatusClient))},
		Authorizations: &dao.Authorizations{Client: authsession_client.NewAuthsessionClient(rpcx.GetCachedRpcClient(c.AuthsessionClient))},
		Settings:       &dao.Settings{Client: user_client.NewUserClient(rpcx.GetCachedRpcClient(c.BizServiceClient))},
		Messages:       &dao.Messages{Client: message_client.NewMessageClient(rpcx.GetCachedRpcClient(c.BizServiceClient))},
		APNs:           apns,
		FCM:            fcm,
	}
	if apns == nil {
		logx.Error("push: APNs is off (no key): iOS gets no pushes")
	} else {
		logx.Infof("push: APNs on, topic %s", c.APNs.Topic)
	}
	if fcm == nil {
		logx.Error("push: FCM is off (no credentials): Android gets no pushes")
	} else {
		logx.Infof("push: FCM on, project %s", fcm.ProjectId())
	}
	return &ServiceContext{Config: c, Core: core.New(deps)}
}
