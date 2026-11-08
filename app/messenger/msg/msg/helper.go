/*
 * Created from 'scheme.tl' by 'mtprotoc'
 *
 * Copyright (c) 2021-present,  Teamgram Studio (https://teamgram.io).
 *  All rights reserved.
 *
 * Author: teamgramio (teamgram.io@gmail.com)
 */

package msg_helper

import (
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/internal/core"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/internal/server/grpc/service"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/internal/svc"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/plugin"
)

// New returns the msg RPC service over the storage d, which it shares with
// the inbox consumer.
func New(d *dao.Dao, plugin plugin.MsgPlugin) *service.Service {
	return service.New(svc.NewServiceContext(d, plugin))
}

// NewScheduledWorker returns the worker that delivers the schedule queues
// (task 061), over the storage d.
func NewScheduledWorker(d *dao.Dao, plugin plugin.MsgPlugin) *core.ScheduledWorker {
	return core.NewScheduledWorker(svc.NewServiceContext(d, plugin))
}
