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

package core

import (
	"errors"
	"slices"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/plugin"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Scheduled messages (task 061, core.telegram.org/api/scheduled-messages):
// a message with schedule_date waits in the sender's queue of the dialog,
// and goes out through the normal send path at its time, with every check
// of a send made again then.

const (
	// scheduleMinAhead: a date less than that ahead sends at once, as a
	// plain message (api/scheduled-messages: "less than 10 seconds").
	scheduleMinAhead = 10
	// scheduleMaxAhead: how far ahead a date may be. The clients let one
	// pick a day up to 365 days ahead, and its last minute (Android
	// AlertsCreator dayPicker 0..365, iOS ChatScheduleTimeControllerNode
	// .day 365); a date past that is SCHEDULE_DATE_TOO_LATE.
	scheduleMaxAhead = 366 * 86400
	// scheduledBatch bounds what one tick of the worker reads.
	scheduledBatch = 200
)

// scheduleRepeatPeriods are the values schedule_repeat_period may take
// (core.telegram.org/api/scheduled-messages: daily to yearly, Android
// AlertsCreator repeat picker: 1, 7, 14, 30, 91, 182 and 365 days).
var scheduleRepeatPeriods = []int32{86400, 7 * 86400, 14 * 86400, 30 * 86400, 91 * 86400, 182 * 86400, 365 * 86400}

// repeatPeriodOf is the schedule_repeat_period of a send, 0 for none, and
// checks it: one of the allowed values, a single message
// (core.telegram.org/api/scheduled-messages: "can only be used when
// sending/forwarding a single message") with a real date. The documentation
// names no error for a wrong value; UNVERIFIED which Telegram answers.
func repeatPeriodOf(list []*msg.OutboxMessage, date int32) (int32, error) {
	var period int32
	for _, o := range list {
		if v := o.GetMessage().GetScheduleRepeatPeriod().GetValue(); v != 0 {
			period = v
		}
	}
	switch {
	case period == 0:
		return 0, nil
	case len(list) != 1 || date == dao.ScheduleWhenOnline || !slices.Contains(scheduleRepeatPeriods, period):
		return 0, mtproto.ErrScheduleDateInvalid
	}
	return period, nil
}

// sendOptions change a send on behalf of a schedule queue.
type sendOptions struct {
	// queued: the messages come off a schedule queue. They went through
	// plugin.RemakeMessage when they were scheduled, and a second pass would
	// add their automatic entities again; and their sender must still be in
	// the group, unlike the sender of the service message of its own leave.
	queued bool
	// fromScheduled: the send is the delivery of waiting messages at their
	// time: in Saved Messages they stay unread as reminders.
	fromScheduled bool
	// after runs in the sender's transaction once the messages are written
	// or found written earlier, with their real ids in order.
	after func(b *userbox.Box, sent []int32) error
}

func (o sendOptions) runAfter(b *userbox.Box, sent []int32) error {
	if o.after == nil {
		return nil
	}
	return o.after(b, sent)
}

// sentIds are the box ids of boxes, in order.
func sentIds(boxes []*mtproto.MessageBox) []int32 {
	ids := make([]int32, len(boxes))
	for i, b := range boxes {
		ids[i] = b.MessageId
	}
	return ids
}

// scheduleDateOf is the common schedule_date of a send, 0 for none: a send
// has one date (messages.sendMultiMedia and forwardMessages take one).
func scheduleDateOf(list []*msg.OutboxMessage) (int32, error) {
	date := list[0].GetScheduleDate().GetValue()
	for _, o := range list {
		if o.GetScheduleDate().GetValue() != date {
			return 0, mtproto.ErrInputRequestInvalid
		}
	}
	return date, nil
}

// send sends in's messages now.
func (c *MsgCore) send(in *msg.TLMsgSendMessageV2, opt sendOptions) (*mtproto.Updates, error) {
	switch in.PeerType {
	case mtproto.PEER_USER:
		return c.sendUserMessages(in.UserId, in.AuthKeyId, in.PeerId, in.GetMessage(), opt)
	case mtproto.PEER_CHAT:
		return c.sendChatMessages(in.UserId, in.AuthKeyId, in.PeerId, in.GetMessage(), opt)
	case mtproto.PEER_CHANNEL:
		return c.sendChannelMessages(in.UserId, in.AuthKeyId, in.PeerId, in.GetMessage(), opt)
	}
	c.Logger.Errorf("msg.sendMessageV2 - error: invalid peer %d/%d", in.PeerType, in.PeerId)
	return nil, mtproto.ErrPeerIdInvalid
}

// queueView is how a queue's update is seen by the sender and the audience
// (the other admins of a broadcast channel) of a queue.
type queueView struct {
	peer     *mtproto.Peer // the dialog in updateDeleteScheduledMessages
	audience []int64
	users    func(viewerId int64) []*mtproto.User
	chats    func(viewerId int64) []*mtproto.Chat
}

// viewScheduled is a waiting message as viewerId sees it: out only for its
// sender (Android canEditMessageScheduleTime reads message.out so).
func viewScheduled(m *mtproto.Message, senderId, viewerId int64) *mtproto.Message {
	v := proto.Clone(m).(*mtproto.Message)
	v.Out = m.Out && senderId == viewerId
	return v
}

// emitScheduled pushes updates of a queue, built per viewer, to the sender's
// devices but the one on authKeyId (0: every one) and to the audience.
func (c *MsgCore) emitScheduled(b *userbox.Box, senderId, authKeyId int64, v queueView, us func(viewerId int64) []*mtproto.Update) error {
	now := int32(b.Date())
	if err := c.svcCtx.Dao.EmitToDevices(b, senderId, authKeyId, dao.UpdatesOf(us(senderId), v.users(senderId), v.chats(senderId), now)); err != nil {
		return err
	}
	for _, id := range v.audience {
		if id == senderId {
			continue
		}
		if err := c.svcCtx.Dao.EmitPushUpdates(b, id, false, dao.UpdatesOf(us(id), v.users(id), v.chats(id), now)); err != nil {
			return err
		}
	}
	return nil
}

func noUsers(int64) []*mtproto.User { return []*mtproto.User{} }
func noChats(int64) []*mtproto.Chat { return []*mtproto.Chat{} }

// queueAudience are the admins of a broadcast channel other than senderId.
func (c *MsgCore) queueAudience(q dao.ScheduledQueue, senderId int64) ([]int64, error) {
	if !q.Broadcast() {
		return nil, nil
	}
	admins, err := c.svcCtx.Dao.ScheduledBroadcastAdmins(c.ctx, q.PeerId)
	if err != nil {
		return nil, err
	}
	out := make([]int64, 0, len(admins))
	for _, id := range admins {
		if id != senderId {
			out = append(out, id)
		}
	}
	return out, nil
}

// channelChats is the channel as each viewer sees it.
func (c *MsgCore) channelChats(channelId int64, viewers []int64) (func(int64) []*mtproto.Chat, error) {
	mc, err := c.svcCtx.Dao.ChannelClient.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: channelId, Id: viewers})
	if err != nil {
		return nil, err
	}
	now := int32(time.Now().Unix())
	return func(v int64) []*mtproto.Chat { return []*mtproto.Chat{mc.ToChat(v, now)} }, nil
}

