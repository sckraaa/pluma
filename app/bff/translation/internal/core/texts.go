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
	"slices"

	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	messagepb "github.com/teamgram/teamgram-server/app/service/biz/message/message"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/translate"
)

// messageTexts returns the texts of the messages ids of the dialog in, one
// item for each id in its order: the clients match the answer to their
// request by position (Android TranslateController.java:1162-1167, iOS
// TelegramCore/.../Messages/Translate.swift:406-412, tdesktop
// lang/translate_mtproto_provider.cpp:67-82). The messages are read as
// every other handler reads them: the caller's copies in a private chat,
// Saved Messages or a group, the messages of a channel the caller can
// read (as transcription does, app/bff/transcription/internal/core/record.go).
//
// A message that is not there, is not of this dialog, or has no text (a
// photo without a caption, a poll, a service message) is an empty item:
// iOS puts polls among the ids, and its answer must stay aligned
// (Translate.swift:296-322). The errors are those of
// core.telegram.org/method/messages.translateText: MSG_ID_INVALID for no
// ids, PEER_ID_INVALID for a dialog the caller does not have or a channel
// it cannot read; errUnreachable stands for the second.
func (c *TranslationCore) messageTexts(in *mtproto.InputPeer, ids []int32) ([]*mtproto.TextWithEntities, error) {
	if len(ids) == 0 {
		return nil, mtproto.ErrMsgIdInvalid
	}
	// The ids are bounded before anything is read; the texts are bounded
	// by the scheduler (translate.CheckItems).
	if len(ids) > c.svcCtx.Config.Translate.MaxItems {
		return nil, translate.ErrInputTextTooLong
	}
	peer, err := c.chatPeer(in)
	if err != nil {
		return nil, err
	}
	uniq := slices.Clone(ids)
	slices.Sort(uniq)
	uniq = slices.Compact(uniq)

	byId := make(map[int32]*mtproto.Message, len(uniq))
	switch peer.Type {
	case mtproto.PEER_USER, mtproto.PEER_CHAT:
		if err = c.checkDialog(peer); err != nil {
			return nil, err
		}
		boxes, err := c.svcCtx.Messages.MessageGetUserMessageList(c.ctx, &messagepb.TLMessageGetUserMessageList{UserId: c.MD.UserId, IdList: uniq})
		if err != nil {
			return nil, err
		}
		for _, box := range boxes.GetDatas() {
			// An id of another dialog of the caller is not a message of
			// this one: its text is never given in place of the asked one.
			if box.GetUserId() == c.MD.UserId && box.GetPeerType() == peer.Type && dialogOf(box) == peer.Id {
				byId[box.GetMessageId()] = box.GetMessage()
			}
		}
	case mtproto.PEER_CHANNEL:
		if _, err = c.readableChannel(in); err != nil {
			return nil, err
		}
		messages, err := c.svcCtx.Channels.ChannelGetMessages(c.ctx, &channelpb.TLChannelGetMessages{SelfId: c.MD.UserId, ChannelId: peer.Id, Id: uniq})
		if isError(err, mtproto.ErrChannelPrivate) {
			return nil, errUnreachable
		}
		if err != nil {
			return nil, err
		}
		for _, m := range messages.GetDatas() {
			byId[m.GetId()] = m
		}
	default:
		return nil, errUnreachable
	}

	out := make([]*mtproto.TextWithEntities, len(ids))
	for i, id := range ids {
		out[i] = textOf(byId[id])
	}
	return out, nil
}

// dialogOf is the id of the user, chat or channel of the dialog box is in,
// for its owner. msg keeps the peer of the send in every copy of a private
// message (app/messenger/msg/internal/dao/send.go): the receiver's copy
// names the receiver, and its dialog is the sender's.
func dialogOf(box *mtproto.MessageBox) int64 {
	if box.GetPeerType() == mtproto.PEER_USER && box.GetPeerId() == box.GetUserId() {
		return box.GetSenderUserId()
	}
	return box.GetPeerId()
}

// textOf is the text of m with its entities as stored, or an empty item.
func textOf(m *mtproto.Message) *mtproto.TextWithEntities {
	if m.GetPredicateName() != mtproto.Predicate_message {
		return emptyText()
	}
	return mtproto.MakeTLTextWithEntities(&mtproto.TextWithEntities{Text: m.GetMessage(), Entities: m.GetEntities()}).To_TextWithEntities()
}

func emptyText() *mtproto.TextWithEntities {
	return mtproto.MakeTLTextWithEntities(&mtproto.TextWithEntities{Text: "", Entities: []*mtproto.MessageEntity{}}).To_TextWithEntities()
}

// clientTexts returns the texts a client sent with their entities as the
// translation keeps them. An inputMessageEntityMentionName becomes the
// messageEntityMentionName of its user, as a sent message carries it, and
// is dropped when there is no such user (translate.Normalize); the
// translation then gives the mention back by id, as the clients show it.
// The texts are bounded first, so that nothing is resolved for a request
// that is refused anyway.
func (c *TranslationCore) clientTexts(items []*mtproto.TextWithEntities) ([]*mtproto.TextWithEntities, error) {
	if err := translate.CheckItems(c.svcCtx.Config.Translate, items); err != nil {
		return nil, err
	}
	out := make([]*mtproto.TextWithEntities, len(items))
	for i, it := range items {
		ents, err := translate.Normalize(c.ctx, it.GetText(), it.GetEntities(), c.MD.UserId, c.usersExist)
		if err != nil {
			return nil, err
		}
		out[i] = mtproto.MakeTLTextWithEntities(&mtproto.TextWithEntities{Text: it.GetText(), Entities: ents}).To_TextWithEntities()
	}
	return out, nil
}

// usersExist is the translate.UserExistence of the users service.
func (c *TranslationCore) usersExist(ctx context.Context, ids []int64) (map[int64]bool, error) {
	users, err := c.svcCtx.Users.UserGetUserDataListByIdList(ctx, &userpb.TLUserGetUserDataListByIdList{UserIdList: ids})
	if err != nil {
		return nil, err
	}
	found := make(map[int64]bool, len(ids))
	for _, u := range users.GetDatas() {
		found[u.GetId()] = true
	}
	return found, nil
}

// clientError tells whether err is an answer for the client (an RPC error
// of the method) rather than a failure of a service read on the way.
func clientError(err error) bool {
	for _, want := range []error{
		mtproto.ErrMsgIdInvalid,
		mtproto.ErrInputTextEmpty,
		translate.ErrInputTextTooLong,
	} {
		if isError(err, want) {
			return true
		}
	}
	return errors.Is(err, errUnreachable)
}
