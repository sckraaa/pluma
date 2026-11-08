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
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// archiveFolderDialog is the entry of the archive in the main list, a
// dialogFolder with the unread counts of its chats, muted and not
// (constructor/dialogFolder), and the chat of its latest message; nil when
// the archive is empty (task 063). The muted ones are those with a period
// of mute running, by their own setting or, without one, by the setting of
// their class of chats.
func (c *DialogsCore) archiveFolderDialog() (*dialog.DialogExt, error) {
	archived, err := c.svcCtx.Dao.DialogClient.DialogGetDialogs(c.ctx, &dialog.TLDialogGetDialogs{
		UserId:        c.MD.UserId,
		ExcludePinned: mtproto.BoolFalse,
		FolderId:      1,
	})
	if err != nil {
		return nil, err
	}
	list := archived.GetDatas()
	if len(list) == 0 {
		return nil, nil
	}
	if err = c.fillChannelDialogs(list); err != nil {
		return nil, err
	}
	settings, err := c.svcCtx.Dao.UserClient.UserGetAllNotifySettings(c.ctx, &userpb.TLUserGetAllNotifySettings{UserId: c.MD.UserId})
	if err != nil {
		return nil, err
	}

	var channels []int64
	for _, ex := range list {
		if p := ex.GetDialog().GetPeer(); p.GetPredicateName() == mtproto.Predicate_peerChannel {
			channels = append(channels, p.GetChannelId())
		}
	}
	// A channel dialog's own row does not follow the channel's messages:
	// the channels' last messages date them.
	dates := map[int64]int32{}
	for _, m := range c.channelTopMessages(channels) {
		dates[m.GetPeerId().GetChannelId()] = m.GetDate()
	}
	broadcast := map[int64]bool{}
	for _, ch := range c.channelChats(channels) {
		broadcast[ch.GetId()] = ch.GetBroadcast()
	}

	now := int32(time.Now().Unix())
	folder := mtproto.MakeTLDialogFolder(&mtproto.Dialog{
		Folder: mtproto.MakeTLFolder(&mtproto.Folder{
			Id:    1,
			Title: "Archived Chats",
		}).To_Folder(),
	}).To_Dialog()
	var latest int32 = -1
	for _, ex := range list {
		d := ex.GetDialog()
		p := mtproto.FromPeer(d.GetPeer())
		date := int32(ex.GetDate())
		if p.IsChannel() {
			date = dates[p.PeerId]
		}
		if date > latest {
			latest = date
			folder.Peer = d.GetPeer()
			folder.TopMessage = d.GetTopMessage()
		}
		unread := d.GetUnreadCount()
		if unread == 0 && !d.GetUnreadMark() {
			continue
		}
		if unread == 0 {
			unread = 1
		}
		if mutedDialog(settings.GetDatas(), p, broadcast[p.PeerId], now) {
			folder.UnreadMutedPeersCount++
			folder.UnreadMutedMessagesCount += unread
		} else {
			folder.UnreadUnmutedPeersCount++
			folder.UnreadUnmutedMessagesCount += unread
		}
	}
	return &dialog.DialogExt{Dialog: folder, Order: -1}, nil
}

// mutedDialog tells whether notifications of the dialog with p are muted at
// now: by its own setting, or, without one, by that of its class, as the
// clients and dao.mutedChat read it.
func mutedDialog(settings []*userpb.PeerPeerNotifySettings, p *mtproto.PeerUtil, broadcast bool, now int32) bool {
	if mute := userpb.FindPeerPeerNotifySettings(settings, p).GetMuteUntil(); mute != nil {
		return mute.GetValue() > now
	}
	class := mtproto.PEER_CHATS
	switch {
	case p.PeerType == mtproto.PEER_USER:
		class = mtproto.PEER_USERS
	case p.PeerType == mtproto.PEER_CHANNEL && broadcast:
		class = mtproto.PEER_BROADCASTS
	}
	return userpb.FindPeerPeerNotifySettings(settings, mtproto.MakePeerUtil(int32(class), 0)).GetMuteUntil().GetValue() > now
}
