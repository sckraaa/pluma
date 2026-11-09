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
	"time"

	"github.com/teamgram/proto/mtproto"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/app/service/biz/message/message"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/channelbox"
	"github.com/teamgram/teamgram-server/pkg/polls"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// makePollMedia checks a poll as sent and makes its message media: the media
// of the answers, the attached and the solution media become message media
// (api/poll).
func (c *MessagesCore) makePollMedia(in *mtproto.InputMedia) (*mtproto.MessageMedia, error) {
	checked, err := polls.Check(in, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	for i, a := range in.GetPoll().GetAnswers() {
		if a.GetMedia_FLAGINPUTMEDIA() == nil {
			continue
		}
		m, err := c.makeMediaByInputMedia(a.GetMedia_FLAGINPUTMEDIA())
		if err != nil {
			return nil, err
		}
		checked.Poll.Answers[i].Media_FLAGMESSAGEMEDIA = m
	}
	attached, err := c.optionalMedia(in.GetAttachedMedia())
	if err != nil {
		return nil, err
	}
	solution, err := c.optionalMedia(in.GetSolutionMedia())
	if err != nil {
		return nil, err
	}
	return polls.NewMedia(checked, attached, solution), nil
}

func (c *MessagesCore) optionalMedia(in *mtproto.InputMedia) (*mtproto.MessageMedia, error) {
	if in == nil {
		return nil, nil
	}
	return c.makeMediaByInputMedia(in)
}

// attachPollReply gives a poll in the reply of a send or a forward the view
// of the account: its creator flag, its results (msg stores copies without
// them).
func (c *MessagesCore) attachPollReply(r *mtproto.Updates) {
	var messages []*mtproto.Message
	for _, u := range r.GetUpdates() {
		if m := u.GetMessage_MESSAGE(); m != nil {
			messages = append(messages, m)
		}
	}
	if err := polls.AttachMessages(c.ctx, c.svcCtx.Reactions, c.MD.UserId, messages...); err != nil {
		c.Logger.Errorf("attach poll to a reply: %v", err)
	}
}

// pollMessage finds the account's copy of msgId, or the channel's message:
// MESSAGE_ID_INVALID when there is none or it has no poll.
func (c *MessagesCore) pollMessage(peer *mtproto.PeerUtil, msgId int32) (*mtproto.Message, error) {
	var m *mtproto.Message
	switch peer.PeerType {
	case mtproto.PEER_CHANNEL:
		msgs, err := c.svcCtx.Dao.Channel.ChannelGetMessages(c.ctx, &channelpb.TLChannelGetMessages{SelfId: c.MD.UserId, ChannelId: peer.PeerId, Id: []int32{msgId}})
		if err != nil {
			return nil, err
		}
		if len(msgs.GetDatas()) > 0 {
			m = msgs.GetDatas()[0]
		}
	case mtproto.PEER_USER, mtproto.PEER_CHAT, mtproto.PEER_SELF:
		peerType, peerId := peer.PeerType, peer.PeerId
		if peerType == mtproto.PEER_SELF {
			peerType, peerId = mtproto.PEER_USER, c.MD.UserId
		}
		did := mtproto.MakeDialogId(c.MD.UserId, peerType, peerId)
		boxes, err := c.svcCtx.Dao.MessageClient.MessageGetUserMessageList(c.ctx, &message.TLMessageGetUserMessageList{UserId: c.MD.UserId, IdList: []int32{msgId}})
		if err != nil {
			return nil, err
		}
		for _, b := range boxes.GetDatas() {
			if b.GetDialogId1() == did.A && b.GetDialogId2() == did.B {
				m = b.GetMessage()
			}
		}
	default:
		return nil, mtproto.ErrPeerIdInvalid
	}
	if m == nil || m.GetMedia().GetPredicateName() != mtproto.Predicate_messageMediaPoll {
		return nil, mtproto.ErrMessageIdInvalid
	}
	return m, nil
}

// pollPeer reads the poll of the message with the account's vote state.
func (c *MessagesCore) pollPeer(peer *mtproto.PeerUtil, msgId int32) (*mtproto.Message, *polls.Record, []polls.Vote, error) {
	m, err := c.pollMessage(peer, msgId)
	if err != nil {
		return nil, nil, nil, err
	}
	id := m.GetMedia().GetPoll().GetId()
	records, votes, err := polls.Load(c.ctx, c.svcCtx.Reactions, []int64{id})
	if err != nil {
		return nil, nil, nil, err
	}
	if records[id] == nil {
		return nil, nil, nil, mtproto.ErrMessageIdInvalid
	}
	return m, records[id], votes[id], nil
}

// pollPeers are the users results name and the chat of peer.
func (c *MessagesCore) pollPeers(peer *mtproto.PeerUtil, ids []int64) ([]*mtproto.User, []*mtproto.Chat, error) {
	ids = append([]int64{c.MD.UserId}, ids...)
	if peer.PeerType == mtproto.PEER_USER {
		ids = append(ids, peer.PeerId)
	}
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: ids, To: []int64{c.MD.UserId}})
	if err != nil {
		return nil, nil, err
	}
	chats := []*mtproto.Chat{}
	switch peer.PeerType {
	case mtproto.PEER_CHAT:
		chat, err := c.svcCtx.Dao.ChatClient.Client().ChatGetMutableChat(c.ctx, &chatpb.TLChatGetMutableChat{ChatId: peer.PeerId})
		if err != nil {
			return nil, nil, err
		}
		chats = append(chats, chat.ToUnsafeChat(c.MD.UserId))
	case mtproto.PEER_CHANNEL:
		mc, err := c.svcCtx.Dao.Channel.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: peer.PeerId, Id: []int64{c.MD.UserId}})
		if err != nil {
			return nil, nil, err
		}
		chats = append(chats, mc.ToChat(c.MD.UserId, int32(time.Now().Unix())))
	}
	return users.GetUserListByIdList(c.MD.UserId, ids...), chats, nil
}

