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
	"context"
	"errors"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/savedmessagedialogs/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/savedmessagedialogs/internal/svc"
	msg_client "github.com/teamgram/teamgram-server/app/messenger/msg/msg/client"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	channel_client "github.com/teamgram/teamgram-server/app/service/biz/channel/client"
	dialog_client "github.com/teamgram/teamgram-server/app/service/biz/dialog/client"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	message_client "github.com/teamgram/teamgram-server/app/service/biz/message/client"
	"github.com/teamgram/teamgram-server/app/service/biz/message/message"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	me        int64 = 1
	channelId int64 = 70
)

// fakeSaved keeps the sub-chats of Saved Messages of me, each with its top
// message, and records the requests.
type fakeSaved struct {
	dialog_client.DialogClient
	message_client.MessageClient
	user_client.UserClient
	channel_client.ChannelClient
	msg_client.MsgClient

	dialogs       []*mtproto.SavedDialog
	count         int32
	byIdList      []*mtproto.PeerUtil
	historyPeer   *message.TLMessageGetSavedHistoryMessages
	deleteRange   *msgpb.TLMsgDeleteHistoryRange
	channelsAsked []int64

	sublists       *channelpb.TLChannelGetSublists
	sublistHistory *channelpb.TLChannelGetSublistHistory
	sublistRead    *channelpb.TLChannelReadSublist
	sublistDelete  *channelpb.TLChannelDeleteSublistHistory
}

func (f *fakeSaved) DialogGetSavedDialogs(context.Context, *dialog.TLDialogGetSavedDialogs) (*dialog.SavedDialogList, error) {
	return &dialog.SavedDialogList{Dialogs: f.dialogs, Count: f.count}, nil
}

func (f *fakeSaved) DialogGetSavedDialogsByIdList(_ context.Context, in *dialog.TLDialogGetSavedDialogsByIdList) (*dialog.SavedDialogList, error) {
	f.byIdList = in.GetIdList()
	var r []*mtproto.SavedDialog
	for _, p := range in.GetIdList() {
		for _, d := range f.dialogs {
			if mtproto.FromPeer(d.GetPeer()).PeerType == p.PeerType && mtproto.FromPeer(d.GetPeer()).PeerId == p.PeerId {
				r = append(r, d)
			}
		}
	}
	return &dialog.SavedDialogList{Dialogs: r, Count: int32(len(r))}, nil
}

func (f *fakeSaved) MessageGetUserMessageList(_ context.Context, in *message.TLMessageGetUserMessageList) (*message.Vector_MessageBox, error) {
	r := &message.Vector_MessageBox{}
	for _, id := range in.GetIdList() {
		r.Datas = append(r.Datas, savedBox(id))
	}
	return r, nil
}

func (f *fakeSaved) MessageGetSavedHistoryMessages(_ context.Context, in *message.TLMessageGetSavedHistoryMessages) (*mtproto.MessageBoxList, error) {
	f.historyPeer = in
	return &mtproto.MessageBoxList{BoxList: []*mtproto.MessageBox{savedBox(5)}}, nil
}

func (f *fakeSaved) UserGetMutableUsers(context.Context, *userpb.TLUserGetMutableUsers) (*userpb.Vector_ImmutableUser, error) {
	return &userpb.Vector_ImmutableUser{}, nil
}

func (f *fakeSaved) ChannelGetChannelListByIdList(_ context.Context, in *channelpb.TLChannelGetChannelListByIdList) (*channelpb.Vector_MutableChannel, error) {
	f.channelsAsked = append(f.channelsAsked, in.GetIdList()...)
	r := &channelpb.Vector_MutableChannel{}
	for _, id := range in.GetIdList() {
		r.Datas = append(r.Datas, &channelpb.MutableChannel{
			Channel:      &channelpb.ImmutableChannel{Id: id, AccessHash: 3, Title: "news", Broadcast: true},
			Participants: []*channelpb.ImmutableChannelParticipant{{ChannelId: id, UserId: me, State: channelpb.StateIn}},
		})
	}
	return r, nil
}

func (f *fakeSaved) MsgDeleteHistoryRange(_ context.Context, in *msgpb.TLMsgDeleteHistoryRange) (*mtproto.Messages_AffectedHistory, error) {
	f.deleteRange = in
	return mtproto.MakeTLMessagesAffectedHistory(&mtproto.Messages_AffectedHistory{PtsCount: 1}).To_Messages_AffectedHistory(), nil
}

