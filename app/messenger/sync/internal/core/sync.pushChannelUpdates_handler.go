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
	"github.com/teamgram/teamgram-server/pkg/botprivacy"
	"github.com/teamgram/teamgram-server/pkg/channelcontents"
	"slices"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"google.golang.org/protobuf/proto"
)

// SyncPushChannelUpdates
// sync.pushChannelUpdates channel_id:long from_id:long from_auth_key_id:long mentioned:Vector<long> updates:Updates author_id:long = Void;
//
// The updates of a channel go to the members in it now (task 039, ADR 0034,
// p. 5), each in its own view: its messages out, mentioned where it is
// mentioned, the channel and the users as it sees them. The sender's other
// devices get them as well; the device of from_auth_key_id had them in its
// answer. A push is best effort: a device that missed it catches up with
// updates.getChannelDifference.
func (c *SyncCore) SyncPushChannelUpdates(in *sync.TLSyncPushChannelUpdates) error {
	members, err := c.svcCtx.Dao.Channel.ChannelGetMemberIdList(c.ctx, &channelpb.TLChannelGetMemberIdList{ChannelId: in.ChannelId})
	if err != nil {
		return err
	}
	if len(members.GetDatas()) == 0 {
		return nil
	}
	mc, err := c.svcCtx.Dao.Channel.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: in.ChannelId, Id: members.GetDatas()})
	if err != nil {
		return err
	}
	h := mtproto.NewIDListHelper()
	h.PickByUpdates(in.GetUpdates().GetUpdates()...)
	// The members as well as the users of the messages: each member's view
	// of the users is made from the member's own row
	// (MutableUsers.GetUserListByIdList), and without it the member gets no
	// users at all, not even the sender of the message pushed.
	users, err := c.svcCtx.Dao.User.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{
		Id:      append(append([]int64{}, h.UserIdList...), members.GetDatas()...),
		Privacy: true,
		HasTo:   true,
		To:      members.GetDatas(),
	})
	if err != nil {
		return err
	}
	// The other channels of the messages, the ones members wrote as
	// (send_as, task 079) and the one a post copy comes from, as each
	// member sees them.
	var others []*channelpb.MutableChannel
	for _, id := range h.ChannelIdList {
		if id == in.ChannelId {
			continue
		}
		other, err := c.svcCtx.Dao.Channel.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: id, Id: members.GetDatas()})
		if err != nil {
			return err
		}
		others = append(others, other)
	}
	// The unread contents of the messages, of every member (task 080): an
	// edit keeps a played voice message played for the one who played it.
	var messages []*mtproto.Message
	for _, u := range in.GetUpdates().GetUpdates() {
		if m := u.GetMessage_MESSAGE(); m != nil {
			messages = append(messages, m)
		}
	}
	contents := &channelcontents.State{}
	if len(messages) > 0 {
		if contents, err = channelcontents.LoadAll(c.ctx, c.svcCtx.Dao.Mysql, in.ChannelId, messages); err != nil {
			return err
		}
	}
	now := int32(time.Now().Unix())
	for _, member := range members.GetDatas() {
		chats := []*mtproto.Chat{mc.ToChat(member, now)}
		for _, other := range others {
			chats = append(chats, other.ToChat(member, now))
		}
		updates := c.channelView(in, member, contents, chats, users.GetUserListByIdList(member, h.UserIdList...))
		repliesTo := func(m *mtproto.Message) bool { return c.repliesTo(in.ChannelId, member, m) }
		if updates = forBot(updates, mc, users, member, slices.Contains(in.GetMentioned(), member), repliesTo); updates == nil {
			continue
		}
		if member == in.FromId {
			_, err = c.SyncUpdatesNotMe(&sync.TLSyncUpdatesNotMe{UserId: member, PermAuthKeyId: in.FromAuthKeyId, Updates: updates})
		} else {
			_, err = c.SyncPushUpdates(&sync.TLSyncPushUpdates{UserId: member, Updates: updates})
		}
		if err != nil {
			c.Logger.Errorf("sync.pushChannelUpdates - channel %d to user %d: %v", in.ChannelId, member, err)
		}
	}
	return nil
}