// scheduleMessages schedules in's messages for date (task 061), or sends
// them right away when the date is near or the peer is online already.
func (c *MsgCore) scheduleMessages(in *msg.TLMsgSendMessageV2, date int32) (*mtproto.Updates, error) {
	now := int64(int32(time.Now().Unix()))
	repeat, err := repeatPeriodOf(in.GetMessage(), date)
	if err != nil {
		return nil, err
	}
	switch {
	case date == dao.ScheduleWhenOnline:
		online, err := c.peerOnline(in.UserId, in.PeerType, in.PeerId)
		if err != nil {
			return nil, err
		}
		if online {
			return c.send(in, sendOptions{})
		}
	case date < 0:
		return nil, mtproto.ErrScheduleDateInvalid
	case int64(date) < now+scheduleMinAhead && repeat == 0:
		// A repeating message stays in the queue, which the worker empties
		// on its next tick: only a delivery schedules the next one.
		return c.send(in, sendOptions{})
	case int64(date) > now+scheduleMaxAhead:
		return nil, mtproto.ErrScheduleDateTooLate
	}

	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: []int64{in.UserId}, To: []int64{in.UserId}})
	if err != nil {
		return nil, err
	}
	if u, ok := users.GetImmutableUser(in.UserId); ok && u.IsBot() {
		return nil, mtproto.ErrScheduleBotNotAllowed
	}

	switch in.PeerType {
	case mtproto.PEER_USER:
		return c.schedulePrivate(in, date)
	case mtproto.PEER_CHAT:
		return c.scheduleChat(in, date)
	case mtproto.PEER_CHANNEL:
		return c.scheduleChannel(in, date)
	}
	return nil, mtproto.ErrPeerIdInvalid
}

// peerOnline answers for a message to send when the peer is online: whether
// the peer is online now. The message must go to a private chat with
// someone else; a peer whose last-seen the sender cannot see is
// SCHEDULE_STATUS_PRIVATE (api/scheduled-messages).
func (c *MsgCore) peerOnline(userId int64, peerType int32, peerId int64) (bool, error) {
	if peerType != mtproto.PEER_USER || peerId == userId {
		return false, mtproto.ErrScheduleDateInvalid
	}
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: []int64{userId, peerId}, To: []int64{userId, peerId}})
	if err != nil {
		return false, err
	}
	sender, ok1 := users.GetImmutableUser(userId)
	peer, ok2 := users.GetImmutableUser(peerId)
	if !ok1 || !ok2 {
		return false, mtproto.ErrPeerIdInvalid
	}
	switch peer.StatusFor(sender).GetPredicateName() {
	case mtproto.Predicate_userStatusOnline:
		return true, nil
	case mtproto.Predicate_userStatusOffline:
		return false, nil
	}
	return false, mtproto.ErrScheduleStatusPrivate
}

