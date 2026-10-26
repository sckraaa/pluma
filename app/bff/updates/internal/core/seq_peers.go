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
	"github.com/teamgram/proto/mtproto"
)

// pickSeqPeers adds to h the users and chats the updates with seq of a
// difference refer to (ADR 0020): a client drops an update about a peer it
// cannot resolve (iOS) or asks for it again. PickByUpdates looks only at
// messages and channels.
func pickSeqPeers(h *mtproto.IDListHelper, selfId int64, updates []*mtproto.Update) {
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
			h.PickByPeerUtil(mtproto.PEER_USER, u.GetUserId())
		case mtproto.Predicate_updatePrivacy:
			h.PickByRules(u.GetRules())
		case mtproto.Predicate_updatePeerBlocked:
			h.PickByPeer(u.GetPeerId())
		case mtproto.Predicate_updatePeerSettings:
			h.PickByPeer(u.GetPeer_PEER())
		}
	}
}
