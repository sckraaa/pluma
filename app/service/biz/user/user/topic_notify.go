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

package user

import (
	"github.com/teamgram/proto/mtproto"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// TopicNotifyColumns are the columns of user_topic_notify_settings that
// TopicNotifyRow reads.
const TopicNotifyColumns = "channel_id, top_msg_id, show_previews, silent, mute_until, sound"

// TopicNotifyRow is a row of user_topic_notify_settings (task 067): -1 and
// "-1" stand for a field not set, as in user_notify_settings.
type TopicNotifyRow struct {
	ChannelId    int64  `db:"channel_id"`
	TopMsgId     int32  `db:"top_msg_id"`
	ShowPreviews int32  `db:"show_previews"`
	Silent       int32  `db:"silent"`
	MuteUntil    int32  `db:"mute_until"`
	Sound        string `db:"sound"`
}

// Settings are the notify settings the row holds.
func (r *TopicNotifyRow) Settings() *mtproto.PeerNotifySettings {
	s := mtproto.MakeTLPeerNotifySettings(nil).To_PeerNotifySettings()
	if r.ShowPreviews != -1 {
		s.ShowPreviews = mtproto.ToBool(r.ShowPreviews == 1)
	}
	if r.Silent != -1 {
		s.Silent = mtproto.ToBool(r.Silent == 1)
	}
	if r.MuteUntil != -1 {
		s.MuteUntil = &wrapperspb.Int32Value{Value: r.MuteUntil}
	}
	if r.Sound != "-1" {
		s.Sound = &wrapperspb.StringValue{Value: r.Sound}
	}
	return s
}

// EmptyNotifySettings tells settings that set nothing: the topic follows its
// forum.
func EmptyNotifySettings(s *mtproto.PeerNotifySettings) bool {
	return s.GetShowPreviews() == nil && s.GetSilent() == nil && s.GetMuteUntil() == nil && s.GetSound() == nil
}