// MessagesSendVote
// messages.sendVote#10ea6184 peer:InputPeer msg_id:int options:Vector<bytes> = Updates;
func (c *MessagesCore) MessagesSendVote(in *mtproto.TLMessagesSendVote) (*mtproto.Updates, error) {
	peer := mtproto.FromInputPeer2(c.MD.UserId, in.Peer)
	r, err := c.svcCtx.Dao.MsgClient.MsgSendVote(c.ctx, &msgpb.TLMsgSendVote{
		UserId:    c.MD.UserId,
		AuthKeyId: c.MD.PermAuthKeyId,
		PeerType:  peer.PeerType,
		PeerId:    peer.PeerId,
		MsgId:     in.MsgId,
		Options:   in.Options,
	})
	if err != nil {
		c.Logger.Errorf("messages.sendVote - error: %v", err)
		return nil, err
	}
	return r, nil
}

// MessagesGetPollResults
// messages.getPollResults#eda3e33b peer:InputPeer msg_id:int poll_hash:long = Updates;
//
// The results as the account sees them; the poll itself only when poll_hash
// is not the one the poll has now (method/messages.getPollResults).
func (c *MessagesCore) MessagesGetPollResults(in *mtproto.TLMessagesGetPollResults) (*mtproto.Updates, error) {
	peer := mtproto.FromInputPeer2(c.MD.UserId, in.Peer)
	_, rec, votes, err := c.pollPeer(peer, in.MsgId)
	if err != nil {
		c.Logger.Errorf("messages.getPollResults - error: %v", err)
		return nil, err
	}
	now := time.Now().Unix()
	poll, results := rec.View(polls.Tally(votes, c.MD.UserId), c.MD.UserId, polls.Full, now)
	update := polls.Update(peer.ToPeer(), in.MsgId, rec.Id, poll, results, rec.Hash(now) != in.PollHash)
	users, chats, err := c.pollPeers(peer, polls.UsersIn(results))
	if err != nil {
		c.Logger.Errorf("messages.getPollResults - error: %v", err)
		return nil, err
	}
	return mtproto.MakeUpdatesByUpdatesUsersChats(users, chats, update), nil
}

