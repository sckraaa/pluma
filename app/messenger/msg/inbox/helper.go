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
	"context"

	kafka "github.com/teamgram/marmota/pkg/mq"
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/internal/server/mq"
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/internal/svc"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
)

// New returns the consumer of inbox events over the storage d, which it
// shares with the msg RPC service.
func New(d *dao.Dao, conf kafka.KafkaConsumerConf) *kafka.ConsumerGroup {
	return mq.New(svc.NewServiceContext(d), conf)
}

// Handler applies one inbox event as the consumer does (Kafka method
// header, key and value).
func Handler(d *dao.Dao) func(ctx context.Context, method, key string, value []byte) {
	svcCtx := svc.NewServiceContext(d)
	return func(ctx context.Context, method, key string, value []byte) {
		mq.Handle(ctx, svcCtx, method, key, value)
	}
}
