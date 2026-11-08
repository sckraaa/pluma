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
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
)

// ChatlistsGetChatlistUpdates
// chatlists.getChatlistUpdates#89419521 chatlist:InputChatlist = chatlists.ChatlistUpdates;
//
// The channels new in the link of an imported folder.
func (c *FoldersCore) ChatlistsGetChatlistUpdates(in *mtproto.TLChatlistsGetChatlistUpdates) (*mtproto.Chatlists_ChatlistUpdates, error) {
	id, err := chatlistFolderId(in.GetChatlist())
	if err != nil {
		c.Logger.Errorf("chatlists.getChatlistUpdates - %v", err)
		return nil, err
	}
	ext, err := c.svcCtx.Dao.DialogClient.DialogGetChatlistInvite(c.ctx, &dialog.TLDialogGetChatlistInvite{UserId: c.MD.UserId, FilterId: id})
	if err != nil {
		c.Logger.Errorf("chatlists.getChatlistUpdates - folder %d: %v", id, err)
		return nil, err
	}
	ids := channelIdsOf(ext.GetPeers())
	channels, err := c.channelsOf(ids)
	if err != nil {
		c.Logger.Errorf("chatlists.getChatlistUpdates - folder %d: %v", id, err)
		return nil, err
	}
	now := int32(time.Now().Unix())
	r := mtproto.MakeTLChatlistsChatlistUpdates(&mtproto.Chatlists_ChatlistUpdates{
		MissingPeers: []*mtproto.Peer{},
		Chats:        []*mtproto.Chat{},
		Users:        []*mtproto.User{},
	}).To_Chatlists_ChatlistUpdates()
	for _, cid := range ids {
		if m, ok := channels[cid]; ok {
			r.MissingPeers = append(r.MissingPeers, mtproto.MakePeer(mtproto.PEER_CHANNEL, cid))
			r.Chats = append(r.Chats, m.ToInvitedChat(c.MD.UserId, now))
		}
	}
	return r, nil
}