// MessagesGetPollVotes
// messages.getPollVotes#b86e380e flags:# peer:InputPeer id:int option:flags.0?bytes offset:flags.1?string limit:int = messages.VotesList;
//
// The voters of a public poll, for polls.Record.MayListVoters
// (POLL_VOTE_REQUIRED otherwise, method/messages.getPollVotes).
// A poll with anonymous voters shows none (BROADCAST_FORBIDDEN).
func (c *MessagesCore) MessagesGetPollVotes(in *mtproto.TLMessagesGetPollVotes) (*mtproto.Messages_VotesList, error) {
	peer := mtproto.FromInputPeer2(c.MD.UserId, in.Peer)
	_, rec, votes, err := c.pollPeer(peer, in.Id)
	if err != nil {
		c.Logger.Errorf("messages.getPollVotes - error: %v", err)
		return nil, err
	}
	if !rec.Poll.GetPublicVoters() {
		return nil, mtproto.ErrBroadcastForbidden
	}
	me := polls.Tally(votes, c.MD.UserId)
	if !rec.MayListVoters(len(me.Mine) > 0, c.MD.UserId, time.Now().Unix()) {
		return nil, mtproto.ErrPollVoteRequired
	}
	voters, count, next, err := polls.VotersPage(c.ctx, c.svcCtx.Reactions, rec.Id, in.GetOption(), in.GetOffset().GetValue(), in.Limit)
	if err != nil {
		c.Logger.Errorf("messages.getPollVotes - error: %v", err)
		return nil, err
	}
	ids := make([]int64, 0, len(voters))
	list := make([]*mtproto.MessagePeerVote, 0, len(voters))
	for _, v := range voters {
		ids = append(ids, v.UserId)
		p := mtproto.MakePeerUser(v.UserId)
		switch {
		case in.GetOption() != nil || len(v.Options) == 1:
			list = append(list, mtproto.MakeTLMessagePeerVote(&mtproto.MessagePeerVote{Peer: p, Option: v.Options[0], Date: int32(v.Date)}).To_MessagePeerVote())
		default:
			list = append(list, mtproto.MakeTLMessagePeerVoteMultiple(&mtproto.MessagePeerVote{Peer: p, Options: v.Options, Date: int32(v.Date)}).To_MessagePeerVote())
		}
	}
	users, chats, err := c.pollPeers(peer, ids)
	if err != nil {
		c.Logger.Errorf("messages.getPollVotes - error: %v", err)
		return nil, err
	}
	out := &mtproto.Messages_VotesList{Count: count, Votes_VECTORMESSAGEPEERVOTE: list, Chats: chats, Users: users}
	if next != "" {
		out.NextOffset = wrapperspb.String(next)
	}
	return mtproto.MakeTLMessagesVotesList(out).To_Messages_VotesList(), nil
}

// MessagesAddPollAnswer
// messages.addPollAnswer#19bc4b6d peer:InputPeer msg_id:int answer:PollAnswer = Updates;
func (c *MessagesCore) MessagesAddPollAnswer(in *mtproto.TLMessagesAddPollAnswer) (*mtproto.Updates, error) {
	peer := mtproto.FromInputPeer2(c.MD.UserId, in.Peer)
	answer := mtproto.MakeTLPollAnswer(&mtproto.PollAnswer{Text_TEXTWITHENTITIES: in.GetAnswer().GetText_TEXTWITHENTITIES()}).To_PollAnswer()
	if media := in.GetAnswer().GetMedia_FLAGINPUTMEDIA(); media != nil {
		m, err := c.makeMediaByInputMedia(media)
		if err != nil {
			return nil, err
		}
		answer.Media_FLAGMESSAGEMEDIA = m
	}
	r, err := c.svcCtx.Dao.MsgClient.MsgAddPollAnswer(c.ctx, &msgpb.TLMsgAddPollAnswer{
		UserId: c.MD.UserId, AuthKeyId: c.MD.PermAuthKeyId, PeerType: peer.PeerType, PeerId: peer.PeerId, MsgId: in.MsgId, Answer: answer,
	})
	if err != nil {
		c.Logger.Errorf("messages.addPollAnswer - error: %v", err)
		return nil, err
	}
	return r, nil
}

