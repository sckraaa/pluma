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
	"math/rand/v2"
	"slices"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/polls"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// MsgSendVote
// msg.sendVote user_id:long auth_key_id:long peer_type:int peer_id:long msg_id:int options:Vector<bytes> = Updates;
func (c *MsgCore) MsgSendVote(in *msg.TLMsgSendVote) (*mtproto.Updates, error) {
	// The voter's country, read before the box transaction, which calls no
	// service; only countries_iso2 polls use it.
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: []int64{in.UserId}, To: []int64{in.UserId}})
	if err != nil {
		c.Logger.Errorf("msg.sendVote - error: %v", err)
		return nil, err
	}
	country := ""
	if me, _ := users.GetImmutableUser(in.UserId); me != nil {
		country = polls.Country(me.Phone())
	}
	r, err := c.pollOp(in.UserId, in.AuthKeyId, in.PeerType, in.PeerId, in.MsgId, false, func(b *userbox.Box, ref *dao.PollRef) (*dao.PollChange, error) {
		return c.svcCtx.Dao.CastVote(b, ref, in.Options, country)
	})
	if err != nil {
		c.Logger.Errorf("msg.sendVote - error: %v", err)
		return nil, err
	}
	return r, nil
}

// MsgClosePoll
// msg.closePoll user_id:long auth_key_id:long peer_type:int peer_id:long msg_id:int = Updates;
func (c *MsgCore) MsgClosePoll(in *msg.TLMsgClosePoll) (*mtproto.Updates, error) {
	r, err := c.pollOp(in.UserId, in.AuthKeyId, in.PeerType, in.PeerId, in.MsgId, true, func(b *userbox.Box, ref *dao.PollRef) (*dao.PollChange, error) {
		return c.svcCtx.Dao.ClosePoll(b, ref)
	})
	if err != nil {
		c.Logger.Errorf("msg.closePoll - error: %v", err)
		return nil, err
	}
	return r, nil
}

// MsgAddPollAnswer
// msg.addPollAnswer user_id:long auth_key_id:long peer_type:int peer_id:long msg_id:int answer:PollAnswer = Updates;
func (c *MsgCore) MsgAddPollAnswer(in *msg.TLMsgAddPollAnswer) (*mtproto.Updates, error) {
	r, err := c.pollOp(in.UserId, in.AuthKeyId, in.PeerType, in.PeerId, in.MsgId, true, func(b *userbox.Box, ref *dao.PollRef) (*dao.PollChange, error) {
		return c.svcCtx.Dao.AddPollAnswer(b, ref, in.Answer)
	})
	if err != nil {
		c.Logger.Errorf("msg.addPollAnswer - error: %v", err)
		return nil, err
	}
	return r, nil
}

// MsgDeletePollAnswer
// msg.deletePollAnswer user_id:long auth_key_id:long peer_type:int peer_id:long msg_id:int option:bytes = Updates;
func (c *MsgCore) MsgDeletePollAnswer(in *msg.TLMsgDeletePollAnswer) (*mtproto.Updates, error) {
	r, err := c.pollOp(in.UserId, in.AuthKeyId, in.PeerType, in.PeerId, in.MsgId, true, func(b *userbox.Box, ref *dao.PollRef) (*dao.PollChange, error) {
		return c.svcCtx.Dao.DeletePollAnswer(b, ref, in.Option)
	})
	if err != nil {
		c.Logger.Errorf("msg.deletePollAnswer - error: %v", err)
		return nil, err
	}
	return r, nil
}

// MsgReadPollVotes
// msg.readPollVotes user_id:long auth_key_id:long peer_type:int peer_id:long top_msg_id:int = messages.AffectedHistory;
//
// The unread votes of the user's polls of the dialog are seen: all at once,
// nothing to journal (as msg.readReactions).
func (c *MsgCore) MsgReadPollVotes(in *msg.TLMsgReadPollVotes) (*mtproto.Messages_AffectedHistory, error) {
	peerType, peerId := selfPeer(in.UserId, in.PeerType, in.PeerId)
	res, err := c.svcCtx.Dao.RunBox(c.ctx, in.UserId,
		[]string{dialog.GetDialogCacheKeyByPeer(in.UserId, peerType, peerId)},
		func(b *userbox.Box) error {
			return c.svcCtx.Dao.ReadPollVotes(b, peerType, peerId, in.TopMsgId)
		})
	if err != nil {
		c.Logger.Errorf("msg.readPollVotes - error: %v", err)
		return nil, err
	}
	return mtproto.MakeTLMessagesAffectedHistory(&mtproto.Messages_AffectedHistory{
		Pts: res.State.Pts,
	}).To_Messages_AffectedHistory(), nil
}

