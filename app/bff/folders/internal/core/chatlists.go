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
	"strings"
	"time"

	"github.com/teamgram/proto/mtproto"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// Shared folders (task 064, core.telegram.org/api/folders). The storage and
// its limits are the dialog service's, one box transaction a change
// (dao/chatlist.go); here are what is sent and read in the clients' form,
// and the joining and leaving, which are one channel at a time through the
// channel service's join and leave.
//
// A join joins the channels first and makes the folder after: when a call
// fails half-way the channels joined stay and a retry goes on from them
// (USER_ALREADY_PARTICIPANT is not a failure), and no folder holds a
// channel its user has not reached.

// newChatlistUpdates is an empty answer of a method that changes channels
// and the folder.
func newChatlistUpdates() *mtproto.Updates {
	return mtproto.MakeTLUpdates(&mtproto.Updates{
		Updates: []*mtproto.Update{},
		Users:   []*mtproto.User{},
		Chats:   []*mtproto.Chat{},
		Date:    int32(time.Now().Unix()),
	}).To_Updates()
}

// mergeChatlistUpdates adds r to out.
func mergeChatlistUpdates(out, r *mtproto.Updates) {
	out.Updates = append(out.Updates, r.GetUpdates()...)
	out.Chats = append(out.Chats, r.GetChats()...)
	out.Users = append(out.Users, r.GetUsers()...)
}

// withUpdateDialogFilter adds updateDialogFilter of folder id to out: iOS
// reads the folder from the answer (ChatFolders.swift:420-446).
func withUpdateDialogFilter(out *mtproto.Updates, id int32, filter *mtproto.DialogFilter) {
	out.Updates = append(out.Updates, mtproto.MakeTLUpdateDialogFilter(&mtproto.Update{Id_INT32: id, Filter: filter}).To_Update())
}

// addChatlistUsers fills the users of out, as a join's answer asks
// (importChannelInvite, task 041).
func (c *FoldersCore) addChatlistUsers(out *mtproto.Updates) error {
	h := mtproto.NewIDListHelper(c.MD.UserId)
	h.AppendUsers(c.MD.UserId)
	h.PickByUpdates(out.GetUpdates()...)
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{
		Id:      h.UserIdList,
		Privacy: true,
		HasTo:   true,
		To:      []int64{c.MD.UserId},
	})
	if err != nil {
		return err
	}
	out.Users = users.GetUserListByIdList(c.MD.UserId, h.UserIdList...)
	return nil
}

// chatlistFolderId is the folder of a request.
func chatlistFolderId(in *mtproto.InputChatlist) (int32, error) {
	if in.GetPredicateName() != mtproto.Predicate_inputChatlistDialogFilter {
		return 0, mtproto.ErrInputChatlistInvalid
	}
	id := in.GetFilterId()
	if id < filterIdMin || id > filterIdMax {
		return 0, mtproto.ErrFilterIdInvalid
	}
	return id, nil
}

// chatlistSlugOf is the slug a link's edit or deletion names. Desktop and Web
// send the whole url of the link there (tdesktop data_chat_filters.cpp:597,
// 621, edit_filter_links.cpp:1059; tweb sharedFolder.tsx:154, 261,
// editFolder.tsx:616), Android and iOS the part after its last slash
// (FilterCreateActivity.java:2077, ChatFolders.swift:62-64). A slug has no
// slash, so both come to the same.
func chatlistSlugOf(s string) string { return s[strings.LastIndexByte(s, '/')+1:] }

// chatlistFolder returns folder id of the user if it is shared.
func (c *FoldersCore) chatlistFolder(id int32) (*mtproto.DialogFilter, []*dialog.DialogFilterExt, error) {
	stored, err := c.svcCtx.Dao.DialogClient.DialogGetDialogFilters(c.ctx, &dialog.TLDialogGetDialogFilters{UserId: c.MD.UserId})
	if err != nil {
		return nil, nil, err
	}
	for _, ext := range stored.GetDatas() {
		if ext.GetId() != id {
			continue
		}
		if ext.GetDialogFilter().GetPredicateName() != mtproto.Predicate_dialogFilterChatlist {
			return nil, nil, mtproto.ErrFilterNotSupported
		}
		return ext.GetDialogFilter(), stored.GetDatas(), nil
	}
	return nil, nil, mtproto.ErrFilterIdInvalid
}

// channelRows reads the channels ids, deleted ones too, each with the
// participant row of the user.
func (c *FoldersCore) channelRows(ids []int64) ([]*channelpb.MutableChannel, error) {
	list, err := c.svcCtx.Dao.ChannelClient.ChannelGetChannelListByIdList(c.ctx, &channelpb.TLChannelGetChannelListByIdList{
		SelfId: c.MD.UserId,
		IdList: ids,
	})
	if err != nil {
		return nil, err
	}
	return list.GetDatas(), nil
}

