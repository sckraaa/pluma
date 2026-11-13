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
	syncpb "github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/groupcalls"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// The updates of a call (ADR 0059 §6). updateGroupCall with the peer goes
// to the members of the chat; updateGroupCallParticipants to those in the
// call. Each viewer gets his own objects: the rows of others as min, his
// own in full (tdesktop data_group_call.cpp:742-752 keeps its own
// settings of a min row). Which of these Telegram sends to whom is not
// verified; a missing or an extra one leaves at most a gap the clients
// close by reading the call (data_group_call.cpp:600-680).

// callUpdate is updateGroupCall of call with its peer; chat_id is the
// form of the older layers.
func callUpdate(call *groupcalls.Call, v groupcalls.CallView) *mtproto.Update {
	return mtproto.MakeTLUpdateGroupCall(&mtproto.Update{
		Peer_PEER:        call.Peer(),
		ChatId_FLAGINT64: wrapperspb.Int64(call.PeerId),
		ChatId_INT64:     call.PeerId,
		Call_GROUPCALL:   call.TL(v),
	}).To_Update()
}

// participantsUpdate is updateGroupCallParticipants of a call at version.
func participantsUpdate(call *groupcalls.Call, version int32, list []*mtproto.GroupCallParticipant) *mtproto.Update {
	return mtproto.MakeTLUpdateGroupCallParticipants(&mtproto.Update{
		Call_INPUTGROUPCALL:                     call.Input(),
		Participants_VECTORGROUPCALLPARTICIPANT: list,
		Version:                                 version,
	}).To_Update()
}

// connectionUpdate is updateGroupCallConnection with the SFU's answer.
func connectionUpdate(params []byte, presentation bool) *mtproto.Update {
	return mtproto.MakeTLUpdateGroupCallConnection(&mtproto.Update{
		Presentation: presentation,
		Params:       mtproto.MakeTLDataJSON(&mtproto.DataJSON{Data: string(params)}).To_DataJSON(),
	}).To_Update()
}

// versions gives each change its version, the call's being that of the
// last: a versioned change is one above the change before it. Each goes in
// an update of its own, as the clients apply one versioned row per version
// (data_group_call.cpp:413-441).
func versions(call *groupcalls.Call, changes []groupcalls.Change) []int32 {
	out := make([]int32, len(changes))
	v := call.Version
	for i := len(changes) - 1; i >= 0; i-- {
		out[i] = v
		if changes[i].Versioned {
			v--
		}
	}
	return out
}

// changeUpdates are the participant updates of changes for viewer.
func changeUpdates(call *groupcalls.Call, changes []groupcalls.Change, viewer int64, prefs map[groupcalls.PeerKey]groupcalls.Pref, min bool) []*mtproto.Update {
	vs := versions(call, changes)
	out := make([]*mtproto.Update, 0, len(changes))
	for i, ch := range changes {
		p := ch.Participant
		out = append(out, participantsUpdate(call, vs[i], []*mtproto.GroupCallParticipant{p.TL(groupcalls.ParticipantView{
			Viewer: viewer, Min: min, Pref: prefs[p.Key()], JustJoined: ch.JustJoined, Versioned: ch.Versioned,
		})}))
	}
	return out
}

// peersOf are the users and the channels the participants are.
func peersOf(list []*groupcalls.Participant) (users, channels []int64) {
	for _, p := range list {
		switch p.PeerType {
		case mtproto.PEER_USER:
			if !slices.Contains(users, p.PeerId) {
				users = append(users, p.PeerId)
			}
		case mtproto.PEER_CHANNEL:
			if !slices.Contains(channels, p.PeerId) {
				channels = append(channels, p.PeerId)
			}
		}
	}
	return users, channels
}

// names are the users and chats of an answer as its viewers see them.
type names struct {
	users    *userpb.Vector_ImmutableUser
	userIds  []int64
	channels []int64
}

// namesOf loads the users ids as the viewers see them, at once. A failed
// read leaves the lists short, which the clients fill themselves.
func (c *GroupCallsCore) namesOf(viewers []int64, users, channels []int64) *names {
	n := &names{userIds: users, channels: channels}
	if len(users) > 0 {
		r, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: append(slices.Clone(viewers), users...), To: viewers})
		if err != nil {
			c.Logger.Errorf("groupcalls: users of a call: %v", err)
		} else {
			n.users = r
		}
	}
	return n
}

// envelope wraps updates for viewer with the users and the chats they
// name; a, when set, adds the chat of the call.
func (c *GroupCallsCore) envelope(viewer int64, updates []*mtproto.Update, n *names, a *access) *mtproto.Updates {
	users := []*mtproto.User{}
	if n != nil && n.users != nil {
		users = n.users.GetUserListByIdList(viewer, n.userIds...)
	}
	chats := []*mtproto.Chat{}
	if a != nil {
		chats = append(chats, a.chatFor(viewer))
	}
	if n != nil && len(n.channels) > 0 {
		var ids []int64
		for _, id := range n.channels {
			if a == nil || a.peerType != mtproto.PEER_CHANNEL || id != a.peerId {
				ids = append(ids, id)
			}
		}
		if len(ids) > 0 {
			l, err := c.svcCtx.Dao.ChannelClient.ChannelGetChannelListByIdList(c.ctx, &channelpb.TLChannelGetChannelListByIdList{SelfId: viewer, IdList: ids})
			if err != nil {
				c.Logger.Errorf("groupcalls: channels of a call: %v", err)
			} else {
				chats = append(chats, l.GetChannelListByIdList(viewer)...)
			}
		}
	}
	return mtproto.MakeTLUpdates(&mtproto.Updates{
		Updates: updates,
		Users:   users,
		Chats:   chats,
		Date:    int32(time.Now().Unix()),
		Seq:     0,
	}).To_Updates()
}