// MessagesDeletePollAnswer
// messages.deletePollAnswer#ac8505a5 peer:InputPeer msg_id:int option:bytes = Updates;
func (c *MessagesCore) MessagesDeletePollAnswer(in *mtproto.TLMessagesDeletePollAnswer) (*mtproto.Updates, error) {
	peer := mtproto.FromInputPeer2(c.MD.UserId, in.Peer)
	r, err := c.svcCtx.Dao.MsgClient.MsgDeletePollAnswer(c.ctx, &msgpb.TLMsgDeletePollAnswer{
		UserId: c.MD.UserId, AuthKeyId: c.MD.PermAuthKeyId, PeerType: peer.PeerType, PeerId: peer.PeerId, MsgId: in.MsgId, Option: in.Option,
	})
	if err != nil {
		c.Logger.Errorf("messages.deletePollAnswer - error: %v", err)
		return nil, err
	}
	return r, nil
}

// closePollByEdit is messages.editMessage with a poll: it only closes it
// (api/poll: "to close early, call messages.editMessage and set
// poll.closed"); the definition of a poll is not edited.
func (c *MessagesCore) closePollByEdit(peer *mtproto.PeerUtil, in *mtproto.TLMessagesEditMessage) (*mtproto.Updates, error) {
	if !in.Media.GetPoll().GetClosed() {
		return nil, mtproto.ErrMessageNotModified
	}
	r, err := c.svcCtx.Dao.MsgClient.MsgClosePoll(c.ctx, &msgpb.TLMsgClosePoll{
		UserId: c.MD.UserId, AuthKeyId: c.MD.PermAuthKeyId, PeerType: peer.PeerType, PeerId: peer.PeerId, MsgId: in.Id,
	})
	if err != nil {
		c.Logger.Errorf("messages.editMessage - error: %v", err)
		return nil, err
	}
	return r, nil
}

// MessagesGetUnreadPollVotes
// messages.getUnreadPollVotes#43286cf2 flags:# peer:InputPeer top_msg_id:flags.0?int offset_id:int add_offset:int limit:int max_id:int min_id:int = messages.Messages;
//
// The account's polls of the dialog with unread votes, newest first
// (api/poll).
func (c *MessagesCore) MessagesGetUnreadPollVotes(in *mtproto.TLMessagesGetUnreadPollVotes) (*mtproto.Messages_Messages, error) {
	peer := mtproto.FromInputPeer2(c.MD.UserId, in.Peer)
	if peer.PeerType == mtproto.PEER_SELF {
		peer.PeerType, peer.PeerId = mtproto.PEER_USER, c.MD.UserId
	}
	where := "creator_user_id = ? AND has_unread = 1 AND peer_type = ? AND peer_id = ?"
	args := []any{c.MD.UserId, peer.PeerType, peer.PeerId}
	if top := in.GetTopMsgId().GetValue(); top != 0 && peer.PeerType == mtproto.PEER_CHANNEL {
		// A topic of a forum, a thread of replies elsewhere (task 067).
		where += " AND message_id IN (SELECT m.id FROM channel_messages m JOIN channels c ON c.id = m.channel_id WHERE m.channel_id = ? AND IF(c.forum, m.topic_id = ?, m.reply_top_id = ?))"
		args = append(args, peer.PeerId, channelbox.TopicColumn(top), top)
	}
	var ids []int32
	if err := c.svcCtx.Reactions.QueryRowsPartial(c.ctx, &ids, "SELECT message_id FROM polls WHERE "+where+" ORDER BY message_id DESC", args...); err != nil {
		c.Logger.Errorf("messages.getUnreadPollVotes - error: %v", err)
		return nil, err
	}
	page := pagePollIds(ids, in.OffsetId, in.AddOffset, in.Limit, in.MaxId, in.MinId)
	var messages []*mtproto.Message
	if len(page) > 0 {
		if peer.PeerType == mtproto.PEER_CHANNEL {
			msgs, err := c.svcCtx.Dao.Channel.ChannelGetMessages(c.ctx, &channelpb.TLChannelGetMessages{SelfId: c.MD.UserId, ChannelId: peer.PeerId, Id: page})
			if err != nil {
				return nil, err
			}
			messages = msgs.GetDatas()
		} else {
			boxes, err := c.svcCtx.Dao.MessageClient.MessageGetUserMessageList(c.ctx, &message.TLMessageGetUserMessageList{UserId: c.MD.UserId, IdList: page})
			if err != nil {
				return nil, err
			}
			for _, box := range boxes.GetDatas() {
				messages = append(messages, box.ToMessage(c.MD.UserId))
			}
		}
	}
	h := mtproto.NewIDListHelper()
	h.PickByMessages(messages...)
	users, chats, err := c.pollPeers(peer, h.UserIdList)
	if err != nil {
		return nil, err
	}
	return mtproto.MakeTLMessagesMessagesSlice(&mtproto.Messages_Messages{
		Messages: messages,
		Chats:    chats,
		Users:    users,
		Count:    int32(len(ids)),
	}).To_Messages_Messages(), nil
}