// channelsOf reads the channels ids that exist, each with the participant
// row of the user.
func (c *FoldersCore) channelsOf(ids []int64) (map[int64]*channelpb.MutableChannel, error) {
	if len(ids) == 0 {
		return map[int64]*channelpb.MutableChannel{}, nil
	}
	list, err := c.channelRows(ids)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]*channelpb.MutableChannel, len(list))
	for _, m := range list {
		if !m.GetChannel().GetDeleted() {
			out[m.GetChannel().GetId()] = m
		}
	}
	return out, nil
}

// chatlistShare is a shared link's channels as a dialog-service request
// takes them.
type chatlistShare struct {
	peers    []*mtproto.InputPeer
	chats    []*mtproto.Chat
	migrated []int64
	channels []int64
}

// resolveShare checks the peers a link is to share and moves the basic
// groups of them to supergroups (core.telegram.org/method/
// chatlists.exportChatlistInvite: "basic groups will be automatically
// converted"). The move is the creator's, as messages.migrateChat is:
// CHAT_ADMIN_REQUIRED to an admin of the group who is not it. check is
// called first, when something is to be moved.
func (c *FoldersCore) resolveShare(in []*mtproto.InputPeer, check func() error) (chatlistShare, error) {
	var s chatlistShare
	if len(in) == 0 {
		return s, mtproto.ErrPeersListEmpty
	}
	if len(in) > filterChatsMax {
		return s, errFilterChatsTooMuch
	}
	var chatsToMove []int64
	peers := make([]*mtproto.InputPeer, 0, len(in))
	for _, p := range in {
		peer := mtproto.FromInputPeer2(c.MD.UserId, p)
		switch {
		case peer.IsChannel():
			peers = append(peers, p)
		case peer.IsChat():
			peers = append(peers, p)
			chatsToMove = append(chatsToMove, peer.PeerId)
		default:
			return s, mtproto.ErrPeerIdInvalid
		}
	}
	if len(chatsToMove) > 0 {
		if err := check(); err != nil {
			return s, err
		}
	}
	for _, id := range chatsToMove {
		r, err := c.svcCtx.Dao.MsgClient.MsgMigrateChat(c.ctx, &msgpb.TLMsgMigrateChat{
			UserId:    c.MD.UserId,
			AuthKeyId: c.MD.PermAuthKeyId,
			ChatId:    id,
		})
		if err != nil {
			return s, err
		}
		channelId := migratedChannel(r)
		if channelId == 0 {
			return s, mtproto.ErrChatIdInvalid
		}
		s.migrated = append(s.migrated, id)
		s.channels = append(s.channels, channelId)
	}
	for i, p := range peers {
		peer := mtproto.FromInputPeer2(c.MD.UserId, p)
		if peer.IsChat() {
			j := slices.Index(s.migrated, peer.PeerId)
			peers[i] = mtproto.MakeTLInputPeerChannel(&mtproto.InputPeer{ChannelId: s.channels[j]}).To_InputPeer()
		}
	}
	ids := make([]int64, 0, len(peers))
	for _, p := range peers {
		if !slices.Contains(ids, p.GetChannelId()) {
			ids = append(ids, p.GetChannelId())
		}
	}
	channels, err := c.channelsOf(ids)
	if err != nil {
		return s, err
	}
	now := int32(time.Now().Unix())
	for i, p := range peers {
		m, ok := channels[p.GetChannelId()]
		if !ok {
			return s, mtproto.ErrChannelInvalid
		}
		if p.GetAccessHash() == 0 {
			peers[i] = mtproto.MakeTLInputPeerChannel(&mtproto.InputPeer{ChannelId: p.GetChannelId(), AccessHash: m.GetChannel().GetAccessHash()}).To_InputPeer()
		}
		s.chats = append(s.chats, m.ToChat(c.MD.UserId, now))
	}
	s.peers = peers
	return s, nil
}

// migratedChannel returns the supergroup of the answer of msg.migrateChat
// (channel in chats, task 047).
func migratedChannel(r *mtproto.Updates) int64 {
	for _, chat := range r.GetChats() {
		if chat.GetPredicateName() == mtproto.Predicate_channel {
			return chat.GetId()
		}
	}
	return 0
}

// channelIdsOf returns the ids of peers, which are channels.
func channelIdsOf(peers []*mtproto.Peer) []int64 {
	out := make([]int64, 0, len(peers))
	for _, p := range peers {
		if p.GetPredicateName() == mtproto.Predicate_peerChannel {
			out = append(out, p.GetChannelId())
		}
	}
	return out
}