type pollOpFn func(b *userbox.Box, ref *dao.PollRef) (*dao.PollChange, error)

// pollOp runs op on the poll of a message in the user's box transaction and
// tells the others. Private chat, basic group: the user's devices with seq,
// the others by inbox events, each with its own view; channel: a push of
// the min results to the members (the channel's pts does not move, as for
// reactions), the voter's other devices its own results.
func (c *MsgCore) pollOp(userId, authKeyId int64, peerType int32, peerId int64, msgId int32, exclusive bool, op pollOpFn) (*mtproto.Updates, error) {
	peerType, peerId = selfPeer(userId, peerType, peerId)
	if peerType == mtproto.PEER_CHANNEL {
		return c.channelPollOp(userId, authKeyId, peerId, msgId, exclusive, op)
	}
	t, err := c.reactionPeers(userId, peerType, peerId, nil)
	if err != nil {
		return nil, err
	}
	serviceId, err := c.svcCtx.Dao.NextId(c.ctx)
	if err != nil {
		return nil, err
	}
	var reply *mtproto.Updates
	_, err = c.svcCtx.Dao.RunBox(c.ctx, userId,
		[]string{dialog.GetDialogCacheKeyByPeer(userId, peerType, peerId)},
		func(b *userbox.Box) error {
			ref, err := c.svcCtx.Dao.PollRefOf(b, peerType, peerId, msgId, exclusive)
			if err != nil {
				return err
			}
			ch, err := op(b, ref)
			if err != nil {
				return err
			}
			poll, results, err := c.svcCtx.Dao.PollViewFor(b, ref, userId, polls.Full)
			if err != nil {
				return err
			}
			update := dao.PollUpdate(ref, poll, results, ch.WithPoll || !ch.Changed)
			updates := []*mtproto.Update{update}
			if !ch.Changed {
				reply = mtproto.MakeUpdatesByUpdatesUsersChats(t.users(userId), t.chats(userId), updates...)
				return nil
			}
			if err = c.svcCtx.Dao.EmitSeqUpdate(b, update, t.users(userId), t.chats(userId)); err != nil {
				return err
			}
			var boxes []*mtproto.MessageBox
			if ch.Service != nil {
				service := dao.ServiceMessageOf(userId, mtproto.MakePeer(peerType, peerId), ref.MessageId, ch.Service, b.Date())
				out := []dao.Outgoing{{RandomId: rand.Int64(), DialogMessageId: serviceId, Message: service}} //nolint:gosec // random_id of a service message
				if boxes, err = c.svcCtx.Dao.SendOutgoing(b, peerType, peerId, out); err != nil {
					return err
				}
				updates = append(updates, serviceReply(boxes)...)
				if err = c.svcCtx.Dao.EmitUpdatesNotMe(b, userId, authKeyId,
					mtproto.MakeUpdatesByUpdatesUsersChats(t.users(userId), t.chats(userId), newMessageUpdates(boxes)...)); err != nil {
					return err
				}
			}
			reply = mtproto.MakeUpdatesByUpdatesUsersChats(t.users(userId), t.chats(userId), updates...)
			withPoll := int32(0)
			if ch.WithPoll {
				withPoll = 1
			}
			recipients := t.recipients
			creator := ref.Rec.Creator
			if creator != userId && ref.Rec.Poll.GetPublicVoters() && !slices.Contains(recipients, creator) {
				// The creator has no copy of this message (the poll was
				// forwarded here): it still counts the unread vote.
				if err = c.svcCtx.Dao.EmitInbox(b, &inbox.TLInboxUpdatePollToInbox{
					UserId:          creator,
					FromId:          userId,
					PeerType:        ref.Rec.PeerType,
					PeerId:          ref.Rec.PeerId,
					DialogMessageId: ref.Rec.DialogMessageId,
					PollId:          ref.Rec.Id,
					WithPoll:        withPoll,
				}); err != nil {
					return err
				}
			}
			for _, to := range recipients {
				if err = c.svcCtx.Dao.EmitInbox(b, &inbox.TLInboxUpdatePollToInbox{
					UserId:          to,
					FromId:          userId,
					PeerType:        peerType,
					PeerId:          peerId,
					DialogMessageId: ref.DialogMessageId,
					PollId:          ref.Rec.Id,
					WithPoll:        withPoll,
				}); err != nil {
					return err
				}
				if len(boxes) > 0 {
					if err = c.svcCtx.Dao.EmitInbox(b, &inbox.TLInboxSendUserMessageToInboxV2{
						UserId:   to,
						FromId:   userId,
						PeerType: peerType,
						PeerId:   peerId,
						BoxList:  boxes,
						Users:    t.users(to),
						Chats:    t.chats(to),
					}); err != nil {
						return err
					}
				}
			}
			return nil
		})
	if err != nil {
		return nil, err
	}
	return reply, nil
}