// savedBox is the message id of me's Saved Messages, a post forwarded from
// the channel.
func savedBox(id int32) *mtproto.MessageBox {
	return &mtproto.MessageBox{
		UserId:    me,
		MessageId: id,
		PeerType:  mtproto.PEER_USER,
		PeerId:    me,
		Message: mtproto.MakeTLMessage(&mtproto.Message{
			Id:          id,
			PeerId:      mtproto.MakePeerUser(me),
			FwdFrom:     mtproto.MakeTLMessageFwdHeader(&mtproto.MessageFwdHeader{FromId: mtproto.MakePeerChannel(channelId)}).To_MessageFwdHeader(),
			SavedPeerId: mtproto.MakePeerChannel(channelId),
		}).To_Message(),
	}
}

func newSavedCore(f *fakeSaved) *SavedMessageDialogsCore {
	return &SavedMessageDialogsCore{
		ctx:    context.Background(),
		Logger: logx.WithContext(context.Background()),
		MD:     &metadata.RpcMetadata{UserId: me},
		svcCtx: &svc.ServiceContext{Dao: &dao.Dao{
			DialogClient:  f,
			MessageClient: f,
			UserClient:    f,
			MsgClient:     f,
			Channel:       f,
		}},
	}
}

func inputSelf() *mtproto.InputPeer {
	return mtproto.MakeTLInputPeerSelf(nil).To_InputPeer()
}

func inputChannel(id int64) *mtproto.InputPeer {
	return mtproto.MakeTLInputPeerChannel(&mtproto.InputPeer{ChannelId: id, AccessHash: 3}).To_InputPeer()
}

func hasChannel(chats []*mtproto.Chat, id int64) bool {
	for _, c := range chats {
		if c.GetPredicateName() == mtproto.Predicate_channel && c.GetId() == id {
			return true
		}
	}
	return false
}

