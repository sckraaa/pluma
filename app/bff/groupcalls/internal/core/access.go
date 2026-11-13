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

	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
	"github.com/teamgram/teamgram-server/pkg/groupcalls"
)

// access is the chat or channel of a call with the rows of the accounts a
// step needs: who may see the call, who manages it (ADR 0059 §6).
type access struct {
	peerType int32
	peerId   int64
	chat     *mtproto.MutableChat
	channel  *channelpb.MutableChannel
}

// loadPeer loads the chat or channel with the rows of users.
// CHANNEL_INVALID / CHAT_ID_INVALID for one that does not exist.
func (c *GroupCallsCore) loadPeer(peerType int32, peerId int64, users ...int64) (*access, error) {
	a := &access{peerType: peerType, peerId: peerId}
	switch peerType {
	case mtproto.PEER_CHAT:
		chat, err := c.svcCtx.Dao.ChatClient.ChatGetMutableChat(c.ctx, &chatpb.TLChatGetMutableChat{ChatId: peerId})
		if err != nil {
			return nil, err
		}
		a.chat = chat
	case mtproto.PEER_CHANNEL:
		mc, err := c.svcCtx.Dao.ChannelClient.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: peerId, Id: uniq(users)})
		if err != nil {
			return nil, err
		}
		if mc.GetChannel().GetDeleted() {
			return nil, mtproto.ErrChannelPrivate
		}
		a.channel = mc
	default:
		return nil, mtproto.ErrPeerIdInvalid
	}
	return a, nil
}

// loadInputPeer resolves the peer of a request: a chat, or a channel with
// its access_hash.
func (c *GroupCallsCore) loadInputPeer(in *mtproto.InputPeer, users ...int64) (*access, error) {
	p := mtproto.FromInputPeer2(c.MD.UserId, in)
	if !p.IsChatOrChannel() {
		return nil, mtproto.ErrPeerIdInvalid
	}
	a, err := c.loadPeer(p.PeerType, p.PeerId, users...)
	if err != nil {
		return nil, err
	}
	if a.channel != nil && a.channel.GetChannel().GetAccessHash() != p.AccessHash {
		return nil, mtproto.ErrChannelInvalid
	}
	return a, nil
}

// canView: the account is in the chat, or reads the public channel (or
// the discussion group through its channel), as channels.getFullChannel
// lets it.
func (a *access) canView(userId int64) bool {
	if a.chat != nil {
		p, ok := a.chat.GetImmutableChatParticipant(userId)
		return ok && p.IsChatMemberStateNormal() && !a.chat.Deactivated()
	}
	ch := a.channel.GetChannel()
	p, _ := a.channel.GetParticipant(userId)
	if p.Member() {
		return !ch.GetBlocked()
	}
	return (ch.Public() || p.GetState() == channelpb.StateLinked) &&
		a.channel.ToChat(userId, now()).GetPredicateName() == mtproto.Predicate_channel
}

// isMember: the account is in the chat itself.
func (a *access) isMember(userId int64) bool {
	if a.chat != nil {
		p, ok := a.chat.GetImmutableChatParticipant(userId)
		return ok && p.IsChatMemberStateNormal()
	}
	p, _ := a.channel.GetParticipant(userId)
	return p.Member()
}

// canManage: the creator, or an admin with manage_call (an admin of a
// basic group has every right).
func (a *access) canManage(userId int64) bool {
	if a.chat != nil {
		p, ok := a.chat.GetImmutableChatParticipant(userId)
		return ok && p.IsChatMemberStateNormal() && (p.IsChatMemberCreator() || p.IsChatMemberAdmin())
	}
	p, ok := a.channel.GetParticipant(userId)
	return ok && a.channel.GetChannel().Can(p, channelperm.ManageCall, now()) == nil
}

// chatFor is the chat or channel as userId sees it.
func (a *access) chatFor(userId int64) *mtproto.Chat {
	if a.chat != nil {
		return a.chat.ToUnsafeChat(userId)
	}
	return a.channel.ToChat(userId, now())
}

// username is the public username of the chat, "" for a private one.
func (a *access) username() string {
	if a.channel != nil && a.channel.GetChannel().Public() {
		return a.channel.GetChannel().GetUsername()
	}
	return ""
}

// callView is how userId sees call.
func (c *GroupCallsCore) callView(a *access, call *groupcalls.Call, userId int64) groupcalls.CallView {
	v := groupcalls.CallView{VideoLimit: c.svcCtx.VideoLimit}
	if a != nil {
		v.CanManage = a.canManage(userId)
	}
	if call.Scheduled() {
		on, err := c.svcCtx.Dao.Store.Subscribed(c.ctx, call.Id, userId)
		if err != nil {
			// The flag only shows a bell; the rest of the answer stands.
			c.Logger.Errorf("groupcalls: subscription of %d to call %d: %v", userId, call.Id, err)
		}
		v.Subscribed = on
	}
	return v
}

// members are the accounts in the chat or channel.
func (c *GroupCallsCore) members(a *access) ([]int64, error) {
	if a.chat != nil {
		return a.chat.ParticipantIdList(), nil
	}
	ids, err := c.svcCtx.Dao.ChannelClient.ChannelGetMemberIdList(c.ctx, &channelpb.TLChannelGetMemberIdList{ChannelId: a.peerId})
	if err != nil {
		return nil, err
	}
	return ids.GetDatas(), nil
}

// joinAs checks the join_as of a request: only the account itself.
// Joining as a channel is not offered (ADR 0060, owner's decision of
// 2026-11-12): phone.getGroupCallJoinAs lists the account alone, and a
// channel is JOIN_AS_PEER_INVALID, on which Android forgets the chosen
// peer and hangs up (VoIPService.java:2169-2181). tdesktop shows a plain
// confirmation instead of the choice when the list holds only the account
// (calls_choose_join_as.cpp:444-460).
func (c *GroupCallsCore) joinAs(in *mtproto.InputPeer) error {
	p := mtproto.FromInputPeer2(c.MD.UserId, in)
	if p.IsUser() && p.PeerId == c.MD.UserId {
		return nil
	}
	return mtproto.ErrJoinAsPeerInvalid
}

// participantKey is the participant a request names: inputPeerSelf, which
// the clients send for the account itself, is the account's user.
func participantKey(p *mtproto.PeerUtil) groupcalls.PeerKey {
	if p.IsUser() {
		return groupcalls.PeerKey{Type: mtproto.PEER_USER, Id: p.PeerId}
	}
	return groupcalls.PeerKey{Type: p.PeerType, Id: p.PeerId}
}

func uniq(ids []int64) []int64 {
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id != 0 && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}
