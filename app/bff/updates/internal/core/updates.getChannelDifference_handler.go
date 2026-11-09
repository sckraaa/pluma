// Copyright 2022 Teamgram Authors
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
	"time"

	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	"github.com/teamgram/teamgram-server/app/service/biz/updates/seqpeers"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// UpdatesGetChannelDifference
// updates.getChannelDifference#3173d78 flags:# force:flags.0?true channel:InputChannel filter:ChannelMessagesFilter pts:int limit:int = updates.ChannelDifference;
//
// What the account missed in a channel (task 040, ADR 0034, p. 4); biz
// channel folds the journal. Only CHANNEL_INVALID (no such channel or a
// wrong access_hash) and CHANNEL_PRIVATE are errors of the method: iOS
// retries any other without end (AccountStateManagementUtils.swift:
// 3448-3458). force changes nothing; a filter of ranges is answered as the
// empty one, as only tdesktop sends them, after a tooLong
// (api_updates.cpp:791-884), and the full answer serves it.
func (c *UpdatesCore) UpdatesGetChannelDifference(in *mtproto.TLUpdatesGetChannelDifference) (*mtproto.Updates_ChannelDifference, error) {
	ch := in.GetChannel()
	if ch.GetPredicateName() != mtproto.Predicate_inputChannel || ch.GetChannelId() == 0 {
		return nil, mtproto.ErrChannelInvalid
	}
	mc, err := c.svcCtx.Dao.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: ch.GetChannelId(), Id: []int64{c.MD.UserId}})
	if err != nil {
		c.Logger.Errorf("updates.getChannelDifference - channel %d: %v", ch.GetChannelId(), err)
		return nil, err
	}
	if mc.GetChannel().GetAccessHash() != ch.GetAccessHash() {
		return nil, mtproto.ErrChannelInvalid
	}
	diff, err := c.svcCtx.Dao.ChannelGetChannelDifference(c.ctx, &channelpb.TLChannelGetChannelDifference{
		SelfId:    c.MD.UserId,
		ChannelId: ch.GetChannelId(),
		Pts:       in.Pts,
		Limit:     in.Limit,
	})
	if err != nil {
		c.Logger.Errorf("updates.getChannelDifference - channel %d: %v", ch.GetChannelId(), err)
		return nil, err
	}
	if diff.GetPredicateName() == mtproto.Predicate_updates_channelDifferenceEmpty {
		return diff, nil
	}
	if diff.GetPredicateName() == mtproto.Predicate_updates_channelDifferenceTooLong {
		if err = c.fillDialog(diff.GetDialog()); err != nil {
			c.Logger.Errorf("updates.getChannelDifference - dialog of channel %d: %v", ch.GetChannelId(), err)
			return nil, err
		}
	}

	if err = c.hideFromBot(ch.GetChannelId(), mc, diff); err != nil {
		c.Logger.Errorf("updates.getChannelDifference - channel %d: %v", ch.GetChannelId(), err)
		return nil, err
	}

	// The channel itself, as the account sees it now, and the peers of the
	// messages and updates.
	h := mtproto.NewIDListHelper(c.MD.UserId)
	h.PickByMessages(diff.GetNewMessages()...)
	h.PickByMessages(diff.GetMessages()...)
	h.PickByUpdates(diff.GetOtherUpdates()...)
	h.AppendChannels(ch.GetChannelId())
	peers, err := seqpeers.Resolve(c.ctx, c.svcCtx.Dao, c.MD.UserId, h)
	if err != nil {
		c.Logger.Errorf("updates.getChannelDifference - users and chats of channel %d: %v", ch.GetChannelId(), err)
	}
	diff.Users = peers.Users
	diff.Chats = peers.Chats
	if !hasChat(diff.Chats, ch.GetChannelId()) {
		diff.Chats = append(diff.Chats, mc.ToChat(c.MD.UserId, int32(time.Now().Unix())))
	}
	return diff, nil
}

// fillDialog adds to the dialog of channelDifferenceTooLong what the
// account's row of dialogs keeps (pin, unread mark, folder, draft) and its
// notification settings: the clients overwrite their dialog with it.
func (c *UpdatesCore) fillDialog(d *mtproto.Dialog) error {
	peer := &mtproto.PeerUtil{PeerType: mtproto.PEER_CHANNEL, PeerId: d.GetPeer().GetChannelId()}
	rows, err := c.svcCtx.Dao.DialogGetDialogsByIdList(c.ctx, &dialog.TLDialogGetDialogsByIdList{
		UserId: c.MD.UserId,
		IdList: []int64{mtproto.MakePeerDialogId(peer.PeerType, peer.PeerId)},
	})
	if err != nil {
		return err
	}
	for _, ex := range rows.GetDatas() {
		if row := ex.GetDialog(); row.GetPeer().GetChannelId() == peer.PeerId {
			d.Pinned = row.GetPinned()
			d.UnreadMark = row.GetUnreadMark()
			d.FolderId = row.GetFolderId()
			d.Draft = row.GetDraft()
			d.TtlPeriod = row.GetTtlPeriod()
		}
	}
	settings, err := c.svcCtx.Dao.UserGetNotifySettingsList(c.ctx, &userpb.TLUserGetNotifySettingsList{
		UserId: c.MD.UserId,
		Peers:  []*mtproto.PeerUtil{peer},
	})
	if err != nil {
		return err
	}
	d.NotifySettings = userpb.FindPeerPeerNotifySettings(settings.GetDatas(), peer)
	return nil
}

func hasChat(chats []*mtproto.Chat, id int64) bool {
	for _, ch := range chats {
		if ch.GetId() == id {
			return true
		}
	}
	return false
}