// prepareScheduled makes the messages as they wait: their date and out
// flag, as SendOutgoing writes them when they go out.
func prepareScheduled(list []*msg.OutboxMessage, peerType int32, peerId, fromId int64, date int32) []dao.Outgoing {
	self := peerType == mtproto.PEER_USER && peerId == fromId
	out := make([]dao.Outgoing, len(list))
	for i, o := range list {
		m := o.Message
		m.Date = date
		m.Out = !self
		if self {
			m.FromId = nil
		}
		out[i] = dao.Outgoing{RandomId: o.RandomId, Message: m}
	}
	return out
}

// schedulePrivate schedules messages to a private chat, or to the sender's
// Saved Messages (a reminder).
func (c *MsgCore) schedulePrivate(in *msg.TLMsgSendMessageV2, date int32) (*mtproto.Updates, error) {
	users, err := c.privateUsers(in.UserId, in.PeerId, in.GetMessage())
	if err != nil {
		return nil, err
	}
	hasBot := func() bool {
		found := false
		users.Visit(func(it *mtproto.ImmutableUser) {
			if it.IsBot() {
				found = true
			}
		})
		return found
	}
	for _, o := range in.GetMessage() {
		o.Message = plugin.RemakeMessage(c.ctx, c.svcCtx.MsgPlugin, o.Message, in.UserId, o.NoWebpage, hasBot)
	}
	idHelper := mtproto.NewIDListHelper(in.UserId, in.PeerId)
	for _, o := range in.GetMessage() {
		idHelper.PickByMessage(o.GetMessage())
	}
	return c.enqueue(in, dao.ScheduledQueueOf(in.UserId, mtproto.PEER_USER, in.PeerId, false), date, nil, queueView{
		peer:  mtproto.MakePeerUser(in.PeerId),
		users: func(v int64) []*mtproto.User { return users.GetUserListByIdList(v, idHelper.UserIdList...) },
		chats: noChats,
	})
}

// scheduleChat schedules messages to a basic group.
func (c *MsgCore) scheduleChat(in *msg.TLMsgSendMessageV2, date int32) (*mtproto.Updates, error) {
	idHelper := mtproto.NewIDListHelper(in.UserId)
	for _, o := range in.GetMessage() {
		idHelper.PickByMessage(o.GetMessage())
	}
	chat, users, err := c.chatAndUsers(in.PeerId, idHelper)
	if err != nil {
		return nil, err
	}
	if p, ok := chat.GetImmutableChatParticipant(in.UserId); !ok || !p.IsChatMemberStateNormal() || chat.GetChat().GetMigratedTo() != nil {
		return nil, mtproto.ErrChatWriteForbidden
	}
	hasBot := func() bool {
		found := false
		chat.Walk(func(userId int64, participant *mtproto.ImmutableChatParticipant) error {
			if participant.IsBot {
				found = true
			}
			return nil
		})
		return found
	}
	for _, o := range in.GetMessage() {
		o.Message = plugin.RemakeMessage(c.ctx, c.svcCtx.MsgPlugin, o.Message, in.UserId, o.NoWebpage, hasBot)
	}
	return c.enqueue(in, dao.ScheduledQueueOf(in.UserId, mtproto.PEER_CHAT, in.PeerId, false), date, nil, queueView{
		peer:  mtproto.MakePeerChat(in.PeerId),
		users: users.For,
		chats: func(v int64) []*mtproto.Chat { return []*mtproto.Chat{chat.ToUnsafeChat(v)} },
	})
}

// scheduleChannel schedules messages to a channel or a supergroup. The
// sender's right to send them is checked with the channel locked, as a send
// is (dao.CheckChannelSchedule).
func (c *MsgCore) scheduleChannel(in *msg.TLMsgSendMessageV2, date int32) (*mtproto.Updates, error) {
	mc, err := c.svcCtx.Dao.ChannelClient.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: in.PeerId, Id: []int64{in.UserId}})
	if err != nil {
		return nil, err
	}
	q := dao.ScheduledQueueOf(in.UserId, mtproto.PEER_CHANNEL, in.PeerId, mc.GetChannel().GetBroadcast())
	audience, err := c.queueAudience(q, in.UserId)
	if err != nil {
		return nil, err
	}
	chats, err := c.channelChats(in.PeerId, append([]int64{in.UserId}, audience...))
	if err != nil {
		return nil, err
	}
	h := mtproto.NewIDListHelper(in.UserId)
	for _, o := range in.GetMessage() {
		if q.Broadcast() {
			// A post has no author (dao.SendChannel), and none while it waits.
			o.Message.Post = true
			o.Message.FromId = nil
		}
		o.Message.PeerId = mtproto.MakePeerChannel(in.PeerId)
		h.PickByMessage(o.GetMessage())
	}
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: h.UserIdList, To: append([]int64{in.UserId}, audience...)})
	if err != nil {
		return nil, err
	}
	if err = c.ghostJoins(in.UserId, in.PeerId); err != nil {
		return nil, err
	}
	check := func(b *userbox.Box) error {
		return c.svcCtx.Dao.CheckChannelSchedule(b, in.PeerId, in.GetMessage())
	}
	return c.enqueue(in, q, date, check, queueView{
		peer:     mtproto.MakePeerChannel(in.PeerId),
		audience: audience,
		users:    func(v int64) []*mtproto.User { return users.GetUserListByIdList(v, h.UserIdList...) },
		chats:    chats,
	})
}