// channelView is the updates of in as member sees them, with the unread
// contents of its messages as member has them (contents).
func (c *SyncCore) channelView(in *sync.TLSyncPushChannelUpdates, member int64, contents *channelcontents.State, chats []*mtproto.Chat, users []*mtproto.User) *mtproto.Updates {
	updates := proto.Clone(in.GetUpdates()).(*mtproto.Updates)
	mentioned := slices.Contains(in.GetMentioned(), member)
	for _, u := range updates.GetUpdates() {
		// The unread marks of reactions are for the author, sent as the
		// mentioned of the push (task 067).
		if u.GetPredicateName() == mtproto.Predicate_updateMessageReactions && !mentioned {
			for _, p := range u.GetReactions_MESSAGEREACTIONS().GetRecentReactions() {
				p.Unread = false
			}
		}
		m := u.GetMessage_MESSAGE()
		if m == nil {
			continue
		}
		// A message written as a channel is out for its author, who sends
		// it and is the one to edit it in a supergroup (task 079), also
		// when another member changes it, as by ticking an item of its
		// checklist (author_id, task 081).
		m.Out = m.GetFromId().GetUserId() == member || member == pushAuthor(in) && channelpb.SentAs(m)
		// A scheduled post tells its sender only that it went out (task 061).
		if member != in.GetFromId() {
			m.FromScheduled = false
		}
		// A voice or round video message is unread for every member but
		// its author until each plays it; a mention until it is read. The
		// author is the stored one where it was read: a post loses its
		// comments by the push of whoever deleted its copy in the
		// discussion group (postCommentsOff).
		own := member == pushAuthor(in)
		if author, ok := contents.Author(m.GetId()); ok {
			own = author == member
		}
		contents.View(m, member, own, true)
		if mentioned && !m.Out {
			m.Mentioned = true
			m.MediaUnread = true
		}
	}
	updates.Chats = chats
	updates.Users = users
	return updates
}

// pushAuthor is who wrote the messages of in written as a channel: author_id,
// or from_id without it.
func pushAuthor(in *sync.TLSyncPushChannelUpdates) int64 {
	if in.GetAuthorId() != 0 {
		return in.GetAuthorId()
	}
	return in.GetFromId()
}

// repliesTo tells whether m replies to a message member sent. A failure of
// the look-up leaves the message out of the push: the gap in pts it makes
// brings the bot to updates.getChannelDifference, which decides again.
func (c *SyncCore) repliesTo(channelId, member int64, m *mtproto.Message) bool {
	id := botprivacy.ReplyToInChat(m)
	if id == 0 {
		return false
	}
	replied, err := c.svcCtx.Dao.Channel.ChannelGetMessages(c.ctx, &channelpb.TLChannelGetMessages{SelfId: member, ChannelId: channelId, Id: []int32{id}})
	if err != nil {
		c.Logger.Errorf("sync.pushChannelUpdates - channel %d, message %d: %v", channelId, id, err)
		return false
	}
	for _, r := range replied.GetDatas() {
		if r.GetFromId().GetUserId() == member {
			return true
		}
	}
	return false
}

// forBot is what of updates the member gets when it is a bot in privacy mode
// of a supergroup: nil when no update remains (pkg/botprivacy; a broadcast
// channel's posts reach all its bots). mentioned holds for the author of the
// message replied to and for the users of the text's mentions, so it only
// tells when the author is worth looking up: repliesTo decides, as the
// channel difference does, and a message that is no reply to the bot is
// hidden from it, as in a basic group.
func forBot(updates *mtproto.Updates, mc *channelpb.MutableChannel, users *mtproto.MutableUsers, member int64, mentioned bool, repliesTo func(*mtproto.Message) bool) *mtproto.Updates {
	u, ok := users.GetImmutableUser(member)
	if !ok || !u.IsBot() || mc.GetChannel().GetBroadcast() {
		return updates
	}
	p, _ := mc.GetParticipant(member)
	bot := botprivacy.Bot{
		Id:       member,
		Username: u.Username(),
		ReadsAll: u.BotChatHistory() || p.GetRole() == channelpb.RoleCreator || p.GetRole() == channelpb.RoleAdmin,
	}
	kept := updates.GetUpdates()[:0]
	for _, up := range updates.GetUpdates() {
		if m := up.GetMessage_MESSAGE(); m != nil {
			sender, _ := users.GetImmutableUser(m.GetFromId().GetUserId())
			if !botprivacy.Visible(bot, m, sender != nil && sender.IsBot(), mentioned && repliesTo(m)) {
				continue
			}
		}
		kept = append(kept, up)
	}
	if len(kept) == 0 {
		return nil
	}
	updates.Updates = kept
	return updates
}
