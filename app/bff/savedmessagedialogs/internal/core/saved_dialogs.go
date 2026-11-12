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

	"google.golang.org/grpc/status"

	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/app/service/biz/message/message"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// errParentPeerInvalid: "The specified parent_peer is invalid"
// (core.telegram.org/method/messages.getSavedHistory, 400).
var errParentPeerInvalid = status.Error(mtproto.ErrBadRequest, "PARENT_PEER_INVALID")

// parentMonoforum reads parent_peer: none, or the account itself, is its
// own Saved Messages (0); a channel is the monoforum whose sublists are
// asked for (core.telegram.org/api/monoforum; Android
// messenger/MessagesController.java:21488 sends it only for one), which biz
// channel checks to be one. PARENT_PEER_INVALID for any other peer.
func (c *SavedMessageDialogsCore) parentMonoforum(parent *mtproto.InputPeer) (int64, error) {
	if parent == nil {
		return 0, nil
	}
	p := mtproto.FromInputPeer2(c.MD.UserId, parent)
	switch {
	case p.PeerType == mtproto.PEER_SELF, p.PeerType == mtproto.PEER_USER && p.PeerId == c.MD.UserId:
		return 0, nil
	case p.PeerType == mtproto.PEER_CHANNEL && p.PeerId != 0:
		return p.PeerId, nil
	}
	return 0, errParentPeerInvalid
}

// sublistPeer is the subscriber a sublist of a monoforum is of, as peer
// names it: PEER_ID_INVALID for anything but a user.
func (c *SavedMessageDialogsCore) sublistPeer(in *mtproto.InputPeer) (int64, error) {
	p := mtproto.FromInputPeer2(c.MD.UserId, in)
	switch p.PeerType {
	case mtproto.PEER_SELF:
		return c.MD.UserId, nil
	case mtproto.PEER_USER:
		return p.PeerId, nil
	}
	return 0, mtproto.ErrPeerIdInvalid
}

// fillSublists puts into r the users and the chats its sublists and their
// messages name, the monoforum among the chats; its channel comes with it
// (channel.getChannelListByIdList).
func (c *SavedMessageDialogsCore) fillSublists(r *mtproto.Messages_SavedDialogs, monoforumId int64) error {
	h := mtproto.NewIDListHelper()
	h.AppendChannels(monoforumId)
	for _, d := range r.GetDialogs() {
		h.PickByPeer(d.GetPeer())
	}
	h.PickByMessages(r.GetMessages()...)
	var err error
	r.Users, r.Chats, err = c.peers(h)
	return err
}

// savedPeer is the peer of a sub-chat of Saved Messages as msg keeps it in
// saved_peer of the messages: a user, the account itself for "My notes", a
// chat or a channel (a post or a message of a supergroup saved from there).
func (c *SavedMessageDialogsCore) savedPeer(in *mtproto.InputPeer) (*mtproto.PeerUtil, error) {
	p := mtproto.FromInputPeer2(c.MD.UserId, in)
	switch p.PeerType {
	case mtproto.PEER_SELF:
		return mtproto.MakePeerUtil(mtproto.PEER_USER, c.MD.UserId), nil
	case mtproto.PEER_USER, mtproto.PEER_CHAT, mtproto.PEER_CHANNEL:
		return p, nil
	default:
		return nil, mtproto.ErrPeerIdInvalid
	}
}

// peers returns the users and chats of h as the account sees them, the
// channels among the chats.
func (c *SavedMessageDialogsCore) peers(h *mtproto.IDListHelper) (users []*mtproto.User, chats []*mtproto.Chat, err error) {
	users, chats = []*mtproto.User{}, []*mtproto.Chat{}
	h.Visit(
		func(userIdList []int64) {
			// The account is in the request: the others are seen as it sees them.
			id := userIdList
			if !slices.Contains(id, c.MD.UserId) {
				id = append([]int64{c.MD.UserId}, id...)
			}
			var r *userpb.Vector_ImmutableUser
			if r, err = c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: id}); err == nil {
				users = r.GetUserListByIdList(c.MD.UserId, userIdList...)
			}
		},
		func(chatIdList []int64) {
			if err != nil {
				return
			}
			var r *chatpb.Vector_MutableChat
			if r, err = c.svcCtx.Dao.ChatClient.ChatGetChatListByIdList(c.ctx, &chatpb.TLChatGetChatListByIdList{IdList: chatIdList}); err == nil {
				chats = append(chats, r.GetChatListByIdList(c.MD.UserId, chatIdList...)...)
			}
		},
		func(channelIdList []int64) {
			if err != nil {
				return
			}
			var r *channelpb.Vector_MutableChannel
			if r, err = c.svcCtx.Dao.Channel.ChannelGetChannelListByIdList(c.ctx, &channelpb.TLChannelGetChannelListByIdList{SelfId: c.MD.UserId, IdList: channelIdList}); err == nil {
				chats = append(chats, r.GetChannelListByIdList(c.MD.UserId)...)
			}
		})
	return users, chats, err
}

// fillSavedDialogs puts dialogs into r with their top messages and every
// peer the sub-chats and the messages name: the clients take the peer of a
// sub-chat from users or chats and its date from the top message (Desktop
// data/data_saved_messages.cpp, SavedMessages::applyReceivedSublists;
// Android messenger/SavedMessagesController.java, loadDialogs).
func (c *SavedMessageDialogsCore) fillSavedDialogs(r *mtproto.Messages_SavedDialogs, dialogs []*mtproto.SavedDialog) error {
	r.Dialogs = dialogs
	if len(dialogs) == 0 {
		return nil
	}

	h := mtproto.NewIDListHelper()
	idList := make([]int32, 0, len(dialogs))
	for _, d := range dialogs {
		idList = append(idList, d.GetTopMessage())
		h.PickByPeer(d.GetPeer())
	}
	boxList, err := c.svcCtx.Dao.MessageClient.MessageGetUserMessageList(c.ctx, &message.TLMessageGetUserMessageList{
		UserId: c.MD.UserId,
		IdList: idList,
	})
	if err != nil {
		return err
	}
	boxList.Visit(c.MD.UserId,
		func(messageList []*mtproto.Message) {
			r.Messages = messageList
			h.PickByMessages(messageList...)
		},
		nil, nil, nil)
	if r.Users, r.Chats, err = c.peers(h); err != nil {
		return err
	}
	return nil
}