// enqueue writes the messages to the queue in the sender's box transaction
// and tells the sender's other devices and the audience. A schedule
// repeated with the same random ids gets the first answer; one after the
// message was sent is RANDOM_ID_DUPLICATE.
func (c *MsgCore) enqueue(in *msg.TLMsgSendMessageV2, q dao.ScheduledQueue, date int32, check func(b *userbox.Box) error, v queueView) (*mtproto.Updates, error) {
	repeat, err := randomIds(in.GetMessage())
	if err != nil {
		return nil, err
	}
	msgs := prepareScheduled(in.GetMessage(), in.PeerType, in.PeerId, in.UserId, date)

	var rows []*dao.ScheduledRow
	_, err = c.svcCtx.Dao.RunBox(c.ctx, in.UserId, nil, func(b *userbox.Box) error {
		var err error
		if rows, err = c.svcCtx.Dao.ScheduledBefore(b, repeat); err != nil || rows != nil {
			return err
		}
		if err = c.refuseSent(b, in, repeat); err != nil {
			return err
		}
		if check != nil {
			if err = check(b); err != nil {
				return err
			}
		}
		if rows, err = c.svcCtx.Dao.InsertScheduled(b, q, date, msgs); err != nil {
			return err
		}
		messages, err := scheduledMessages(rows)
		if err != nil {
			return err
		}
		return c.emitScheduled(b, in.UserId, in.AuthKeyId, v, func(viewer int64) []*mtproto.Update {
			return dao.NewScheduledUpdates(viewAll(messages, rows, viewer))
		})
	})
	if err != nil {
		return nil, err
	}
	messages, err := scheduledMessages(rows)
	if err != nil {
		return nil, err
	}
	shown := viewAll(messages, rows, in.UserId)
	updates := make([]*mtproto.Update, 0, 2*len(shown))
	for i, m := range shown {
		if rows[i].RandomId != 0 {
			updates = append(updates, mtproto.MakeTLUpdateMessageID(&mtproto.Update{Id_INT32: m.Id, RandomId: rows[i].RandomId}).To_Update())
		}
		updates = append(updates, mtproto.MakeTLUpdateNewScheduledMessage(&mtproto.Update{Message_MESSAGE: m}).To_Update())
	}
	return dao.UpdatesOf(updates, v.users(in.UserId), v.chats(in.UserId), int32(time.Now().Unix())), nil
}

// refuseSent refuses a schedule whose random ids went out already.
func (c *MsgCore) refuseSent(b *userbox.Box, in *msg.TLMsgSendMessageV2, repeat []int64) error {
	if in.PeerType == mtproto.PEER_CHANNEL {
		sent, err := c.svcCtx.Dao.ChannelSentBefore(b, in.PeerId, repeat)
		if err != nil {
			return err
		}
		if sent != nil {
			return mtproto.ErrRandomIdDuplicate
		}
		return nil
	}
	sent, err := c.svcCtx.Dao.SentBefore(b, in.PeerType, in.PeerId, repeat)
	if err != nil {
		return err
	}
	if sent != nil {
		return mtproto.ErrRandomIdDuplicate
	}
	return nil
}

func scheduledMessages(rows []*dao.ScheduledRow) ([]*mtproto.Message, error) {
	out := make([]*mtproto.Message, len(rows))
	for i, r := range rows {
		m, err := r.Message()
		if err != nil {
			return nil, err
		}
		out[i] = m
	}
	return out, nil
}

func viewAll(messages []*mtproto.Message, rows []*dao.ScheduledRow, viewer int64) []*mtproto.Message {
	out := make([]*mtproto.Message, len(messages))
	for i, m := range messages {
		out[i] = viewScheduled(m, rows[i].SenderUserId, viewer)
	}
	return out
}

// MsgGetScheduledHistory
// msg.getScheduledHistory user_id:long peer_type:int peer_id:long hash:long = messages.Messages;
func (c *MsgCore) MsgGetScheduledHistory(in *msg.TLMsgGetScheduledHistory) (*mtproto.Messages_Messages, error) {
	peerType, peerId := selfPeer(in.UserId, in.PeerType, in.PeerId)
	q, err := c.svcCtx.Dao.ScheduledQueueFor(c.ctx, in.UserId, peerType, peerId)
	if err != nil {
		c.Logger.Errorf("msg.getScheduledHistory - error: %v", err)
		return nil, err
	}
	rows, err := c.svcCtx.Dao.LoadScheduledQueue(c.ctx, q)
	if err != nil {
		return nil, err
	}
	dao.SortScheduled(rows)
	if in.Hash != 0 && in.Hash == dao.ScheduledHash(rows) {
		return mtproto.MakeTLMessagesMessagesNotModified(&mtproto.Messages_Messages{Count: int32(len(rows))}).To_Messages_Messages(), nil
	}
	return c.scheduledMessagesAnswer(in.UserId, peerType, peerId, rows)
}

