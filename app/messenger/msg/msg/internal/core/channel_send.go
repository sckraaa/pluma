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
	"slices"
	"strings"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/plugin"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/channelcontents"
	"github.com/teamgram/teamgram-server/pkg/userbox"
	"google.golang.org/protobuf/proto"
)

// sendChannelMessages sends messages of fromUserId to a channel or a
// supergroup (task 039, ADR 0034): one copy each with the channel's next id
// and pts, in the sender's box transaction with the channel locked, and one
// event that pushes them to the members (sync.pushChannelUpdates). A send
// repeated with the same random ids gets the first answer from the
// database, whatever a cache kept. The answer holds updateMessageID and
// updateNewChannelMessage of each message, the channel and the users.
func (c *MsgCore) sendChannelMessages(fromUserId, fromAuthKeyId, channelId int64, outBoxList []*msg.OutboxMessage, opt sendOptions) (*mtproto.Updates, error) {
	randomIds, err := randomIds(outBoxList)
	if err != nil {
		return nil, err
	}
	// The author's name, for a channel that signs its posts (task 044).
	author, err := c.authorName(fromUserId)
	if err != nil {
		return nil, err
	}
	if !opt.queued {
		// The links, mentions, hashtags and bot commands of the text and the
		// card of its first link, as a message to a user or a chat gets them
		// (plugin.RemakeMessage): the clients show only the entities a
		// message carries once it comes from the server, and a mention's
		// user is the one the message notifies. A queued message got them
		// when it was scheduled.
		hasBot := c.channelHasBots(channelId)
		for _, outBox := range outBoxList {
			outBox.Message = plugin.RemakeMessage(c.ctx, c.svcCtx.MsgPlugin, outBox.Message, fromUserId, outBox.NoWebpage, hasBot)
		}
	}
	// The snowflake of each message is the id of a new poll in it.
	outgoing, err := c.outgoing(outBoxList)
	if err != nil {
		return nil, err
	}
	if err = c.ghostJoins(fromUserId, channelId); err != nil {
		return nil, err
	}
	var sent []dao.ChannelSent
	_, err = c.svcCtx.Dao.RunBox(c.ctx, fromUserId, nil, func(b *userbox.Box) error {
		before, err := c.svcCtx.Dao.ChannelSentBefore(b, channelId, randomIds)
		if err != nil || before != nil {
			sent = before
			if err != nil {
				return err
			}
			return opt.runAfter(b, channelSentIds(sent))
		}
		var (
			mentioned []int64
			disc      *dao.Discussion
		)
		if sent, mentioned, disc, err = c.svcCtx.Dao.SendChannel(b, channelId, outgoing, author); err != nil {
			return err
		}
		pushed := sent
		if opt.fromScheduled {
			pushed = flagFromScheduled(sent)
		}
		if err = c.svcCtx.Dao.EmitChannelPush(b, channelId, fromAuthKeyId, pushed, mentioned); err != nil {
			return err
		}
		if disc != nil && len(disc.Copies) > 0 {
			// The copies in the discussion group, to its members: every
			// device of the author too, whose answer holds the posts alone.
			if err = c.svcCtx.Dao.EmitChannelPush(b, disc.GroupId, 0, disc.Copies, nil); err != nil {
				return err
			}
		}
		return opt.runAfter(b, channelSentIds(sent))
	})
	if err != nil {
		return nil, err
	}

	mc, err := c.svcCtx.Dao.ChannelClient.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: channelId, Id: []int64{fromUserId}})
	if err != nil {
		return nil, err
	}
	h := mtproto.NewIDListHelper(fromUserId)
	updates := make([]*mtproto.Update, 0, 2*len(sent))
	for _, s := range sent {
		m := proto.Clone(s.Message).(*mtproto.Message)
		m.Out = true
		h.PickByMessage(m)
		if s.RandomId != 0 {
			updates = append(updates, mtproto.MakeTLUpdateMessageID(&mtproto.Update{Id_INT32: m.Id, RandomId: s.RandomId}).To_Update())
		}
		updates = append(updates, mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{Message_MESSAGE: m, Pts_INT32: s.Pts, PtsCount: 1}).To_Update())
	}
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: h.UserIdList, To: []int64{fromUserId}})
	if err != nil {
		return nil, err
	}
	chats, err := c.sentChats(fromUserId, channelId, mc, h)
	if err != nil {
		return nil, err
	}
	return mtproto.MakeTLUpdates(&mtproto.Updates{
		Updates: updates,
		Users:   users.GetUserListByIdList(fromUserId, h.UserIdList...),
		Chats:   chats,
		Date:    int32(time.Now().Unix()),
	}).To_Updates(), nil
}

// ghostJoins makes userId, when it is the support ghost (ADR 0048) and not
// in the channel it writes to (a message, a reaction, a vote), a hidden
// member of it first, where it may read it without a row (a public chat,
// the comments of a public channel): it comments and reacts without
// subscribing, and the replies to what it wrote reach it as a member's.
// biz channel joins it in a box transaction of its own (channel.joinHidden:
// the row, the dialog read up to the last message, updateChannel with seq
// for its devices; nobody else learns of it), before the write's: a
// transaction calls no service, and a box takes no inbox event it emits
// itself. A write that fails afterwards leaves the ghost in, unseen.
func (c *MsgCore) ghostJoins(userId, channelId int64) error {
	may, err := c.svcCtx.Dao.GhostMayJoin(c.ctx, userId, channelId)
	if err != nil || !may {
		return err
	}
	_, err = c.svcCtx.Dao.ChannelClient.ChannelJoinHidden(c.ctx, &channelpb.TLChannelJoinHidden{UserId: userId, ChannelId: channelId})
	return err
}

