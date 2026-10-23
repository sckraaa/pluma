/*
 * Created from 'scheme.tl' by 'mtprotoc'
 *
 * Copyright (c) 2021-present,  Teamgram Studio (https://teamgram.io).
 *  All rights reserved.
 *
 * Author: teamgramio (teamgram.io@gmail.com)
 */

package inbox_helper

import (
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/internal/server/mq"
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/internal/svc"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	"github.com/teamgram/teamgram-server/pkg/mqconsumer"
)

// New returns the consumer of inbox events over the storage d, which it
// shares with the msg RPC service.
func New(d *dao.Dao, conf mqconsumer.Conf) *mqconsumer.Consumer {
	h, p := Handler(d)
	return mqconsumer.New(conf, "msg", h, p)
}

// Handler returns what the consumer of inbox events runs on each copy, and
// its policy of errors.
func Handler(d *dao.Dao) (mqconsumer.Handler, mqconsumer.Policy) {
	c := mq.New(svc.NewServiceContext(d))
	return c, c.Policy()
}