// MsgGetScheduledMessages
// msg.getScheduledMessages user_id:long peer_type:int peer_id:long id:Vector<int> = messages.Messages;
func (c *MsgCore) MsgGetScheduledMessages(in *msg.TLMsgGetScheduledMessages) (*mtproto.Messages_Messages, error) {
	peerType, peerId := selfPeer(in.UserId, in.PeerType, in.PeerId)
	q, err := c.svcCtx.Dao.ScheduledQueueFor(c.ctx, in.UserId, peerType, peerId)
	if err != nil {
		c.Logger.Errorf("msg.getScheduledMessages - error: %v", err)
		return nil, err
	}
	rows, err := c.svcCtx.Dao.LoadScheduledById(c.ctx, q, in.Id)
	if err != nil {
		return nil, err
	}
	return c.scheduledMessagesAnswer(in.UserId, peerType, peerId, rows)
}

func (c *MsgCore) scheduledMessagesAnswer(userId int64, peerType int32, peerId int64, rows []*dao.ScheduledRow) (*mtproto.Messages_Messages, error) {
	messages, err := scheduledMessages(rows)
	if err != nil {
		return nil, err
	}
	shown := viewAll(messages, rows, userId)
	h := mtproto.NewIDListHelper(userId)
	h.PickByPeer(queuePeer(peerType, peerId))
	for _, m := range shown {
		h.PickByMessage(m)
	}
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: h.UserIdList, To: []int64{userId}})
	if err != nil {
		return nil, err
	}
	chats, err := c.namedChats(userId, h)
	if err != nil {
		return nil, err
	}
	return mtproto.MakeTLMessagesMessages(&mtproto.Messages_Messages{
		Messages: shown,
		Chats:    chats,
		Users:    users.GetUserListByIdList(userId, h.UserIdList...),
	}).To_Messages_Messages(), nil
}

// namedChats are the chats and channels h holds as userId sees them: the
// dialog of a queue and those its messages name, a forward from a channel
// for one, without which the clients have no name to show for it.
func (c *MsgCore) namedChats(userId int64, h *mtproto.IDListHelper) ([]*mtproto.Chat, error) {
	chats := []*mtproto.Chat{}
	if len(h.ChatIdList) > 0 {
		list, err := c.svcCtx.Dao.ChatClient.ChatGetChatListByIdList(c.ctx, &chatpb.TLChatGetChatListByIdList{SelfId: userId, IdList: h.ChatIdList})
		if err != nil {
			return nil, err
		}
		chats = append(chats, list.GetChatListByIdList(userId, h.ChatIdList...)...)
	}
	if len(h.ChannelIdList) > 0 {
		list, err := c.svcCtx.Dao.ChannelClient.ChannelGetChannelListByIdList(c.ctx, &channelpb.TLChannelGetChannelListByIdList{SelfId: userId, IdList: h.ChannelIdList})
		if err != nil {
			return nil, err
		}
		chats = append(chats, list.GetChannelListByIdList(userId)...)
	}
	return chats, nil
}

// queueChats is the chat of a dialog's queue as each viewer sees it.
func (c *MsgCore) queueChats(userId int64, peerType int32, peerId int64) (func(int64) []*mtproto.Chat, error) {
	switch peerType {
	case mtproto.PEER_CHAT:
		chat, err := c.svcCtx.Dao.ChatClient.ChatGetMutableChat(c.ctx, &chatpb.TLChatGetMutableChat{ChatId: peerId})
		if err != nil {
			return nil, err
		}
		return func(v int64) []*mtproto.Chat { return []*mtproto.Chat{chat.ToUnsafeChat(v)} }, nil
	case mtproto.PEER_CHANNEL:
		return c.channelChats(peerId, []int64{userId})
	}
	return noChats, nil
}

// MsgHasScheduled
// msg.hasScheduled user_id:long peer_type:int peer_id:long = Bool;
func (c *MsgCore) MsgHasScheduled(in *msg.TLMsgHasScheduled) (*mtproto.Bool, error) {
	peerType, peerId := selfPeer(in.UserId, in.PeerType, in.PeerId)
	q, err := c.svcCtx.Dao.ScheduledQueueFor(c.ctx, in.UserId, peerType, peerId)
	if err != nil {
		if errors.Is(err, mtproto.ErrChatAdminRequired) {
			return mtproto.BoolFalse, nil
		}
		return nil, err
	}
	has, err := c.svcCtx.Dao.HasScheduled(c.ctx, q)
	if err != nil {
		return nil, err
	}
	return mtproto.ToBool(has), nil
}