// exportedChatlistChats returns the channels of links as the owner sees them.
func (c *FoldersCore) exportedChatlistChats(invites []*mtproto.ExportedChatlistInvite) ([]*mtproto.Chat, error) {
	var ids []int64
	for _, inv := range invites {
		for _, id := range channelIdsOf(inv.GetPeers()) {
			if !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
	}
	channels, err := c.channelsOf(ids)
	if err != nil {
		return nil, err
	}
	now := int32(time.Now().Unix())
	out := make([]*mtproto.Chat, 0, len(channels))
	for _, id := range ids {
		if m, ok := channels[id]; ok {
			out = append(out, m.ToChat(c.MD.UserId, now))
		}
	}
	return out, nil
}

// joinPeer is one channel of a link to join.
type joinPeer struct {
	id   int64
	link string
}

// chosenJoinPeers are the peers chosen by the client that the link has, with
// their links.
func chosenJoinPeers(ext *dialog.ChatlistInviteExt, in []*mtproto.InputPeer) []joinPeer {
	chosen := make([]int64, 0, len(in))
	for _, p := range in {
		if p.GetPredicateName() == mtproto.Predicate_inputPeerChannel {
			chosen = append(chosen, p.GetChannelId())
		}
	}
	var out []joinPeer
	for i, p := range ext.GetPeers() {
		id := p.GetChannelId()
		if slices.Contains(chosen, id) && !slices.ContainsFunc(out, func(j joinPeer) bool { return j.id == id }) {
			out = append(out, joinPeer{id: id, link: ext.GetLinks()[i]})
		}
	}
	return out
}

// joinChatlistChannels joins the channels of peers one at a time, in the
// order of the link. It returns the join answers merged, the channels reached
// (joined, already in, or a join request sent: those wait for the admin and
// are in the folder when approved) and, when the limit of channels stopped
// the joining midway, the error to answer after the folder is written. A
// channel this user cannot reach (banned, the link revoked, a channel gone,
// no room in it) is left out.
func (c *FoldersCore) joinChatlistChannels(peers []joinPeer, channels map[int64]*channelpb.MutableChannel) (*mtproto.Updates, []int64, error) {
	out := newChatlistUpdates()
	joined := make([]int64, 0, len(peers))
	for _, p := range peers {
		m, ok := channels[p.id]
		if !ok {
			continue
		}
		if q, ok := m.GetParticipant(c.MD.UserId); ok && q.Member() {
			joined = append(joined, p.id)
			continue
		}
		r, err := c.svcCtx.Dao.ChannelClient.ChannelJoinChannel(c.ctx, &channelpb.TLChannelJoinChannel{
			SelfId:        c.MD.UserId,
			PermAuthKeyId: c.MD.PermAuthKeyId,
			ChannelId:     p.id,
			Link:          p.link,
		})
		switch {
		case err == nil:
			mergeChatlistUpdates(out, r)
			joined = append(joined, p.id)
		case errors.Is(err, mtproto.ErrUserAlreadyParticipant), errors.Is(err, mtproto.ErrInviteRequestSent):
			joined = append(joined, p.id)
		case errors.Is(err, mtproto.ErrChannelsTooMuch):
			return out, joined, err
		case errors.Is(err, mtproto.ErrChannelPrivate), errors.Is(err, mtproto.ErrChannelInvalid),
			errors.Is(err, mtproto.ErrInviteHashExpired), errors.Is(err, mtproto.ErrInviteHashInvalid), errors.Is(err, mtproto.ErrUsersTooMuch):
			c.Logger.Infof("chatlists join - channel %d of user %d not joined: %v", p.id, c.MD.UserId, err)
		default:
			return nil, nil, err
		}
	}
	return out, joined, nil
}

// joinedChatlistPeers are the channels joined as the folder stores them, and
// their chats as the answer lists them.
func (c *FoldersCore) joinedChatlistPeers(ids []int64, channels map[int64]*channelpb.MutableChannel) ([]*mtproto.InputPeer, []*mtproto.Chat) {
	now := int32(time.Now().Unix())
	peers := make([]*mtproto.InputPeer, 0, len(ids))
	chats := make([]*mtproto.Chat, 0, len(ids))
	for _, id := range ids {
		m := channels[id]
		peers = append(peers, mtproto.MakeTLInputPeerChannel(&mtproto.InputPeer{ChannelId: id, AccessHash: m.GetChannel().GetAccessHash()}).To_InputPeer())
		chats = append(chats, m.ToInvitedChat(c.MD.UserId, now))
	}
	return peers, chats
}

// joinChatlistFrom joins the channels of ext the client chose, checks the
// folder may be made first and writes it after: slug on the first import,
// empty for the new channels of the folder filterId. It answers the joins'
// updates and the folder's updateDialogFilter (Android, tdesktop and iOS
// apply the answer as any Updates; iOS needs that update in it).
func (c *FoldersCore) joinChatlistFrom(ext *dialog.ChatlistInviteExt, slug string, filterId int32, in []*mtproto.InputPeer) (*mtproto.Updates, error) {
	peers := chosenJoinPeers(ext, in)
	if len(peers) == 0 {
		return nil, mtproto.ErrFilterIncludeEmpty
	}
	if _, err := c.svcCtx.Dao.DialogClient.DialogJoinChatlist(c.ctx, &dialog.TLDialogJoinChatlist{
		UserId:    c.MD.UserId,
		Slug:      slug,
		FilterId:  filterId,
		CheckOnly: mtproto.BoolTrue,
	}); err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(peers))
	for _, p := range peers {
		ids = append(ids, p.id)
	}
	channels, err := c.channelsOf(ids)
	if err != nil {
		return nil, err
	}
	out, joined, stop := c.joinChatlistChannels(peers, channels)
	if out == nil {
		return nil, stop
	}
	if len(joined) == 0 {
		if stop != nil {
			return nil, stop
		}
		return nil, mtproto.ErrFilterIncludeEmpty
	}
	inputs, chats := c.joinedChatlistPeers(joined, channels)
	folder, err := c.svcCtx.Dao.DialogClient.DialogJoinChatlist(c.ctx, &dialog.TLDialogJoinChatlist{
		UserId:    c.MD.UserId,
		Slug:      slug,
		FilterId:  filterId,
		Peers:     inputs,
		Chats:     chats,
		CheckOnly: mtproto.BoolFalse,
	})
	if err != nil {
		return nil, err
	}
	if stop != nil {
		return nil, stop
	}
	withUpdateDialogFilter(out, folder.GetId(), folder.GetDialogFilter())
	out.Chats = mergeChats(out.Chats, chats)
	if err = c.addChatlistUsers(out); err != nil {
		return nil, err
	}
	return out, nil
}

