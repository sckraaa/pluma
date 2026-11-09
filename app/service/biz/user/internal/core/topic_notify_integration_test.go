//go:build integration

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
	"testing"

	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// The settings of a topic hold while its channel is a forum: push asks
// General about every message of a supergroup, so those of a forum turned
// off would mute the whole group with no way to change it (task 067).
func TestTopicNotifySettingsOfForumOnly(t *testing.T) {
	c := userCore(t)
	u, _ := newUser(t, "Alice")
	channelId := 2_000_000_000 + u
	if _, err := itRaw.Exec("INSERT INTO channels (id, access_hash, creator_user_id, title, forum) VALUES (?, 1, ?, 'f', 1)", channelId, u); err != nil {
		t.Fatal(err)
	}
	if _, err := c.UserSetTopicNotifySettings(&user.TLUserSetTopicNotifySettings{UserId: u, ChannelId: channelId, TopMsgId: 1, Settings: notifySettings(1000)}); err != nil {
		t.Fatal(err)
	}
	mute := func() int32 {
		t.Helper()
		s, err := c.UserGetTopicNotifySettings(&user.TLUserGetTopicNotifySettings{UserId: u, ChannelId: channelId, TopMsgId: 1})
		if err != nil {
			t.Fatal(err)
		}
		return s.GetMuteUntil().GetValue()
	}
	if got := mute(); got != 1000 {
		t.Errorf("in a forum: mute_until %d", got)
	}
	if _, err := itRaw.Exec("UPDATE channels SET forum = 0 WHERE id = ?", channelId); err != nil {
		t.Fatal(err)
	}
	if got := mute(); got != 0 {
		t.Errorf("the forum turned off: mute_until %d", got)
	}
	if _, err := itRaw.Exec("UPDATE channels SET forum = 1 WHERE id = ?", channelId); err != nil {
		t.Fatal(err)
	}
	if got := mute(); got != 1000 {
		t.Errorf("the forum back: mute_until %d", got)
	}
}