// The sub-chats of channels come with their channels in chats, and the
// whole list in one page is messages.savedDialogs.
func TestGetSavedDialogsChannels(t *testing.T) {
	f := &fakeSaved{
		dialogs: []*mtproto.SavedDialog{
			mtproto.MakeTLSavedDialog(&mtproto.SavedDialog{Peer: mtproto.MakePeerChannel(channelId), TopMessage: 5}).To_SavedDialog(),
		},
		count: 1,
	}
	r, err := newSavedCore(f).MessagesGetSavedDialogs(&mtproto.TLMessagesGetSavedDialogs{
		OffsetPeer: mtproto.MakeTLInputPeerEmpty(nil).To_InputPeer(),
		Limit:      20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.GetPredicateName() != mtproto.Predicate_messages_savedDialogs {
		t.Errorf("answer %s, want messages.savedDialogs", r.GetPredicateName())
	}
	if len(r.GetMessages()) != 1 || !hasChannel(r.GetChats(), channelId) {
		t.Errorf("messages %v, chats %v", r.GetMessages(), r.GetChats())
	}

	// A page of more: a slice with the count.
	f.count = 3
	r, err = newSavedCore(f).MessagesGetSavedDialogs(&mtproto.TLMessagesGetSavedDialogs{
		OffsetPeer: mtproto.MakeTLInputPeerEmpty(nil).To_InputPeer(),
		Limit:      1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.GetPredicateName() != mtproto.Predicate_messages_savedDialogsSlice || r.GetCount() != 3 {
		t.Errorf("answer %s count %d, want a slice of 3", r.GetPredicateName(), r.GetCount())
	}
}

// By id: "My notes" is asked as the account itself, in the order given, and
// the channel of a sub-chat comes in chats.
func TestGetSavedDialogsByID(t *testing.T) {
	f := &fakeSaved{dialogs: []*mtproto.SavedDialog{
		mtproto.MakeTLSavedDialog(&mtproto.SavedDialog{Peer: mtproto.MakePeerChannel(channelId), TopMessage: 5}).To_SavedDialog(),
	}}
	r, err := newSavedCore(f).MessagesGetSavedDialogsByID(&mtproto.TLMessagesGetSavedDialogsByID{
		Ids: []*mtproto.InputPeer{inputChannel(channelId), inputSelf()},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.byIdList) != 2 ||
		f.byIdList[0].PeerType != mtproto.PEER_CHANNEL || f.byIdList[0].PeerId != channelId ||
		f.byIdList[1].PeerType != mtproto.PEER_USER || f.byIdList[1].PeerId != me {
		t.Errorf("asked %v", f.byIdList)
	}
	if r.GetPredicateName() != mtproto.Predicate_messages_savedDialogs || len(r.GetDialogs()) != 1 || !hasChannel(r.GetChats(), channelId) {
		t.Errorf("answer %v", r)
	}
}

// parent_peer is the account itself, its own Saved Messages, or a channel,
// the monoforum whose sublists are asked for; any other peer is
// PARENT_PEER_INVALID.
func TestSavedParentPeer(t *testing.T) {
	f := &fakeSaved{}
	c := newSavedCore(f)
	if _, err := c.MessagesGetSavedDialogsByID(&mtproto.TLMessagesGetSavedDialogsByID{
		ParentPeer: inputSelf(),
		Ids:        []*mtproto.InputPeer{inputSelf()},
	}); err != nil {
		t.Errorf("the account's own parent_peer: %v", err)
	}
	if _, err := c.MessagesGetSavedDialogs(&mtproto.TLMessagesGetSavedDialogs{
		ParentPeer: mtproto.MakeTLInputPeerChat(&mtproto.InputPeer{ChatId: 5}).To_InputPeer(),
	}); !errors.Is(err, errParentPeerInvalid) {
		t.Errorf("a basic group's parent_peer: %v, want PARENT_PEER_INVALID", err)
	}
	if _, err := c.MessagesReadSavedHistory(&mtproto.TLMessagesReadSavedHistory{
		ParentPeer: inputSelf(), Peer: inputUser(8), MaxId: 3,
	}); !errors.Is(err, errParentPeerInvalid) {
		t.Errorf("reading a sub-chat of Saved Messages: %v, want PARENT_PEER_INVALID", err)
	}
	if _, err := c.MessagesGetSavedHistory(&mtproto.TLMessagesGetSavedHistory{
		Peer: mtproto.MakeTLInputPeerEmpty(nil).To_InputPeer(),
	}); !errors.Is(err, mtproto.ErrPeerIdInvalid) {
		t.Errorf("the history of no peer: %v", err)
	}
}

func inputUser(id int64) *mtproto.InputPeer {
	return mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: id, AccessHash: 1}).To_InputPeer()
}

func (f *fakeSaved) ChannelGetSublists(_ context.Context, in *channelpb.TLChannelGetSublists) (*mtproto.Messages_SavedDialogs, error) {
	f.sublists = in
	return mtproto.MakeTLMessagesSavedDialogs(&mtproto.Messages_SavedDialogs{
		Dialogs: []*mtproto.SavedDialog{mtproto.MakeTLMonoForumDialog(&mtproto.SavedDialog{
			Peer: mtproto.MakePeerUser(8), TopMessage: 4, UnreadCount: 2,
		}).To_SavedDialog()},
		Messages: []*mtproto.Message{mtproto.MakeTLMessage(&mtproto.Message{
			Id: 4, PeerId: mtproto.MakePeerChannel(in.GetChannelId()), FromId: mtproto.MakePeerUser(8), SavedPeerId: mtproto.MakePeerUser(8),
		}).To_Message()},
	}).To_Messages_SavedDialogs(), nil
}

func (f *fakeSaved) ChannelGetSublistHistory(_ context.Context, in *channelpb.TLChannelGetSublistHistory) (*mtproto.Messages_Messages, error) {
	f.sublistHistory = in
	return mtproto.MakeTLMessagesChannelMessages(&mtproto.Messages_Messages{Pts: 9, Count: 1, Messages: []*mtproto.Message{}}).To_Messages_Messages(), nil
}

func (f *fakeSaved) ChannelReadSublist(_ context.Context, in *channelpb.TLChannelReadSublist) (*mtproto.Bool, error) {
	f.sublistRead = in
	return mtproto.BoolTrue, nil
}

func (f *fakeSaved) ChannelDeleteSublistHistory(_ context.Context, in *channelpb.TLChannelDeleteSublistHistory) (*mtproto.Messages_AffectedHistory, error) {
	f.sublistDelete = in
	return mtproto.MakeTLMessagesAffectedHistory(&mtproto.Messages_AffectedHistory{Pts: 12, PtsCount: 2}).To_Messages_AffectedHistory(), nil
}

// With a channel in parent_peer the methods of the sub-chats work on the
// sublists of that monoforum, through biz channel: the list as
// monoForumDialog with the monoforum in chats, the history, the read mark
// and the deletion of one sublist.
func TestMonoforumSublists(t *testing.T) {
	f := &fakeSaved{}
	c := newSavedCore(f)
	c.MD.PermAuthKeyId = 77

	r, err := c.MessagesGetSavedDialogs(&mtproto.TLMessagesGetSavedDialogs{
		ParentPeer: inputChannel(channelId), ExcludePinned: true, OffsetId: 40, Limit: 20,
		OffsetPeer: mtproto.MakeTLInputPeerEmpty(nil).To_InputPeer(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if in := f.sublists; in.GetSelfId() != me || in.GetChannelId() != channelId || in.GetOffsetId() != 40 || in.GetLimit() != 20 || len(in.GetIds()) != 0 {
		t.Errorf("the list was asked as %v", in)
	}
	if len(r.GetDialogs()) != 1 || r.GetDialogs()[0].GetPredicateName() != mtproto.Predicate_monoForumDialog || !hasChannel(r.GetChats(), channelId) {
		t.Errorf("the list: %v", r)
	}

	if _, err = c.MessagesGetSavedDialogsByID(&mtproto.TLMessagesGetSavedDialogsByID{
		ParentPeer: inputChannel(channelId), Ids: []*mtproto.InputPeer{inputUser(8), inputUser(9)},
	}); err != nil {
		t.Fatal(err)
	}
	if in := f.sublists; len(in.GetIds()) != 2 || in.GetIds()[0] != 8 || in.GetIds()[1] != 9 {
		t.Errorf("the sublists were asked as %v", in)
	}
	if _, err = c.MessagesGetSavedDialogsByID(&mtproto.TLMessagesGetSavedDialogsByID{
		ParentPeer: inputChannel(channelId), Ids: []*mtproto.InputPeer{inputChannel(5)},
	}); !errors.Is(err, mtproto.ErrPeerIdInvalid) {
		t.Errorf("a sublist of a channel: %v, want PEER_ID_INVALID", err)
	}

	h, err := c.MessagesGetSavedHistory(&mtproto.TLMessagesGetSavedHistory{
		ParentPeer: inputChannel(channelId), Peer: inputUser(8), OffsetId: 30, AddOffset: -5, Limit: 10, MaxId: 50, MinId: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if in := f.sublistHistory; in.GetChannelId() != channelId || in.GetPeerId() != 8 || in.GetOffsetId() != 30 || in.GetAddOffset() != -5 || in.GetLimit() != 10 || in.GetMaxId() != 50 || in.GetMinId() != 2 {
		t.Errorf("the history was asked as %v", in)
	}
	if h.GetPredicateName() != mtproto.Predicate_messages_channelMessages || h.GetPts() != 9 || !hasChannel(h.GetChats(), channelId) {
		t.Errorf("the history: %v", h)
	}

	if _, err = c.MessagesReadSavedHistory(&mtproto.TLMessagesReadSavedHistory{ParentPeer: inputChannel(channelId), Peer: inputUser(8), MaxId: 4}); err != nil {
		t.Fatal(err)
	}
	if in := f.sublistRead; in.GetSelfId() != me || in.GetPermAuthKeyId() != 77 || in.GetChannelId() != channelId || in.GetPeerId() != 8 || in.GetMaxId() != 4 {
		t.Errorf("the read was asked as %v", in)
	}

	a, err := c.MessagesDeleteSavedHistory(&mtproto.TLMessagesDeleteSavedHistory{ParentPeer: inputChannel(channelId), Peer: inputUser(8), MaxId: 6})
	if err != nil {
		t.Fatal(err)
	}
	if in := f.sublistDelete; in.GetChannelId() != channelId || in.GetPeerId() != 8 || in.GetMaxId() != 6 || in.GetPermAuthKeyId() != 77 {
		t.Errorf("the deletion was asked as %v", in)
	}
	if a.GetPts() != 12 || a.GetPtsCount() != 2 || f.deleteRange != nil {
		t.Errorf("the deletion: %v, own Saved Messages asked: %v", a, f.deleteRange)
	}
}

// The history of "My notes" is the sub-chat of the account as msg keeps it,
// and the channels its messages name come in chats.
func TestGetSavedHistorySelf(t *testing.T) {
	f := &fakeSaved{}
	r, err := newSavedCore(f).MessagesGetSavedHistory(&mtproto.TLMessagesGetSavedHistory{
		Peer:  inputSelf(),
		Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.historyPeer.GetPeerType() != mtproto.PEER_USER || f.historyPeer.GetPeerId() != me {
		t.Errorf("asked the sub-chat %d:%d", f.historyPeer.GetPeerType(), f.historyPeer.GetPeerId())
	}
	if len(r.GetMessages()) != 1 || !hasChannel(r.GetChats(), channelId) {
		t.Errorf("messages %v, chats %v", r.GetMessages(), r.GetChats())
	}
}

// The sub-chat of posts saved from a channel is deleted as any other.
func TestDeleteSavedHistoryChannel(t *testing.T) {
	f := &fakeSaved{}
	if _, err := newSavedCore(f).MessagesDeleteSavedHistory(&mtproto.TLMessagesDeleteSavedHistory{
		Peer: inputChannel(channelId),
	}); err != nil {
		t.Fatal(err)
	}
	if d := f.deleteRange; d.GetSavedPeerType() != mtproto.PEER_CHANNEL || d.GetSavedPeerId() != channelId ||
		d.GetPeerType() != mtproto.PEER_USER || d.GetPeerId() != me {
		t.Errorf("deleted %v", d)
	}
}