// mergeChats adds to chats those of more it does not have.
func mergeChats(chats, more []*mtproto.Chat) []*mtproto.Chat {
	for _, m := range more {
		id := m.GetId()
		if !slices.ContainsFunc(chats, func(c *mtproto.Chat) bool { return c.GetId() == id }) {
			chats = append(chats, m)
		}
	}
	return chats
}

// chatlistInviteOf is the chatlistInvite of a folder not yet imported.
func chatlistInviteOf(folder *mtproto.DialogFilter, peers []*mtproto.Peer, chats []*mtproto.Chat) *mtproto.Chatlists_ChatlistInvite {
	return mtproto.MakeTLChatlistsChatlistInvite(&mtproto.Chatlists_ChatlistInvite{
		TitleNoanimate:         folder.GetTitleNoanimate(),
		Title_TEXTWITHENTITIES: folder.GetTitle_TEXTWITHENTITIES(),
		Title_STRING:           folder.GetTitle_TEXTWITHENTITIES().GetText(),
		Emoticon:               folder.GetEmoticon(),
		Peers:                  peers,
		Chats:                  chats,
		Users:                  []*mtproto.User{},
	}).To_Chatlists_ChatlistInvite()
}

// checkChatlistInviteOf is the answer of chatlists.checkChatlistInvite:
// chatlistInviteAlready to whoever has the folder of the link (missing_peers
// what the link has and its folder lacks; tdesktop api_chat_filters.cpp:917),
// chatlistInvite otherwise, the channels as a guest sees them.
func (c *FoldersCore) checkChatlistInviteOf(ext *dialog.ChatlistInviteExt) (*mtproto.Chatlists_ChatlistInvite, error) {
	ids := channelIdsOf(ext.GetPeers())
	channels, err := c.channelsOf(ids)
	if err != nil {
		return nil, err
	}
	now := int32(time.Now().Unix())
	present := make([]*mtproto.Peer, 0, len(ids))
	chats := make([]*mtproto.Chat, 0, len(ids))
	for _, id := range ids {
		m, ok := channels[id]
		if !ok {
			continue
		}
		present = append(present, mtproto.MakePeer(mtproto.PEER_CHANNEL, id))
		chats = append(chats, m.ToInvitedChat(c.MD.UserId, now))
	}
	if ext.GetMyFilterId() == 0 {
		return chatlistInviteOf(ext.GetDialogFilter(), present, chats), nil
	}
	mine := channelIdsOf(ext.GetMyPeers())
	missing := make([]*mtproto.Peer, 0, len(present))
	already := make([]*mtproto.Peer, 0, len(present))
	for _, p := range present {
		id := p.GetChannelId()
		q, member := channels[id].GetParticipant(c.MD.UserId)
		if member && q.Member() && slices.Contains(mine, id) {
			already = append(already, p)
		} else {
			missing = append(missing, p)
		}
	}
	return mtproto.MakeTLChatlistsChatlistInviteAlready(&mtproto.Chatlists_ChatlistInvite{
		FilterId:     ext.GetMyFilterId(),
		MissingPeers: missing,
		AlreadyPeers: already,
		Chats:        chats,
		Users:        []*mtproto.User{},
	}).To_Chatlists_ChatlistInvite(), nil
}

