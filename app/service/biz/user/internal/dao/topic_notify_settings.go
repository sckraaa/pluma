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

package dao

import (
	"context"

	"github.com/teamgram/proto/mtproto"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// topicNotifyUpdate is updateNotifySettings of a topic of a forum
// (notifyForumTopic: Android MessagesController.java:20051, tdesktop
// data_notify_settings.cpp:133, iOS AccountStateManagementUtils.swift:1351).
func topicNotifyUpdate(channelId int64, topMsgId int32, settings *mtproto.PeerNotifySettings) *mtproto.Update {
	return mtproto.MakeTLUpdateNotifySettings(&mtproto.Update{
		Peer_NOTIFYPEER: mtproto.MakeTLNotifyForumTopic(&mtproto.NotifyPeer{
			Peer:     mtproto.MakePeerChannel(channelId),
			TopMsgId: topMsgId,
		}).To_NotifyPeer(),
		NotifySettings: settings,
	}).To_Update()
}

// SetTopicNotifySettings stores id's settings for a topic and pushes
// updateNotifySettings with seq to every device of the account, in one box
// transaction (ADR 0020). Empty settings remove the row: the topic follows
// its forum again (core.telegram.org/constructor/inputPeerNotifySettings).
func (d *Dao) SetTopicNotifySettings(ctx context.Context, id, channelId int64, topMsgId int32, settings *mtproto.PeerNotifySettings) error {
	_, err := d.RunBox(ctx, id, nil, func(b *userbox.Box) error {
		var err error
		if userpb.EmptyNotifySettings(settings) {
			_, err = b.Tx().Exec("DELETE FROM user_topic_notify_settings WHERE user_id = ? AND channel_id = ? AND top_msg_id = ?", id, channelId, topMsgId)
		} else {
			c := makeDOByPeerNotifySettings(settings)
			_, err = b.Tx().Exec("INSERT INTO user_topic_notify_settings (user_id, channel_id, top_msg_id, show_previews, silent, mute_until, sound) VALUES (?, ?, ?, ?, ?, ?, ?) AS new"+
				" ON DUPLICATE KEY UPDATE show_previews = new.show_previews, silent = new.silent, mute_until = new.mute_until, sound = new.sound",
				id, channelId, topMsgId, c["show_previews"], c["silent"], c["mute_until"], c["sound"])
		}
		if err != nil {
			return err
		}
		return d.EmitSeqUpdate(b, topicNotifyUpdate(channelId, topMsgId, settings), nil, nil)
	})
	return err
}

// GetTopicNotifySettings are id's settings for a topic, empty when it has
// none or the channel is no forum now: push asks General about every message
// of a supergroup, and the settings of a forum turned off are not the
// group's. The row stays for a forum turned on again.
func (d *Dao) GetTopicNotifySettings(ctx context.Context, id, channelId int64, topMsgId int32) (*mtproto.PeerNotifySettings, error) {
	var rows []userpb.TopicNotifyRow
	if err := d.Mysql.QueryRowsPartial(ctx, &rows,
		"SELECT "+userpb.TopicNotifyColumns+" FROM user_topic_notify_settings WHERE user_id = ? AND channel_id = ? AND top_msg_id = ?"+
			" AND EXISTS (SELECT 1 FROM channels c WHERE c.id = user_topic_notify_settings.channel_id AND c.forum = 1)", id, channelId, topMsgId); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return mtproto.MakeTLPeerNotifySettings(nil).To_PeerNotifySettings(), nil
	}
	return rows[0].Settings(), nil
}

// TopicNotifyExceptions are updateNotifySettings of each topic of the forum
// with settings of id.
func (d *Dao) TopicNotifyExceptions(ctx context.Context, id, channelId int64) ([]*mtproto.Update, error) {
	var rows []userpb.TopicNotifyRow
	if err := d.Mysql.QueryRowsPartial(ctx, &rows,
		"SELECT "+userpb.TopicNotifyColumns+" FROM user_topic_notify_settings WHERE user_id = ? AND channel_id = ? ORDER BY top_msg_id", id, channelId); err != nil {
		return nil, err
	}
	out := make([]*mtproto.Update, 0, len(rows))
	for i := range rows {
		out = append(out, topicNotifyUpdate(rows[i].ChannelId, rows[i].TopMsgId, rows[i].Settings()))
	}
	return out, nil
}

// resetTopicNotifySettings removes all of id's topic settings in b's
// transaction, with updateNotifySettings of each topic: it follows its
// forum.
func (d *Dao) resetTopicNotifySettings(b *userbox.Box, id int64) error {
	var rows []userpb.TopicNotifyRow
	if err := b.Tx().QueryRowsPartial(&rows,
		"SELECT "+userpb.TopicNotifyColumns+" FROM user_topic_notify_settings WHERE user_id = ? ORDER BY channel_id, top_msg_id FOR UPDATE", id); err != nil || len(rows) == 0 {
		return err
	}
	if _, err := b.Tx().Exec("DELETE FROM user_topic_notify_settings WHERE user_id = ?", id); err != nil {
		return err
	}
	for i := range rows {
		u := topicNotifyUpdate(rows[i].ChannelId, rows[i].TopMsgId, mtproto.MakeTLPeerNotifySettings(nil).To_PeerNotifySettings())
		if err := d.EmitSeqUpdate(b, u, nil, nil); err != nil {
			return err
		}
	}
	return nil
}