// editScheduled edits a waiting message (messages.editMessage with
// schedule_date): its content, and its date. The answer and the pushes are
// updateNewScheduledMessage with the same id (api/scheduled-messages).
func (c *MsgCore) editScheduled(in *msg.TLMsgEditMessageV2, id int32) (*mtproto.Updates, error) {
	date := in.NewMessage.GetScheduleDate().GetValue()
	now := int64(int32(time.Now().Unix()))
	switch {
	case date == dao.ScheduleWhenOnline:
		// Only the conditions of a schedule count: a peer online now gets
		// the message from the worker's next look.
		if _, err := c.peerOnline(in.UserId, in.PeerType, in.PeerId); err != nil {
			return nil, err
		}
	case date <= 0, int64(date) <= now:
		return nil, mtproto.ErrScheduleDateInvalid
	case int64(date) > now+scheduleMaxAhead:
		return nil, mtproto.ErrScheduleDateTooLate
	}

	if _, err := repeatPeriodOf([]*msg.OutboxMessage{in.NewMessage}, date); err != nil {
		return nil, err
	}

	edit := plugin.RemakeMessage(c.ctx, c.svcCtx.MsgPlugin, in.NewMessage.Message, in.UserId, in.NewMessage.NoWebpage, func() bool { return false })

	peerType, peerId := selfPeer(in.UserId, in.PeerType, in.PeerId)
	q, err := c.svcCtx.Dao.ScheduledQueueFor(c.ctx, in.UserId, peerType, peerId)
	if err != nil {
		return nil, err
	}
	view, err := c.scheduledEditView(in.UserId, q, peerType, peerId)
	if err != nil {
		return nil, err
	}

	var (
		row    *dao.ScheduledRow
		edited *mtproto.Message
	)
	_, err = c.svcCtx.Dao.RunBox(c.ctx, in.UserId, nil, func(b *userbox.Box) error {
		_, rows, err := c.svcCtx.Dao.LockScheduledFor(b, peerType, peerId, []int32{id}, dao.ScheduledOpEdit)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return mtproto.ErrMessageIdInvalid
		}
		row = rows[0]
		// A delivery took it: it is going out with what it held.
		if row.ClaimUntil > b.Date() {
			return mtproto.ErrMessageIdInvalid
		}
		stored, err := row.Message()
		if err != nil {
			return err
		}
		edited = dao.ApplyScheduledEdit(stored, edit, date)
		if err = c.svcCtx.Dao.EditScheduled(b, row.Id, date, edited); err != nil {
			return err
		}
		return c.emitScheduled(b, in.UserId, in.AuthKeyId, view, func(viewer int64) []*mtproto.Update {
			return dao.NewScheduledUpdates([]*mtproto.Message{viewScheduled(edited, row.SenderUserId, viewer)})
		})
	})
	if err != nil {
		return nil, err
	}
	return dao.UpdatesOf(dao.NewScheduledUpdates([]*mtproto.Message{viewScheduled(edited, row.SenderUserId, in.UserId)}), view.users(in.UserId), view.chats(in.UserId), int32(time.Now().Unix())), nil
}

// scheduledEditView is how an edit in q is shown: the peer and the audience
// of the queue, the users and the chat of the dialog.
func (c *MsgCore) scheduledEditView(userId int64, q dao.ScheduledQueue, peerType int32, peerId int64) (queueView, error) {
	audience, err := c.queueAudience(q, userId)
	if err != nil {
		return queueView{}, err
	}
	viewers := append([]int64{userId}, audience...)
	var chats func(int64) []*mtproto.Chat
	if peerType == mtproto.PEER_CHANNEL {
		chats, err = c.channelChats(peerId, viewers)
	} else {
		chats, err = c.queueChats(userId, peerType, peerId)
	}
	if err != nil {
		return queueView{}, err
	}
	h := mtproto.NewIDListHelper(userId)
	if peerType == mtproto.PEER_USER {
		h.AppendUsers(peerId)
	}
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: h.UserIdList, To: viewers})
	if err != nil {
		return queueView{}, err
	}
	return queueView{
		peer:     queuePeer(peerType, peerId),
		audience: audience,
		users:    func(v int64) []*mtproto.User { return users.GetUserListByIdList(v, h.UserIdList...) },
		chats:    chats,
	}, nil
}

func queuePeer(peerType int32, peerId int64) *mtproto.Peer {
	switch peerType {
	case mtproto.PEER_CHAT:
		return mtproto.MakePeerChat(peerId)
	case mtproto.PEER_CHANNEL:
		return mtproto.MakePeerChannel(peerId)
	}
	return mtproto.MakePeerUser(peerId)
}

// MsgDeleteScheduledMessages
// msg.deleteScheduledMessages user_id:long auth_key_id:long peer_type:int peer_id:long id:Vector<int> = Updates;
//
// Takes messages off the queue; those not in it are left out.
func (c *MsgCore) MsgDeleteScheduledMessages(in *msg.TLMsgDeleteScheduledMessages) (*mtproto.Updates, error) {
	peerType, peerId := selfPeer(in.UserId, in.PeerType, in.PeerId)
	q, err := c.svcCtx.Dao.ScheduledQueueFor(c.ctx, in.UserId, peerType, peerId)
	if err != nil {
		c.Logger.Errorf("msg.deleteScheduledMessages - error: %v", err)
		return nil, err
	}
	audience, err := c.queueAudience(q, in.UserId)
	if err != nil {
		return nil, err
	}
	var ids []int32
	v := queueView{peer: queuePeer(peerType, peerId), audience: audience, users: noUsers, chats: noChats}
	_, err = c.svcCtx.Dao.RunBox(c.ctx, in.UserId, nil, func(b *userbox.Box) error {
		_, rows, err := c.svcCtx.Dao.LockScheduledFor(b, peerType, peerId, in.Id, dao.ScheduledOpDelete)
		if err != nil {
			return err
		}
		ids = dao.ScheduledIds(rows)
		if len(ids) == 0 {
			return nil
		}
		if err = c.svcCtx.Dao.DeleteScheduled(b, ids); err != nil {
			return err
		}
		u := dao.DeleteScheduledUpdate(v.peer, ids, nil)
		return c.emitScheduled(b, in.UserId, in.AuthKeyId, v, func(int64) []*mtproto.Update { return []*mtproto.Update{u} })
	})
	if err != nil {
		return nil, err
	}
	now := int32(time.Now().Unix())
	if len(ids) == 0 {
		return dao.UpdatesOf([]*mtproto.Update{}, []*mtproto.User{}, []*mtproto.Chat{}, now), nil
	}
	return dao.UpdatesOf([]*mtproto.Update{dao.DeleteScheduledUpdate(v.peer, ids, nil)}, []*mtproto.User{}, []*mtproto.Chat{}, now), nil
}