// fanOut says what a delivery carries.
type fanOut struct {
	// changes go to those in the call and to the accounts of the changes.
	changes []groupcalls.Change
	// call: updateGroupCall to the members of the chat too.
	call bool
	// chats: the chat object with the updates, when call_active or
	// call_not_empty changed: the clients take them from it (tdesktop
	// data_session.cpp:1110-1172).
	chats bool
	// left are those who were in the call and are not any more without a
	// change of their own (the end of the call): they get updateGroupCall
	// as those in it do, members of the chat or not.
	left []int64
}

// deliver sends a step to everyone it concerns, except the device that
// made it (its answer carries the step). Each send is logged on failure:
// one lost recipient must not lose the others. It returns the chat as the
// answer of the step shows it: read again when the step changed its call
// flags (f.chats), since a was loaded before the step. tdesktop applies
// call_active and call_not_empty of every chat it gets
// (data_session.cpp:999-1016), so a chat from before the step would undo
// the step for it.
func (c *GroupCallsCore) deliver(a *access, call *groupcalls.Call, f fanOut) *access {
	inCall, err := c.svcCtx.Dao.Store.Users(c.ctx, call.Id)
	if err != nil {
		c.Logger.Errorf("groupcalls: who is in call %d: %v", call.Id, err)
	}
	var changed []*groupcalls.Participant
	for _, ch := range f.changes {
		changed = append(changed, ch.Participant)
		inCall = append(inCall, ch.Participant.UserId)
	}
	inCall = uniq(inCall)
	recipients := uniq(append(slices.Clone(inCall), f.left...))
	if f.call {
		members, err := c.members(a)
		if err != nil {
			c.Logger.Errorf("groupcalls: members of the chat of call %d: %v", call.Id, err)
		}
		recipients = uniq(append(recipients, members...))
	}
	if f.chats || (f.call && a.channel != nil && len(recipients) > 0) {
		// The rows of the recipients, for what each may do and see, and
		// of the account that made the step, for its answer.
		fresh, err := c.loadPeer(a.peerType, a.peerId, uniq(append(slices.Clone(recipients), c.MD.UserId))...)
		if err != nil {
			c.Logger.Errorf("groupcalls: the chat of call %d: %v", call.Id, err)
			return a
		}
		a = fresh
	}
	if len(recipients) == 0 {
		return a
	}
	var subscribers []int64
	if f.call && call.Scheduled() {
		if subscribers, err = c.svcCtx.Dao.Store.Subscribers(c.ctx, call.Id); err != nil {
			c.Logger.Errorf("groupcalls: subscribers of call %d: %v", call.Id, err)
		}
	}
	users, channels := peersOf(changed)
	n := c.namesOf(recipients, users, channels)
	var chatOf *access
	if f.chats {
		chatOf = a
	}
	for _, r := range recipients {
		var ups []*mtproto.Update
		if len(f.changes) > 0 && slices.Contains(inCall, r) {
			ups = append(ups, changeUpdates(call, f.changes, r, nil, true)...)
		}
		if f.call {
			v := groupcalls.CallView{VideoLimit: c.svcCtx.VideoLimit, CanManage: a.canManage(r), Subscribed: slices.Contains(subscribers, r)}
			ups = append(ups, callUpdate(call, v))
		}
		if len(ups) == 0 {
			continue
		}
		c.send(r, c.envelope(r, ups, n, chatOf))
	}
	return a
}

// send pushes updates to every device of userId; to the other devices
// only when userId made the step.
func (c *GroupCallsCore) send(userId int64, updates *mtproto.Updates) {
	var err error
	if userId == c.MD.UserId && c.MD.PermAuthKeyId != 0 {
		_, err = c.svcCtx.Dao.SyncClient.SyncUpdatesNotMe(c.ctx, &syncpb.TLSyncUpdatesNotMe{
			UserId:        userId,
			PermAuthKeyId: c.MD.PermAuthKeyId,
			Updates:       updates,
		})
	} else {
		_, err = c.svcCtx.Dao.SyncClient.SyncPushUpdates(c.ctx, &syncpb.TLSyncPushUpdates{UserId: userId, Updates: updates})
	}
	if err != nil {
		c.Logger.Errorf("groupcalls: push to user %d: %v", userId, err)
	}
}

// answer is the Updates of a step for its own device: the updates of the
// call and of the changes as the account sees them, with the chat.
func (c *GroupCallsCore) answer(a *access, call *groupcalls.Call, changes []groupcalls.Change, head []*mtproto.Update, withCall bool) *mtproto.Updates {
	prefs, err := c.svcCtx.Dao.Store.Prefs(c.ctx, call.Id, c.MD.UserId)
	if err != nil {
		c.Logger.Errorf("groupcalls: settings of %d in call %d: %v", c.MD.UserId, call.Id, err)
	}
	ups := slices.Clone(head)
	if withCall {
		ups = append(ups, callUpdate(call, c.callView(a, call, c.MD.UserId)))
	}
	ups = append(ups, changeUpdates(call, changes, c.MD.UserId, prefs, false)...)
	var list []*groupcalls.Participant
	for _, ch := range changes {
		list = append(list, ch.Participant)
	}
	users, channels := peersOf(list)
	return c.envelope(c.MD.UserId, ups, c.namesOf([]int64{c.MD.UserId}, users, channels), a)
}
