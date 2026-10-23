// Copyright (c) 2021-present,  Teamgram Studio (https://teamgram.io).
//  All rights reserved.
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package sync_client

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/pkg/outbox"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/protobuf/proto"
)

// OutboxConf says where the pushes of a service go: rows of mq_outbox in
// the database at DSN, for Topic, which the relay of msg publishes
// (ADR 0012, p. 4).
type OutboxConf struct {
	DSN   string
	Topic string
}

// outboxClient writes each push as a row of mq_outbox (outbox.Insert), one
// autocommit INSERT within the request's context: a push is durable once
// the call returns, Kafka is not on the path of the request, and the
// pushes of the service keep the order of their ids with those of msg.
type outboxClient struct {
	db    *sql.DB
	topic string
}

// NewSyncOutboxClient returns the SyncClient of c. It registers c.Topic
// as a push topic.
func NewSyncOutboxClient(c OutboxConf) (SyncClient, error) {
	if c.DSN == "" || c.Topic == "" {
		return nil, fmt.Errorf("sync: outbox client without a DSN or a topic")
	}
	if err := outbox.Register(outbox.Topics{Push: []string{c.Topic}}); err != nil {
		return nil, err
	}
	db, err := userbox.Open(c.DSN)
	if err != nil {
		return nil, err
	}
	return &outboxClient{db: db, topic: c.Topic}, nil
}

// MustNewSyncOutboxClient is NewSyncOutboxClient that panics on an error,
// at the start of a service.
func MustNewSyncOutboxClient(c OutboxConf) SyncClient {
	cli, err := NewSyncOutboxClient(c)
	if err != nil {
		panic(err)
	}
	return cli
}

// insert writes in, addressed to recipient (the Kafka key).
func (m *outboxClient) insert(ctx context.Context, recipient int64, in proto.Message) (*mtproto.Void, error) {
	data, err := jsonx.Marshal(in)
	if err != nil {
		return nil, err
	}
	if _, err = outbox.Insert(ctx, m.db, userbox.Event{
		Topic:     m.topic,
		Recipient: recipient,
		Method:    string(proto.MessageName(in)),
		Data:      data,
	}); err != nil {
		return nil, err
	}
	return mtproto.EmptyVoid, nil
}

// SyncUpdatesMe
// sync.updatesMe flags:# user_id:long auth_key_id:long server_id:string session_id:flags.0?long updates:Updates = Void;
func (m *outboxClient) SyncUpdatesMe(ctx context.Context, in *sync.TLSyncUpdatesMe) (*mtproto.Void, error) {
	return m.insert(ctx, in.GetUserId(), in)
}

// SyncUpdatesNotMe
// sync.updatesNotMe user_id:long auth_key_id:long updates:Updates = Void;
func (m *outboxClient) SyncUpdatesNotMe(ctx context.Context, in *sync.TLSyncUpdatesNotMe) (*mtproto.Void, error) {
	return m.insert(ctx, in.GetUserId(), in)
}

// SyncPushUpdates
// sync.pushUpdates user_id:long updates:Updates = Void;
func (m *outboxClient) SyncPushUpdates(ctx context.Context, in *sync.TLSyncPushUpdates) (*mtproto.Void, error) {
	return m.insert(ctx, in.GetUserId(), in)
}

// SyncPushUpdatesIfNot
// sync.pushUpdatesIfNot user_id:long excludes:Vector<long> updates:Updates = Void;
func (m *outboxClient) SyncPushUpdatesIfNot(ctx context.Context, in *sync.TLSyncPushUpdatesIfNot) (*mtproto.Void, error) {
	return m.insert(ctx, in.GetUserId(), in)
}

// SyncPushBotUpdates
// sync.pushBotUpdates user_id:long updates:Updates = Void;
func (m *outboxClient) SyncPushBotUpdates(ctx context.Context, in *sync.TLSyncPushBotUpdates) (*mtproto.Void, error) {
	return m.insert(ctx, in.GetUserId(), in)
}

// SyncPushRpcResult
// sync.pushRpcResult auth_key_id:long server_id:string session_id:long client_req_msg_id:long rpc_result:bytes = Void;
func (m *outboxClient) SyncPushRpcResult(ctx context.Context, in *sync.TLSyncPushRpcResult) (*mtproto.Void, error) {
	return m.insert(ctx, in.GetUserId(), in)
}

// SyncBroadcastUpdates
// sync.broadcastUpdates broadcast_type:int chat_id:long exclude_id_list:Vector<long> updates:Updates = Void;
func (m *outboxClient) SyncBroadcastUpdates(ctx context.Context, in *sync.TLSyncBroadcastUpdates) (*mtproto.Void, error) {
	return m.insert(ctx, in.GetChatId(), in)
}