// MsgSendScheduledMessages
// msg.sendScheduledMessages user_id:long auth_key_id:long peer_type:int peer_id:long id:Vector<int> = Updates;
//
// Sends messages of the queue right away. Each album and each other message
// goes as one send of its own.
func (c *MsgCore) MsgSendScheduledMessages(in *msg.TLMsgSendScheduledMessages) (*mtproto.Updates, error) {
	peerType, peerId := selfPeer(in.UserId, in.PeerType, in.PeerId)
	var rows []*dao.ScheduledRow
	_, err := c.svcCtx.Dao.RunBox(c.ctx, in.UserId, nil, func(b *userbox.Box) error {
		var err error
		_, rows, err = c.svcCtx.Dao.LockScheduledFor(b, peerType, peerId, in.Id, dao.ScheduledOpSend)
		return err
	})
	if err != nil {
		c.Logger.Errorf("msg.sendScheduledMessages - error: %v", err)
		return nil, err
	}
	if len(rows) == 0 {
		return nil, mtproto.ErrMessageIdInvalid
	}

	out := dao.UpdatesOf([]*mtproto.Update{}, []*mtproto.User{}, []*mtproto.Chat{}, int32(time.Now().Unix()))
	for _, unit := range deliveryUnits(rows) {
		// The requester's device gets it in the answer: its own key.
		key := int64(0)
		if unit[0].SenderUserId == in.UserId {
			key = in.AuthKeyId
		}
		r, err := c.deliverScheduled(unit, key, false)
		if err != nil {
			if errors.Is(err, dao.ErrScheduledGone) {
				continue
			}
			c.Logger.Errorf("msg.sendScheduledMessages - error: %v", err)
			return nil, err
		}
		if unit[0].SenderUserId != in.UserId {
			r = queueUpdatesOf(r)
		}
		out.Updates = append(out.Updates, r.Updates...)
		out.Users = append(out.Users, r.Users...)
		out.Chats = append(out.Chats, r.Chats...)
	}
	return out, nil
}

// queueUpdatesOf is what is left of a delivery's answer for an admin who sent
// another admin's post: the send was made in the author's name, so its
// message and chat are as the author sees them (out, the author's rights).
// The admin gets the post as any member does, by the channel's push.
func queueUpdatesOf(r *mtproto.Updates) *mtproto.Updates {
	us := make([]*mtproto.Update, 0, 1)
	for _, u := range r.GetUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateDeleteScheduledMessages {
			us = append(us, u)
		}
	}
	return dao.UpdatesOf(us, []*mtproto.User{}, []*mtproto.Chat{}, r.GetDate())
}

// deliveryUnits splits rows into what goes as one send: an album whole,
// every other message alone. Rows of one send share a sender.
func deliveryUnits(rows []*dao.ScheduledRow) [][]*dao.ScheduledRow {
	var (
		out    [][]*dao.ScheduledRow
		albums = map[[2]int64]int{}
	)
	for _, r := range rows {
		if r.GroupedId == 0 {
			out = append(out, []*dao.ScheduledRow{r})
			continue
		}
		k := [2]int64{r.SenderUserId, r.GroupedId}
		if i, ok := albums[k]; ok {
			out[i] = append(out[i], r)
			continue
		}
		albums[k] = len(out)
		out = append(out, []*dao.ScheduledRow{r})
	}
	return out
}

