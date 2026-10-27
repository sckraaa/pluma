package dao

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/teamgram/proto/mtproto"
	sync_client "github.com/teamgram/teamgram-server/app/messenger/sync/client"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/pkg/outbox"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/protobuf/proto"
)

// Bounds of the steps of a request of msg. The wait for running requests
// at shutdown derives from them.
const (
	// cacheTimeout limits one Redis operation of a request: a cache
	// deletion around a box transaction.
	cacheTimeout = 5 * time.Second
	// MaxBoxOpTime is the longest RunBox can take: the cache deletions
	// before and after the transaction, and the transaction.
	MaxBoxOpTime = cacheTimeout + userbox.MaxRunTime + cacheTimeout
	// MaxRequestTime bounds a request of msg from its box operation on;
	// RPCs to other services before it are not counted.
	MaxRequestTime = MaxBoxOpTime
)

// RunBox changes userId's box in one transaction (userbox.Run on BoxDB).
// The events of the transaction are its mq_outbox rows: committed with it,
// published by the relay; RunBox wakes the relay of the process after a
// commit that gave it rows to publish.
//
// rowKeys are the cache keys of dialog rows the operation is known to
// change: they are deleted before the transaction and again after commit,
// with the keys fn added (b.InvalidateCache), which narrows the window in
// which a reader puts an old row back into the cache. A failed deletion is
// logged and counted; the operation stays successful.
func (d *Dao) RunBox(ctx context.Context, userId int64, rowKeys []string, fn func(b *userbox.Box) error) (*userbox.Result, error) {
	d.deleteCache(ctx, rowKeys)
	res, err := userbox.Run(ctx, d.BoxDB, userId, fn)
	if err != nil {
		return nil, err
	}
	// New rows, or rows a deferral returned to pending.
	if res.Emitted > 0 || res.Taken == userbox.TakeDeferred {
		outbox.Notify()
	}
	d.deleteCache(ctx, append(rowKeys, res.CacheKeys...))
	return res, nil
}

// ReadState returns the committed state of userId's box.
func (d *Dao) ReadState(ctx context.Context, userId int64) (userbox.State, error) {
	return userbox.Read(ctx, d.BoxDB, userId)
}

func (d *Dao) deleteCache(ctx context.Context, keys []string) {
	if len(keys) == 0 {
		return
	}
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cacheTimeout)
	defer cancel()
	if err := d.CachedConn.DelCache(cctx, keys...); err != nil {
		metricPostCommitErrors.Inc("cache")
		logx.WithContext(ctx).Errorf("msg: delete %d cache keys: %v", len(keys), err)
	}
}

// InsertEvent writes an event that no box transaction goes with
// (outbox.Insert): once it returns, the event is durable.
func (d *Dao) InsertEvent(ctx context.Context, ev userbox.Event) error {
	_, err := outbox.Insert(ctx, d.BoxDB, ev)
	return err
}

// userEvent is an event request addressed to one user, user_id.
type userEvent interface {
	proto.Message
	GetUserId() int64
}

// fromEvent is an inbox event: from_id caused it.
type fromEvent interface {
	userEvent
	GetFromId() int64
}

func newEvent(topic string, in userEvent) (userbox.Event, error) {
	if in.GetUserId() <= 0 {
		return userbox.Event{}, fmt.Errorf("msg: event %s without a recipient", proto.MessageName(in))
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

// errSelfEvent: an inbox event addressed to the user who caused it. The
// initiator's own box is written in its transaction, never by an event.
var errSelfEvent = errors.New("msg: inbox event addressed to its initiator")

// EmitInbox adds an inbox event for in.user_id to the transaction.
func (d *Dao) EmitInbox(b *userbox.Box, in fromEvent) error {
	if in.GetUserId() == in.GetFromId() {
		return errSelfEvent
	}
	ev, err := newEvent(d.Topics.Inbox, in)
	if err != nil {
		return err
	}
	b.Emit(ev)
	return nil
}

// InboxEvent returns an inbox event to write outside a box transaction.
func (d *Dao) InboxEvent(in fromEvent) (userbox.Event, error) {
	if in.GetUserId() == in.GetFromId() {
		return userbox.Event{}, errSelfEvent
	}
	return newEvent(d.Topics.Inbox, in)
}

// EmitUpdatesNotMe adds a push of updates to userId's devices other than
// the one with permAuthKeyId (the one that made the request and gets them
// in the reply).
func (d *Dao) EmitUpdatesNotMe(b *userbox.Box, userId, permAuthKeyId int64, updates *mtproto.Updates) error {
	ev, err := newEvent(d.Topics.Sync, &sync.TLSyncUpdatesNotMe{
		UserId:        userId,
		PermAuthKeyId: permAuthKeyId,
		Updates:       updates,
	})
	if err != nil {
		return err
	}
	b.Emit(ev)
	return nil
}

// EmitSeqUpdate journals u with the next seq of b's account and pushes it to
// every device of the account (sync_client.EmitSeqUpdate, ADR 0020). users
// and chats are what u refers to, as the account sees them.
func (d *Dao) EmitSeqUpdate(b *userbox.Box, u *mtproto.Update, users []*mtproto.User, chats []*mtproto.Chat) error {
	return sync_client.EmitSeqUpdate(b, d.Topics.Sync, u, users, chats)
}

// EmitPushUpdates adds a push of updates to all of userId's devices, or to
// the bot gateway when userId is a bot (nothing when bots are not served).
func (d *Dao) EmitPushUpdates(b *userbox.Box, userId int64, isBot bool, updates *mtproto.Updates) error {
	var (
		ev  userbox.Event
		err error
	)
	if isBot {
		if d.Topics.BotSync == "" {
			return nil
		}
		ev, err = newEvent(d.Topics.BotSync, &sync.TLSyncPushBotUpdates{UserId: userId, Updates: updates})
	} else {
		ev, err = newEvent(d.Topics.Sync, &sync.TLSyncPushUpdates{UserId: userId, Updates: updates})
	}
	if err != nil {
		return err
	}
	b.Emit(ev)
	return nil
}