// pagePollIds applies the arguments of a history request to ids, in
// descending order.
func pagePollIds(ids []int32, offsetId, addOffset, limit, maxId, minId int32) []int32 {
	filtered := make([]int32, 0, len(ids))
	for _, id := range ids {
		if (maxId == 0 || id < maxId) && (minId == 0 || id > minId) {
			filtered = append(filtered, id)
		}
	}
	start := 0
	if offsetId != 0 {
		start = slices.IndexFunc(filtered, func(id int32) bool { return id < offsetId })
		if start < 0 {
			start = len(filtered)
		}
	}
	start += int(addOffset)
	start = max(0, min(start, len(filtered)))
	end := min(start+int(max(limit, 0)), len(filtered))
	return filtered[start:end]
}

// MessagesReadPollVotes
// messages.readPollVotes#1720b4d8 flags:# peer:InputPeer top_msg_id:flags.0?int = messages.AffectedHistory;
func (c *MessagesCore) MessagesReadPollVotes(in *mtproto.TLMessagesReadPollVotes) (*mtproto.Messages_AffectedHistory, error) {
	peer := mtproto.FromInputPeer2(c.MD.UserId, in.Peer)
	r, err := c.svcCtx.Dao.MsgClient.MsgReadPollVotes(c.ctx, &msgpb.TLMsgReadPollVotes{
		UserId: c.MD.UserId, AuthKeyId: c.MD.PermAuthKeyId, PeerType: peer.PeerType, PeerId: peer.PeerId, TopMsgId: in.GetTopMsgId().GetValue(),
	})
	if err != nil {
		c.Logger.Errorf("messages.readPollVotes - error: %v", err)
		return nil, err
	}
	return r, nil
}

// MessagesToggleTodoCompleted
// messages.toggleTodoCompleted#d3e03124 peer:InputPeer msg_id:int completed:Vector<int> incompleted:Vector<int> = Updates;
func (c *MessagesCore) MessagesToggleTodoCompleted(in *mtproto.TLMessagesToggleTodoCompleted) (*mtproto.Updates, error) {
	peer := mtproto.FromInputPeer2(c.MD.UserId, in.Peer)
	r, err := c.svcCtx.Dao.MsgClient.MsgToggleTodoCompleted(c.ctx, &msgpb.TLMsgToggleTodoCompleted{
		UserId: c.MD.UserId, AuthKeyId: c.MD.PermAuthKeyId, PeerType: peer.PeerType, PeerId: peer.PeerId, MsgId: in.MsgId,
		Completed: in.Completed, Incompleted: in.Incompleted,
	})
	if err != nil {
		c.Logger.Errorf("messages.toggleTodoCompleted - error: %v", err)
		return nil, err
	}
	return r, nil
}

// MessagesAppendTodoList
// messages.appendTodoList#21a61057 peer:InputPeer msg_id:int list:Vector<TodoItem> = Updates;
func (c *MessagesCore) MessagesAppendTodoList(in *mtproto.TLMessagesAppendTodoList) (*mtproto.Updates, error) {
	peer := mtproto.FromInputPeer2(c.MD.UserId, in.Peer)
	r, err := c.svcCtx.Dao.MsgClient.MsgAppendTodoList(c.ctx, &msgpb.TLMsgAppendTodoList{
		UserId: c.MD.UserId, AuthKeyId: c.MD.PermAuthKeyId, PeerType: peer.PeerType, PeerId: peer.PeerId, MsgId: in.MsgId, List: in.List,
	})
	if err != nil {
		c.Logger.Errorf("messages.appendTodoList - error: %v", err)
		return nil, err
	}
	return r, nil
}
