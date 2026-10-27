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

// Package seqpeers finds the users and chats updates with seq refer to
// (ADR 0020, 0021), for the two places that send them with those peers:
// updates.getDifference of bff, and the inbox consumer of msg, which
// journals and pushes the updates another user's change gives an account.
// A client drops an update about a peer it cannot resolve (iOS) or asks
// for the peer again.
package seqpeers

import (
	"context"
	"errors"

	"github.com/teamgram/proto/mtproto"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// Pick adds to h the users and chats updates with seq refer to; selfId is
// the account they are for (the peers of its filters are its input peers).
// mtproto.IDListHelper.PickByUpdates looks only at messages and channels.
func Pick(h *mtproto.IDListHelper, selfId int64, updates ...*mtproto.Update) {
	users := func(ids ...int64) {
		for _, id := range ids {
			if id != 0 {
				h.AppendUsers(id)
			}
		}
	}
	chat := func(id int64) {
		if id != 0 {
			h.AppendChats(id)
		}
	}
	dialogPeer := func(p *mtproto.DialogPeer) {
		if p.GetPredicateName() == mtproto.Predicate_dialogPeer {
			h.PickByPeer(p.GetPeer())
		}
	}
	inputPeers := func(peers []*mtproto.InputPeer) {
		for _, p := range peers {
			u := mtproto.FromInputPeer2(selfId, p)
			h.PickByPeerUtil(u.PeerType, u.PeerId)
		}
	}
	participant := func(p *mtproto.ChatParticipant) {
		users(p.GetUserId(), p.GetInviterId())
	}
	for _, u := range updates {
		switch u.GetPredicateName() {
		case mtproto.Predicate_updateNotifySettings:
			if p := u.GetPeer_NOTIFYPEER().GetPeer(); p != nil {
				h.PickByPeer(p)
			}
		case mtproto.Predicate_updateDialogPinned,
			mtproto.Predicate_updateSavedDialogPinned:
			dialogPeer(u.GetPeer_DIALOGPEER())
		case mtproto.Predicate_updateDialogUnreadMark:
			dialogPeer(u.GetPeer_DIALOGPEER())
			if p := u.GetSavedPeerId(); p != nil {
				h.PickByPeer(p)
			}
		case mtproto.Predicate_updatePinnedDialogs,
			mtproto.Predicate_updatePinnedSavedDialogs:
			for _, p := range u.GetOrder_FLAGVECTORDIALOGPEER() {
				dialogPeer(p)
			}
		case mtproto.Predicate_updateDialogFilter:
			f := u.GetFilter()
			inputPeers(f.GetPinnedPeers())
			inputPeers(f.GetIncludePeers())
			inputPeers(f.GetExcludePeers())
		case mtproto.Predicate_updateUserName,
			mtproto.Predicate_updateUser:
			// The account's own or, from another user's change, that
			// user's.
			users(u.GetUserId())
		case mtproto.Predicate_updatePrivacy:
			h.PickByRules(u.GetRules())
		case mtproto.Predicate_updatePeerBlocked:
			h.PickByPeer(u.GetPeerId())
		case mtproto.Predicate_updatePeerSettings:
			h.PickByPeer(u.GetPeer_PEER())
		case mtproto.Predicate_updateChatParticipants:
			// The chat, each member and who invited them; of a chat the
			// account left, its own participant only.
			ps := u.GetParticipants_CHATPARTICIPANTS()
			chat(ps.GetChatId())
			if p := ps.GetSelfParticipant(); p != nil {
				participant(p)
			}
			for _, p := range ps.GetParticipants() {
				participant(p)
			}
		case mtproto.Predicate_updateChat:
			chat(u.GetChatId_INT64())
		case mtproto.Predicate_updateChatDefaultBannedRights:
			h.PickByPeer(u.GetPeer_PEER())
		case mtproto.Predicate_updatePendingJoinRequests:
			h.PickByPeer(u.GetPeer_PEER())
			users(u.GetRecentRequesters()...)
		}
	}
}

// Source looks users and chats up: biz user and biz chat, whose clients
// the Daos of bff updates and of msg embed.
type Source interface {
	UserGetMutableUsers(ctx context.Context, in *userpb.TLUserGetMutableUsers) (*userpb.Vector_ImmutableUser, error)
	ChatGetChatListByIdList(ctx context.Context, in *chatpb.TLChatGetChatListByIdList) (*chatpb.Vector_MutableChat, error)
}

// Peers are users and chats as one account, the viewer, sees them.
type Peers struct {
	viewerId int64
	// Viewer is the account itself; nil when biz user did not find it.
	Viewer *mtproto.ImmutableUser
	// Users and Chats are the ones asked for that were found.
	Users []*mtproto.User
	Chats []*mtproto.Chat
}

// Resolve looks up the users and chats of h as viewer sees them, the
// viewer with the users; channels are not looked up. A failed look-up does
// not stop the other one: what was found is returned with the errors.
func Resolve(ctx context.Context, src Source, viewer int64, h *mtproto.IDListHelper) (*Peers, error) {
	p := &Peers{viewerId: viewer}
	var errs []error
	ids := make([]int64, 0, len(h.UserIdList)+1)
	ids = append(ids, viewer)
	for _, id := range h.UserIdList {
		if id != 0 && id != viewer {
			ids = append(ids, id)
		}
	}
	users, err := src.UserGetMutableUsers(ctx, &userpb.TLUserGetMutableUsers{Id: ids})
	if err != nil {
		errs = append(errs, err)
	} else {
		p.Viewer, _ = users.GetImmutableUser(viewer)
		if len(h.UserIdList) > 0 {
			p.Users = users.GetUserListByIdList(viewer, h.UserIdList...)
		}
	}
	if len(h.ChatIdList) > 0 {
		chats, err := src.ChatGetChatListByIdList(ctx, &chatpb.TLChatGetChatListByIdList{SelfId: viewer, IdList: h.ChatIdList})
		if err != nil {
			errs = append(errs, err)
		} else {
			p.Chats = chats.GetChatListByIdList(viewer, h.ChatIdList...)
		}
	}
	return p, errors.Join(errs...)
}

// Of returns the users and chats of p that u refers to (Pick): the peers
// of a container with u alone.
func (p *Peers) Of(u *mtproto.Update) ([]*mtproto.User, []*mtproto.Chat) {
	h := mtproto.NewIDListHelper()
	Pick(h, p.viewerId, u)
	var (
		users []*mtproto.User
		chats []*mtproto.Chat
	)
	for _, id := range h.UserIdList {
		for _, it := range p.Users {
			if it.GetId() == id {
				users = append(users, it)
				break
			}
		}
	}
	for _, id := range h.ChatIdList {
		for _, it := range p.Chats {
			if it.GetId() == id {
				chats = append(chats, it)
				break
			}
		}
	}
	return users, chats
}
