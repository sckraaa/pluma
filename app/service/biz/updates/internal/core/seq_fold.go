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
	"container/list"
	"strconv"

	"github.com/teamgram/proto/mtproto"
)

// seqFold is the state of the fold of the seq journal rows of a difference
// (ADR 0020). An update with seq is a state of the account, so only the
// last one of each state stays, where it was:
//
//   - one notify peer's settings, one user's name or user, a privacy key,
//     a peer's block or settings bar, a dialog's unread mark, the order of
//     filters, a basic group's members, the group itself, its default
//     rights, a peer's join requests, the saved GIFs (reload them): the
//     last update of each, at its place in the journal (each a full state,
//     ADR 0021);
//   - a filter: its last state at the place of its first update, so that
//     it exists before a later order of filters names it;
//   - updateDialogFilters (reload them all) drops every filter update
//     before it;
//   - a pin of one dialog is not folded: Telegram Desktop moves an already
//     pinned dialog to the top and Android leaves it where it is, so only
//     the full order of the folder (updatePinnedDialogs) after it may drop
//     it. The same holds for Saved Messages.
type seqFold struct {
	keyed   map[string]*list.Element
	pins    map[string][]*list.Element // pins and orders of a folder
	filters map[int32]*list.Element
}

func newSeqFold() seqFold {
	return seqFold{
		keyed:   map[string]*list.Element{},
		pins:    map[string][]*list.Element{},
		filters: map[int32]*list.Element{},
	}
}

// addSeq folds the next update of the seq journal in, after the journal
// updates committed before it.
func (b *differenceBuilder) addSeq(u *mtproto.Update) {
	f := &b.seq
	switch u.PredicateName {
	case mtproto.Predicate_updateDialogPinned:
		k := folderKey(u.GetFolderId().GetValue())
		f.pins[k] = append(f.pins[k], b.updates.PushBack(u))
	case mtproto.Predicate_updateSavedDialogPinned:
		f.pins["saved"] = append(f.pins["saved"], b.updates.PushBack(u))
	case mtproto.Predicate_updatePinnedDialogs:
		b.resetPins(folderKey(u.GetFolderId().GetValue()), u)
	case mtproto.Predicate_updatePinnedSavedDialogs:
		b.resetPins("saved", u)
	case mtproto.Predicate_updateDialogFilter:
		if e, ok := f.filters[u.GetId_INT32()]; ok {
			e.Value = u
			return
		}
		f.filters[u.GetId_INT32()] = b.updates.PushBack(u)
	case mtproto.Predicate_updateDialogFilters:
		for id, e := range f.filters {
			b.updates.Remove(e)
			delete(f.filters, id)
		}
		for _, k := range []string{"filter_order", "filters"} {
			if e, ok := f.keyed[k]; ok {
				b.updates.Remove(e)
			}
		}
		f.keyed["filters"] = b.updates.PushBack(u)
	default:
		k := seqKey(u)
		if k == "" {
			b.updates.PushBack(u)
			return
		}
		if e, ok := f.keyed[k]; ok {
			b.updates.Remove(e)
		}
		f.keyed[k] = b.updates.PushBack(u)
	}
}

// resetPins replaces the pins and orders of a folder by its full order u.
func (b *differenceBuilder) resetPins(folder string, u *mtproto.Update) {
	for _, e := range b.seq.pins[folder] {
		b.updates.Remove(e)
	}
	b.seq.pins[folder] = []*list.Element{b.updates.PushBack(u)}
}

func folderKey(folderId int32) string { return "folder:" + strconv.Itoa(int(folderId)) }

// seqKey is the state an update with seq sets, "" for one not folded.
func seqKey(u *mtproto.Update) string {
	switch u.PredicateName {
	case mtproto.Predicate_updateNotifySettings:
		return "notify:" + notifyPeerKey(u.GetPeer_NOTIFYPEER())
	case mtproto.Predicate_updateDialogUnreadMark:
		return "unread:" + dialogPeerKey(u.GetPeer_DIALOGPEER()) + "/" + peerKey(u.GetSavedPeerId())
	case mtproto.Predicate_updateUserName:
		return "user_name:" + strconv.FormatInt(u.GetUserId(), 10)
	case mtproto.Predicate_updateUser:
		return "user:" + strconv.FormatInt(u.GetUserId(), 10)
	case mtproto.Predicate_updatePrivacy:
		return "privacy:" + u.GetKey().GetPredicateName()
	case mtproto.Predicate_updatePeerBlocked:
		return "blocked:" + peerKey(u.GetPeerId())
	case mtproto.Predicate_updatePeerSettings:
		return "peer_settings:" + peerKey(u.GetPeer_PEER())
	case mtproto.Predicate_updateDialogFilterOrder:
		return "filter_order"
	case mtproto.Predicate_updateChatParticipants:
		return "chat_participants:" + strconv.FormatInt(u.GetParticipants_CHATPARTICIPANTS().GetChatId(), 10)
	case mtproto.Predicate_updateChat:
		return "chat:" + strconv.FormatInt(u.GetChatId_INT64(), 10)
	case mtproto.Predicate_updateChatDefaultBannedRights:
		return "chat_rights:" + peerKey(u.GetPeer_PEER())
	case mtproto.Predicate_updatePendingJoinRequests:
		return "join_requests:" + peerKey(u.GetPeer_PEER())
	case mtproto.Predicate_updateNewAuthorization:
		return "new_authorization:" + strconv.FormatInt(u.GetHash(), 10)
	case mtproto.Predicate_updateSavedGifs:
		return "saved_gifs"
	}
	return ""
}

func peerKey(p *mtproto.Peer) string {
	switch p.GetPredicateName() {
	case mtproto.Predicate_peerUser:
		return "u" + strconv.FormatInt(p.GetUserId(), 10)
	case mtproto.Predicate_peerChat:
		return "c" + strconv.FormatInt(p.GetChatId(), 10)
	case mtproto.Predicate_peerChannel:
		return "h" + strconv.FormatInt(p.GetChannelId(), 10)
	}
	return ""
}

func notifyPeerKey(p *mtproto.NotifyPeer) string {
	switch p.GetPredicateName() {
	case mtproto.Predicate_notifyPeer:
		return peerKey(p.GetPeer())
	case mtproto.Predicate_notifyForumTopic:
		return peerKey(p.GetPeer()) + "#" + strconv.Itoa(int(p.GetTopMsgId()))
	}
	return p.GetPredicateName() // notifyUsers, notifyChats, notifyBroadcasts
}

func dialogPeerKey(p *mtproto.DialogPeer) string {
	if p.GetPredicateName() == mtproto.Predicate_dialogPeerFolder {
		return folderKey(p.GetFolderId())
	}
	return peerKey(p.GetPeer())
}