// deliverScheduled sends rows, one queue's messages of one sender, through
// the normal send path of their dialog, and in the same transaction takes
// them off the queue and tells the sender's devices and the audience. The
// rows are gone exactly when the messages are out: ErrScheduledGone when
// someone took them first. authKeyId is the requester's key of a manual
// send, 0 for the worker's. Waiting messages leave with from_scheduled when
// they go at their time (the worker's fromScheduled), so the clients tell
// the sender.
func (c *MsgCore) deliverScheduled(rows []*dao.ScheduledRow, authKeyId int64, fromScheduled bool) (*mtproto.Updates, error) {
	q := rows[0].Queue()
	senderId := rows[0].SenderUserId
	peerType, peerId := rows[0].PeerType, rows[0].PeerId
	ids := dao.ScheduledIds(rows)
	audience, err := c.queueAudience(q, senderId)
	if err != nil {
		return nil, err
	}
	outBox := make([]*msg.OutboxMessage, len(rows))
	now := int32(time.Now().Unix())
	var next *dao.Outgoing
	for i, r := range rows {
		m, err := r.Message()
		if err != nil {
			return nil, err
		}
		if m.GetScheduleRepeatPeriod().GetValue() != 0 {
			next = &dao.Outgoing{Message: proto.Clone(m).(*mtproto.Message)}
			m.ScheduleRepeatPeriod = nil
		}
		m.Id = 0
		m.Date = now
		m.FromScheduled = fromScheduled
		outBox[i] = &msg.OutboxMessage{RandomId: r.RandomId, Message: m}
	}

	v := queueView{peer: queuePeer(peerType, peerId), audience: audience, users: noUsers, chats: noChats}
	var (
		deleted *mtproto.Update
		again   *mtproto.Message
	)
	in := &msg.TLMsgSendMessageV2{UserId: senderId, AuthKeyId: authKeyId, PeerType: peerType, PeerId: peerId, Message: outBox}
	// A restriction may have come after the message was scheduled; for the
	// worker it is final, as for a sender who lost the chat (dropScheduled).
	if err = c.checkRestriction(in); err != nil {
		return nil, err
	}
	updates, err := c.send(in, sendOptions{
		queued:        true,
		fromScheduled: fromScheduled,
		after: func(b *userbox.Box, sent []int32) error {
			if err := c.svcCtx.Dao.DeleteScheduled(b, ids); err != nil {
				return err
			}
			deleted = dao.DeleteScheduledUpdate(v.peer, ids, sent)
			if err := c.emitScheduled(b, senderId, authKeyId, v, func(int64) []*mtproto.Update { return []*mtproto.Update{deleted} }); err != nil {
				return err
			}
			if next == nil {
				return nil
			}
			var err error
			again, err = c.scheduleNext(b, q, senderId, authKeyId, *next, v)
			return err
		},
	})
	if err != nil {
		return nil, err
	}
	reply := updates.Updates[:0]
	for _, u := range updates.Updates {
		// The random ids mean nothing to the device that asks now.
		if u.GetPredicateName() != mtproto.Predicate_updateMessageID {
			reply = append(reply, u)
		}
	}
	updates.Updates = append(reply, deleted)
	if again != nil {
		updates.Updates = append(updates.Updates, dao.NewScheduledUpdates([]*mtproto.Message{viewScheduled(again, senderId, senderId)})...)
	}
	return updates, nil
}

// scheduleNext puts a repeating message back in its queue, in the
// transaction of the send that just took it off: the next date is "the
// current time plus schedule_repeat_period", with "all the same updates as
// above, as if we had re-invoked sendMessage" (core.telegram.org/api/
// scheduled-messages), so a new queue id and updateNewScheduledMessage. A
// crash can neither lose the series nor send a message twice. It returns
// the message as it now waits.
func (c *MsgCore) scheduleNext(b *userbox.Box, q dao.ScheduledQueue, senderId, authKeyId int64, next dao.Outgoing, v queueView) (*mtproto.Message, error) {
	date := int32(b.Date()) + next.Message.GetScheduleRepeatPeriod().GetValue()
	rows, err := c.svcCtx.Dao.InsertScheduled(b, q, date, []dao.Outgoing{next})
	if err != nil {
		return nil, err
	}
	m, err := rows[0].Message()
	if err != nil {
		return nil, err
	}
	return m, c.emitScheduled(b, senderId, authKeyId, v, func(viewer int64) []*mtproto.Update {
		return dao.NewScheduledUpdates([]*mtproto.Message{viewScheduled(m, senderId, viewer)})
	})
}

// permanentSendError tells an error no later attempt can change: the sender
// or the chat no longer allows the message. Anything else (a database or
// network failure) is worth another attempt.
func permanentSendError(err error) bool {
	if errors.Is(err, dao.ErrScheduledGone) {
		return false
	}
	switch status.Code(err) {
	case mtproto.ErrBadRequest, mtproto.ErrForbidden, codes.Code(406):
		return true
	}
	return false
}

// dropScheduled takes rows that can never go out off the queue. The sender
// and the audience see them disappear (updateDeleteScheduledMessages without
// sent_messages).
func (c *MsgCore) dropScheduled(rows []*dao.ScheduledRow) error {
	senderId := rows[0].SenderUserId
	q := rows[0].Queue()
	audience, err := c.queueAudience(q, senderId)
	if err != nil {
		return err
	}
	ids := dao.ScheduledIds(rows)
	v := queueView{peer: queuePeer(rows[0].PeerType, rows[0].PeerId), audience: audience, users: noUsers, chats: noChats}
	_, err = c.svcCtx.Dao.RunBox(c.ctx, senderId, nil, func(b *userbox.Box) error {
		if err := c.svcCtx.Dao.DeleteScheduled(b, ids); err != nil {
			return err
		}
		u := dao.DeleteScheduledUpdate(v.peer, ids, nil)
		return c.emitScheduled(b, senderId, 0, v, func(int64) []*mtproto.Update { return []*mtproto.Update{u} })
	})
	return err
}