// serviceReply is updateMessageID and updateNewMessage of service boxes.
func serviceReply(boxes []*mtproto.MessageBox) []*mtproto.Update {
	out := make([]*mtproto.Update, 0, 2*len(boxes))
	for _, box := range boxes {
		out = append(out,
			mtproto.MakeTLUpdateMessageID(&mtproto.Update{Id_INT32: box.MessageId, RandomId: box.RandomId}).To_Update(),
			mtproto.MakeTLUpdateNewMessage(&mtproto.Update{Message_MESSAGE: box.Message, Pts_INT32: box.Pts, PtsCount: box.PtsCount}).To_Update())
	}
	return out
}

func (c *MsgCore) channelPollOp(userId, authKeyId, channelId int64, msgId int32, exclusive bool, op pollOpFn) (*mtproto.Updates, error) {
	var (
		update  *mtproto.Update
		service *dao.ChannelSent
		randId  = rand.Int64() //nolint:gosec // random_id of a service message
	)
	if err := c.ghostJoins(userId, channelId); err != nil {
		return nil, err
	}
	if _, err := c.svcCtx.Dao.RunBox(c.ctx, userId, nil, func(b *userbox.Box) error {
		ref, err := c.svcCtx.Dao.ChannelPollRefOf(b, channelId, msgId, exclusive)
		if err != nil {
			return err
		}
		ch, err := op(b, ref)
		if err != nil {
			return err
		}
		poll, results, err := c.svcCtx.Dao.PollViewFor(b, ref, userId, polls.Full)
		if err != nil {
			return err
		}
		update = dao.PollUpdate(ref, poll, results, ch.WithPoll || !ch.Changed)
		if !ch.Changed {
			return nil
		}
		minPoll, minResults, err := c.svcCtx.Dao.PollViewFor(b, ref, 0, polls.Min)
		if err != nil {
			return err
		}
		push := []*mtproto.Update{polls.Update(nil, 0, ref.Rec.Id, minPoll, minResults, ch.WithPoll)}
		// The voter's other devices learn their own results: a min update
		// says nothing of the voter's choice.
		if err = c.svcCtx.Dao.EmitUpdatesNotMe(b, userId, authKeyId, mtproto.MakeUpdatesByUpdates(update)); err != nil {
			return err
		}
		if ch.Service != nil {
			if service, err = c.svcCtx.Dao.PostChannelService(b, ref.Channel, channelId, ref.Broadcast, msgId, ch.Service, randId); err != nil {
				return err
			}
			push = append(push, mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{Message_MESSAGE: service.Message, Pts_INT32: service.Pts, PtsCount: 1}).To_Update())
		}
		if err = c.svcCtx.Dao.EmitChannelUpdatesPush(b, channelId, authKeyId, push...); err != nil {
			return err
		}
		if !ref.Rec.Poll.GetPublicVoters() || ref.Rec.Creator == userId {
			return nil
		}
		withPoll := int32(0)
		if ch.WithPoll {
			withPoll = 1
		}
		return c.svcCtx.Dao.EmitInbox(b, &inbox.TLInboxUpdatePollToInbox{
			UserId:          ref.Rec.Creator,
			FromId:          userId,
			PeerType:        mtproto.PEER_CHANNEL,
			PeerId:          channelId,
			DialogMessageId: ref.Rec.DialogMessageId,
			PollId:          ref.Rec.Id,
			WithPoll:        withPoll,
		})
	}); err != nil {
		return nil, err
	}
	return c.channelReply(userId, channelId, update, service, randId)
}