// channelSentIds are the ids of sent, in order.
func channelSentIds(sent []dao.ChannelSent) []int32 {
	ids := make([]int32, len(sent))
	for i, s := range sent {
		ids[i] = s.Message.Id
	}
	return ids
}

// flagFromScheduled is sent as pushed to the sender: from_scheduled marks a
// delivery for its sender alone (sync.pushChannelUpdates clears it for the
// others). The stored message and the journal do not carry it.
func flagFromScheduled(sent []dao.ChannelSent) []dao.ChannelSent {
	out := make([]dao.ChannelSent, len(sent))
	for i, s := range sent {
		s.Message = proto.Clone(s.Message).(*mtproto.Message)
		s.Message.FromScheduled = true
		out[i] = s
	}
	return out
}

// editChannelMessage applies fromUserId's edit to message id of a channel or
// a supergroup (task 043, dao.EditChannel): in the editor's box transaction
// with the channel locked, pushed to the members by one event. The answer
// holds updateEditChannelMessage, the channel and the users.
func (c *MsgCore) editChannelMessage(fromUserId, fromAuthKeyId, channelId int64, editBox *msg.OutboxMessage, id int32) (*mtproto.Updates, error) {
	edit := plugin.RemakeMessage(c.ctx, c.svcCtx.MsgPlugin, editBox.Message, fromUserId, editBox.NoWebpage, func() bool { return false })
	var edited *dao.ChannelEdited
	_, err := c.svcCtx.Dao.RunBox(c.ctx, fromUserId, nil, func(b *userbox.Box) error {
		var err error
		if edited, err = c.svcCtx.Dao.EditChannel(b, channelId, id, edit); err != nil {
			return err
		}
		return c.svcCtx.Dao.EmitChannelEditPush(b, channelId, fromAuthKeyId, edited)
	})
	if err != nil {
		return nil, err
	}
	mc, err := c.svcCtx.Dao.ChannelClient.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: channelId, Id: []int64{fromUserId}})
	if err != nil {
		return nil, err
	}
	// A message of a supergroup is edited by its author alone; a post is
	// never written as another (dao.EditChannel).
	shown := channelpb.ViewSentMessage(edited.Message, fromUserId, true)
	// An admin who edits another's post has its own unread contents of it.
	contents, err := channelcontents.Load(c.ctx, c.svcCtx.Dao.Mysql, channelId, fromUserId, []*mtproto.Message{shown})
	if err != nil {
		return nil, err
	}
	contents.View(shown, fromUserId, edited.AuthorId == fromUserId, true)
	h := mtproto.NewIDListHelper(fromUserId)
	h.PickByMessage(shown)
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: h.UserIdList, To: []int64{fromUserId}})
	if err != nil {
		return nil, err
	}
	chats, err := c.sentChats(fromUserId, channelId, mc, h)
	if err != nil {
		return nil, err
	}
	return mtproto.MakeTLUpdates(&mtproto.Updates{
		Updates: []*mtproto.Update{mtproto.MakeTLUpdateEditChannelMessage(&mtproto.Update{Message_MESSAGE: shown, Pts_INT32: edited.Pts, PtsCount: 1}).To_Update()},
		Users:   users.GetUserListByIdList(fromUserId, h.UserIdList...),
		Chats:   chats,
		Date:    int32(time.Now().Unix()),
	}).To_Updates(), nil
}

// sentChats are the chats of an answer about messages of channelId to
// userId: the channel and the other channels h found in the messages, the
// ones they came from (send_as, task 079).
func (c *MsgCore) sentChats(userId, channelId int64, mc *channelpb.MutableChannel, h *mtproto.IDListHelper) ([]*mtproto.Chat, error) {
	chats := []*mtproto.Chat{mc.ToChat(userId, int32(time.Now().Unix()))}
	others := slices.DeleteFunc(slices.Clone(h.ChannelIdList), func(id int64) bool { return id == channelId })
	// The channel of a monoforum goes with it (task 084): the clients read
	// from it what the user is in the monoforum.
	if linked := mc.GetChannel().GetLinkedMonoforumId(); mc.GetChannel().GetMonoforum() && linked != 0 && !slices.Contains(others, linked) {
		others = append(others, linked)
	}
	if len(others) == 0 {
		return chats, nil
	}
	list, err := c.svcCtx.Dao.ChannelClient.ChannelGetChannelListByIdList(c.ctx, &channelpb.TLChannelGetChannelListByIdList{SelfId: userId, IdList: others})
	if err != nil {
		return nil, err
	}
	return append(chats, list.GetChannelListByIdList(userId)...), nil
}

// authorName is the name a signed post shows: the user's first and last
// name, as Telegram signs posts (channels.toggleSignatures).
func (c *MsgCore) authorName(userId int64) (string, error) {
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: []int64{userId}})
	if err != nil {
		return "", err
	}
	u, ok := users.GetImmutableUser(userId)
	if !ok {
		return "", nil
	}
	return strings.TrimSpace(u.FirstName() + " " + u.LastName()), nil
}

// channelHasBots is whether channelId has a bot among its members, read at
// most once per send. A failed read is logged and counts as none: the
// message goes, its commands as plain text.
func (c *MsgCore) channelHasBots(channelId int64) func() bool {
	var (
		read bool
		has  bool
	)
	return func() bool {
		if !read {
			read = true
			var err error
			if has, err = c.svcCtx.Dao.ChannelHasBots(c.ctx, channelId); err != nil {
				c.Logger.Errorf("msg.sendMessageV2 - bots of channel %d: %v", channelId, err)
			}
		}
		return has
	}
}
