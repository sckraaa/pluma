// Copyright 2024 Teamgram Authors
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
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package core

import (
	"math"

	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
)

// MessagesGetSavedDialogs
// messages.getSavedDialogs#1e91fc99 flags:# exclude_pinned:flags.0?true parent_peer:flags.1?InputPeer offset_date:int offset_id:int offset_peer:InputPeer limit:int hash:long = messages.SavedDialogs;
//
// The sub-chats of Saved Messages, newest top message first, a page below
// offset_id; pinned ones go with them unless exclude_pinned (Desktop
// excludes them and asks messages.getPinnedSavedDialogs, Android takes them
// here and puts them first itself: messenger/SavedMessagesController.java,
// updateAllDialogs). The answer is messages.savedDialogs when it holds all
// of them and messages.savedDialogsSlice with count otherwise.
func (c *SavedMessageDialogsCore) MessagesGetSavedDialogs(in *mtproto.TLMessagesGetSavedDialogs) (*mtproto.Messages_SavedDialogs, error) {
	monoforumId, err := c.parentMonoforum(in.GetParentPeer())
	if err != nil {
		c.Logger.Errorf("messages.getSavedDialogs - error: %v", err)
		return nil, err
	}
	if monoforumId != 0 {
		return c.getSublists(monoforumId, nil, in.GetOffsetId(), in.GetLimit())
	}

	limit := in.GetLimit()
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	offsetPeer := mtproto.FromInputPeer2(c.MD.UserId, in.GetOffsetPeer())
	dialogs, err := c.svcCtx.Dao.DialogClient.DialogGetSavedDialogs(c.ctx, &dialog.TLDialogGetSavedDialogs{
		UserId:        c.MD.UserId,
		ExcludePinned: mtproto.ToBool(in.GetExcludePinned()),
		OffsetDate:    in.GetOffsetDate(),
		OffsetId:      in.GetOffsetId(),
		OffsetPeer:    offsetPeer,
		Limit:         limit,
	})
	if err != nil {
		c.Logger.Errorf("messages.getSavedDialogs - error: %v", err)
		return nil, err
	}

	r := &mtproto.Messages_SavedDialogs{
		Dialogs:  []*mtproto.SavedDialog{},
		Messages: []*mtproto.Message{},
		Chats:    []*mtproto.Chat{},
		Users:    []*mtproto.User{},
		Count:    dialogs.GetCount(),
	}
	if err = c.fillSavedDialogs(r, dialogs.GetDialogs()); err != nil {
		c.Logger.Errorf("messages.getSavedDialogs - error: %v", err)
		return nil, err
	}
	if isFirstPage(in) && int32(len(r.Dialogs)) >= r.Count {
		return mtproto.MakeTLMessagesSavedDialogs(r).To_Messages_SavedDialogs(), nil
	}
	return mtproto.MakeTLMessagesSavedDialogsSlice(r).To_Messages_SavedDialogs(), nil
}

// getSublists is messages.getSavedDialogs and getSavedDialogsByID of a
// monoforum (task 084): its sublists as monoForumDialog, the one written to
// last first, a page below the sublist whose last message is offsetId, or
// those of peers alone. The clients page by the last message of the page
// (tdesktop data_saved_messages.cpp:298-320, Android
// TopicsController.java), whose id orders the list. Only the admins of the
// channel's direct messages list them (CHAT_ADMIN_REQUIRED).
func (c *SavedMessageDialogsCore) getSublists(monoforumId int64, peers []int64, offsetId, limit int32) (*mtproto.Messages_SavedDialogs, error) {
	if offsetId == math.MaxInt32 {
		offsetId = 0
	}
	r, err := c.svcCtx.Dao.Channel.ChannelGetSublists(c.ctx, &channelpb.TLChannelGetSublists{
		SelfId:    c.MD.UserId,
		ChannelId: monoforumId,
		Ids:       peers,
		OffsetId:  offsetId,
		Limit:     limit,
	})
	if err != nil {
		c.Logger.Errorf("messages.getSavedDialogs - sublists of %d: %v", monoforumId, err)
		return nil, err
	}
	if err = c.fillSublists(r, monoforumId); err != nil {
		c.Logger.Errorf("messages.getSavedDialogs - sublists of %d: %v", monoforumId, err)
		return nil, err
	}
	return r, nil
}

// isFirstPage tells a request of the newest sub-chats: no offset, or the
// offset_id of Android's first page (Integer.MAX_VALUE).
func isFirstPage(in *mtproto.TLMessagesGetSavedDialogs) bool {
	return in.GetOffsetDate() == 0 && (in.GetOffsetId() <= 0 || in.GetOffsetId() == math.MaxInt32)
}