// channelReply is the answer of an operation on a channel: the channel, the
// users named and, with a service message, it with its ids.
func (c *MsgCore) channelReply(userId, channelId int64, update *mtproto.Update, service *dao.ChannelSent, randId int64) (*mtproto.Updates, error) {
	mc, err := c.svcCtx.Dao.ChannelClient.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: channelId, Id: []int64{userId}})
	if err != nil {
		return nil, err
	}
	ids := append([]int64{userId}, polls.UsersIn(update.GetResults())...)
	r := mtproto.MakeTLUpdates(&mtproto.Updates{
		Updates: []*mtproto.Update{update},
		Chats:   []*mtproto.Chat{mc.ToChat(userId, int32(time.Now().Unix()))},
		Users:   []*mtproto.User{},
		Date:    int32(time.Now().Unix()),
	}).To_Updates()
	if service != nil {
		shown := channelpb.ViewMessage(service.Message, userId)
		r.Updates = append(r.Updates,
			mtproto.MakeTLUpdateMessageID(&mtproto.Update{Id_INT32: shown.Id, RandomId: randId}).To_Update(),
			mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{Message_MESSAGE: shown, Pts_INT32: service.Pts, PtsCount: 1}).To_Update())
		h := mtproto.NewIDListHelper(userId)
		h.PickByMessage(shown)
		ids = append(ids, h.UserIdList...)
	}
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: ids, To: []int64{userId}})
	if err != nil {
		return nil, err
	}
	r.Users = users.GetUserListByIdList(userId, ids...)
	return r, nil
}

// pollSweepBatch is how many polls one sweep closes.
const pollSweepBatch = 200

// SweepDuePolls closes the polls past their closing date, each in its
// creator's box transaction. A poll that fails is left for the next sweep
// (an hourly-rare failure does not hold the others back); several instances
// may race for one: lockPoll decides (dao.DueClose). The members get
// updateMessagePoll with the poll closed; a client past close_date does not
// wait for it, its next reload shows the poll closed (polls.IsClosed).
func (c *MsgCore) SweepDuePolls() (closed int, err error) {
	due, err := c.svcCtx.Dao.DuePolls(c.ctx, time.Now().Unix(), pollSweepBatch)
	if err != nil {
		return 0, err
	}
	for _, rec := range due {
		if err := c.closeDuePoll(rec); err != nil {
			c.Logger.Errorf("msg: close poll %d: %v", rec.Id, err)
			continue
		}
		closed++
	}
	return closed, nil
}

func (c *MsgCore) closeDuePoll(rec *polls.Record) error {
	var recipients []int64
	switch rec.PeerType {
	case mtproto.PEER_CHAT:
		// The members of now, whether or not the creator is still one of
		// them: its poll closes all the same.
		chat, err := c.svcCtx.Dao.ChatClient.ChatGetMutableChat(c.ctx, &chatpb.TLChatGetMutableChat{ChatId: rec.PeerId})
		if err != nil {
			return err
		}
		recipients = chatRecipients(chat, rec.Creator, false)
	case mtproto.PEER_USER:
		if rec.PeerId != rec.Creator {
			recipients = []int64{rec.PeerId}
		}
	}
	_, err := c.svcCtx.Dao.RunBox(c.ctx, rec.Creator, nil, func(b *userbox.Box) error {
		current, poll, results, err := c.svcCtx.Dao.DueClose(b, rec.Id)
		if err != nil {
			if dao.IsPollClosed(err) {
				return nil
			}
			return err
		}
		if current.PeerType == mtproto.PEER_CHANNEL {
			return c.svcCtx.Dao.EmitChannelUpdatesPush(b, current.PeerId, 0, polls.Update(nil, 0, current.Id, poll, results, true))
		}
		update := polls.Update(mtproto.MakePeer(current.PeerType, current.PeerId), current.MessageId, current.Id, poll, results, true)
		if err = c.svcCtx.Dao.EmitSeqUpdate(b, update, nil, nil); err != nil {
			return err
		}
		for _, to := range recipients {
			if err = c.svcCtx.Dao.EmitInbox(b, &inbox.TLInboxUpdatePollToInbox{
				UserId:          to,
				FromId:          current.Creator,
				PeerType:        current.PeerType,
				PeerId:          current.PeerId,
				DialogMessageId: current.DialogMessageId,
				PollId:          current.Id,
				WithPoll:        1,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	return err
}