// chatlistLeaveSuggestions are the channels a client offers to leave with
// folder: those in it, and in no other folder, that the user may leave
// (core.telegram.org/method/chatlists.getLeaveChatlistSuggestions; Android
// marks them in FolderBottomSheet.showForDeletion). Which of them Telegram
// suggests is not documented: staying in a channel kept for another folder,
// or created by the user, is what the user wants unverified.
func (c *FoldersCore) chatlistLeaveSuggestions(id int32, folder *mtproto.DialogFilter, all []*dialog.DialogFilterExt) ([]*mtproto.Peer, error) {
	others := map[int64]bool{}
	for _, ext := range all {
		if ext.GetId() == id {
			continue
		}
		for _, list := range [][]*mtproto.InputPeer{ext.GetDialogFilter().GetPinnedPeers(), ext.GetDialogFilter().GetIncludePeers()} {
			for _, p := range list {
				if p.GetPredicateName() == mtproto.Predicate_inputPeerChannel {
					others[p.GetChannelId()] = true
				}
			}
		}
	}
	var ids []int64
	for _, list := range [][]*mtproto.InputPeer{folder.GetPinnedPeers(), folder.GetIncludePeers()} {
		for _, p := range list {
			if p.GetPredicateName() == mtproto.Predicate_inputPeerChannel && !others[p.GetChannelId()] && !slices.Contains(ids, p.GetChannelId()) {
				ids = append(ids, p.GetChannelId())
			}
		}
	}
	channels, err := c.channelsOf(ids)
	if err != nil {
		return nil, err
	}
	out := make([]*mtproto.Peer, 0, len(ids))
	for _, cid := range ids {
		m, ok := channels[cid]
		if !ok {
			continue
		}
		q, member := m.GetParticipant(c.MD.UserId)
		if member && q.Member() && q.GetRole() != channelpb.RoleCreator {
			out = append(out, mtproto.MakePeer(mtproto.PEER_CHANNEL, cid))
		}
	}
	return out, nil
}

// leaveChatlistChannels leaves the channels of peers that the folder holds.
// What cannot be left any more does not stop the folder from going
// (not in it, its creator, banned, a channel gone).
func (c *FoldersCore) leaveChatlistChannels(folder *mtproto.DialogFilter, in []*mtproto.InputPeer) (*mtproto.Updates, error) {
	inFolder := map[int64]bool{}
	for _, list := range [][]*mtproto.InputPeer{folder.GetPinnedPeers(), folder.GetIncludePeers()} {
		for _, p := range list {
			if p.GetPredicateName() == mtproto.Predicate_inputPeerChannel {
				inFolder[p.GetChannelId()] = true
			}
		}
	}
	out := newChatlistUpdates()
	for _, p := range in {
		if p.GetPredicateName() != mtproto.Predicate_inputPeerChannel || !inFolder[p.GetChannelId()] {
			continue
		}
		r, err := c.svcCtx.Dao.ChannelClient.ChannelLeaveChannel(c.ctx, &channelpb.TLChannelLeaveChannel{
			SelfId:        c.MD.UserId,
			PermAuthKeyId: c.MD.PermAuthKeyId,
			ChannelId:     p.GetChannelId(),
		})
		switch {
		case err == nil:
			mergeChatlistUpdates(out, r)
		case errors.Is(err, mtproto.ErrUserNotParticipant), errors.Is(err, mtproto.ErrUserCreator),
			errors.Is(err, mtproto.ErrChannelPrivate), errors.Is(err, mtproto.ErrChannelInvalid):
			c.Logger.Infof("chatlists.leaveChatlist - channel %d of user %d stays: %v", p.GetChannelId(), c.MD.UserId, err)
		default:
			return nil, err
		}
	}
	return out, nil
}
