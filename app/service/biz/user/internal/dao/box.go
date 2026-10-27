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
	"fmt"
	"strings"
	"time"

	"github.com/teamgram/proto/mtproto"
	inboxpb "github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	sync_client "github.com/teamgram/teamgram-server/app/messenger/sync/client"
	"github.com/teamgram/teamgram-server/pkg/outbox"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/protobuf/proto"
)

// cacheTimeout bounds a deletion of cache keys after a commit: the change
// is committed, a slow Redis must not hold the request.
const cacheTimeout = 5 * time.Second

// MaxBoxOpTime is the longest a box operation of the service takes: the
// cache deletion before the transaction, the transaction with its retries
// and the check of an uncertain commit (userbox.MaxRunTime), the deletion
// after it. The deadline of the biz server covers it (dialog.MaxBoxOpTime
// is the same sum).
const MaxBoxOpTime = cacheTimeout + userbox.MaxRunTime + cacheTimeout

// RunBox runs fn as a box transaction of userId (userbox.Run). rowKeys are
// deleted from the cache before the transaction, and again with the keys
// fn added after the commit; then the relays of the process are woken when
// the transaction wrote events. The cache goes first: msg builds the users
// of an inbox event with seq from biz user when it applies it (ADR 0021),
// and right after the commit it would get a changed account as the cache
// still had it. A failed deletion is logged: the change is committed, the
// cache entries expire.
func (d *Dao) RunBox(ctx context.Context, userId int64, rowKeys []string, fn func(b *userbox.Box) error) (*userbox.Result, error) {
	d.deleteCache(ctx, rowKeys)
	res, err := userbox.Run(ctx, d.BoxDB, userId, fn)
	if err != nil {
		return nil, err
	}
	d.deleteCache(ctx, append(rowKeys, res.CacheKeys...))
	if res.Emitted > 0 {
		outbox.Notify()
	}
	return res, nil
}

func (d *Dao) deleteCache(ctx context.Context, keys []string) {
	if len(keys) == 0 {
		return
	}
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cacheTimeout)
	defer cancel()
	if err := d.CachedConn.DelCache(cctx, keys...); err != nil {
		logx.WithContext(ctx).Errorf("user: delete %d cache keys: %v", len(keys), err)
	}
}

// EmitSeqUpdate journals u with the next seq of b's account and pushes it to
// every device of the account (sync_client.EmitSeqUpdate, ADR 0020). users
// and chats are what u refers to, as the account sees them.
func (d *Dao) EmitSeqUpdate(b *userbox.Box, u *mtproto.Update, users []*mtproto.User, chats []*mtproto.Chat) error {
	return sync_client.EmitSeqUpdate(b, d.Topics.Sync, u, users, chats)
}

// EmitSeqInbox journals updates with seq in the account of to and pushes
// them to its devices, as the transaction's inbox event to to
// (inbox.EmitSeqInbox, ADR 0021): the users and chats they refer to are
// built when msg applies it, as to sees them then.
func (d *Dao) EmitSeqInbox(b *userbox.Box, to int64, updates ...*mtproto.Update) error {
	return inboxpb.EmitSeqInbox(b, d.Topics.Inbox, to, updates...)
}

// userEvent is an event request addressed to one user, user_id.
type userEvent interface {
	proto.Message
	GetUserId() int64
}

// newEvent is the event of in on topic, as msg writes them: the method is
// the request's proto message name, the payload its JSON.
func newEvent(topic string, in userEvent) (userbox.Event, error) {
	if in.GetUserId() <= 0 {
		return userbox.Event{}, fmt.Errorf("user: event %s without a recipient", proto.MessageName(in))
	}
	data, err := jsonx.Marshal(in)
	if err != nil {
		return userbox.Event{}, err
	}
	return userbox.Event{
		Topic:     topic,
		Recipient: in.GetUserId(),
		Method:    string(proto.MessageName(in)),
		Data:      data,
	}, nil
}

// inList is the placeholders of an IN list of n values.
func inList(n int) string {
	if n == 0 {
		return "NULL"
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}
