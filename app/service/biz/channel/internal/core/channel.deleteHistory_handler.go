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
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/internal/dao"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// ChannelDeleteHistory
// channel.deleteHistory self_id:long perm_auth_key_id:long channel_id:long for_everyone:Bool max_id:int = Updates;
//
// self_id deletes the history of the channel up to max_id, for everyone
// (dao.DeleteHistory, portion after portion) or for itself (dao.HideHistory).
// The answer is the Updates Android, iOS and Web K apply
// (MessagesController.java:10255, ManagedCloudChatRemoveMessagesOperations.
// swift:591, appMessagesManager.ts flushHistory); tdesktop does not look at
// it (data_histories.cpp:838) and meets the same pts by the next difference.
func (c *ChannelCore) ChannelDeleteHistory(in *channel.TLChannelDeleteHistory) (*mtproto.Updates, error) {
	if !mtproto.FromBool(in.ForEveryone) {
		var hidden int32
		if _, err := c.svcCtx.Dao.RunBox(c.ctx, in.SelfId, func(b *userbox.Box) error {
			var err error
			hidden, err = c.svcCtx.Dao.HideHistory(b, in.ChannelId, in.MaxId)
			return err
		}); err != nil {
			return nil, c.membershipError("channel.deleteHistory", err)
		}
		return hideHistoryUpdates(in.ChannelId, hidden), nil
	}

	var (
		maxId    = in.MaxId
		portions []*dao.Deleted
	)
	for {
		var del *dao.HistoryDeleted
		if _, err := c.svcCtx.Dao.RunBox(c.ctx, in.SelfId, func(b *userbox.Box) error {
			var err error
			if del, err = c.svcCtx.Dao.DeleteHistory(b, in.ChannelId, maxId); err != nil {
				return err
			}
			return c.svcCtx.Dao.EmitDeletePush(b, in.PermAuthKeyId, del.Deleted)
		}); err != nil {
			return nil, c.membershipError("channel.deleteHistory", err)
		}
		maxId = del.MaxId
		if len(del.Ids) > 0 {
			portions = append(portions, del.Deleted)
		}
		if !del.More {
			return deleteHistoryUpdates(in.ChannelId, portions), nil
		}
	}
}

// hideHistoryUpdates is the answer of a history hidden up to hidden: no
// update when nothing is.
func hideHistoryUpdates(channelId int64, hidden int32) *mtproto.Updates {
	if hidden == 0 {
		return mtproto.MakeUpdatesByUpdates()
	}
	return mtproto.MakeUpdatesByUpdates(mtproto.MakeTLUpdateChannelAvailableMessages(&mtproto.Update{
		ChannelId:      channelId,
		AvailableMinId: hidden,
	}).To_Update())
}

// deleteHistoryUpdates is the answer of a history deleted for everyone: one
// portion is named by its update, more of them are not sent as a list of
// up to HistoryPortion ids each but as updateChannelTooLong with the pts
// reached, for which the clients fetch the difference (Web K flushHistory:
// "a history too big to enumerate comes back as updateChannelTooLong").
func deleteHistoryUpdates(channelId int64, portions []*dao.Deleted) *mtproto.Updates {
	switch len(portions) {
	case 0:
		return mtproto.MakeUpdatesByUpdates()
	case 1:
		del := portions[0]
		return mtproto.MakeUpdatesByUpdates(mtproto.MakeTLUpdateDeleteChannelMessages(&mtproto.Update{
			ChannelId: channelId,
			Messages:  del.Ids,
			Pts_INT32: del.Pts,
			PtsCount:  del.PtsCount,
		}).To_Update())
	}
	last := portions[len(portions)-1]
	return mtproto.MakeUpdatesByUpdates(mtproto.MakeTLUpdateChannelTooLong(&mtproto.Update{
		ChannelId:     channelId,
		Pts_FLAGINT32: mtproto.MakeFlagsInt32(last.Pts),
	}).To_Update())
}

// ChannelDeleteParticipantHistory
// channel.deleteParticipantHistory self_id:long perm_auth_key_id:long channel_id:long participant_id:long = messages.AffectedHistory;
//
// self_id deletes the newest messages participant_id sent to the supergroup,
// a portion at a time (dao.DeleteParticipantHistory). offset is the number
// left: above 0 the client repeats the call, and it falls as the history
// goes (core.telegram.org/constructor/messages.affectedHistory; Android
// MessagesController.java:9633, tdesktop apiwrap.cpp:1509, iOS
// DeleteMessagesInteractively.swift:269). The pts is the channel's, as
// channels.deleteMessages answers, with count 0 when nothing was deleted.
func (c *ChannelCore) ChannelDeleteParticipantHistory(in *channel.TLChannelDeleteParticipantHistory) (*mtproto.Messages_AffectedHistory, error) {
	var del *dao.ParticipantHistoryDeleted
	if _, err := c.svcCtx.Dao.RunBox(c.ctx, in.SelfId, func(b *userbox.Box) error {
		var err error
		if del, err = c.svcCtx.Dao.DeleteParticipantHistory(b, in.ChannelId, in.ParticipantId); err != nil {
			return err
		}
		return c.svcCtx.Dao.EmitDeletePush(b, in.PermAuthKeyId, del.Deleted)
	}); err != nil {
		return nil, c.membershipError("channel.deleteParticipantHistory", err)
	}
	pts := del.Pts
	if pts == 0 {
		var err error
		if pts, err = c.svcCtx.Dao.ChannelPts(c.ctx, in.ChannelId); err != nil {
			return nil, err
		}
	}
	return mtproto.MakeTLMessagesAffectedHistory(&mtproto.Messages_AffectedHistory{
		Pts:      pts,
		PtsCount: del.PtsCount,
		Offset:   del.Left,
	}).To_Messages_AffectedHistory(), nil
}
