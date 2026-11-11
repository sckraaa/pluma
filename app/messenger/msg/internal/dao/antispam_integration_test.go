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

package dao_test

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/antispam"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
)

// antispamChannels records the deletions the antispam's events ask biz
// channel for.
type antispamChannels struct {
	noChannels
	mu    sync.Mutex
	calls []*channelpb.TLChannelAntispamDelete
}

func (a *antispamChannels) ChannelAntispamDelete(_ context.Context, in *channelpb.TLChannelAntispamDelete) (*mtproto.Bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, in)
	return mtproto.BoolTrue, nil
}

func (a *antispamChannels) called(userId, channelId int64) []*channelpb.TLChannelAntispamDelete {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []*channelpb.TLChannelAntispamDelete
	for _, c := range a.calls {
		if c.UserId == userId && c.ChannelId == channelId {
			out = append(out, c)
		}
	}
	return out
}

// account writes the users row of a person.
func (w *world) account() int64 {
	w.t.Helper()
	id := newUser()
	w.exec("INSERT INTO users (id, access_hash, phone, country_code, first_name) VALUES (?, 1, ?, 'US', 'U')", id, fmt.Sprint("1777", id))
	return id
}

// joinedNow makes userId a member of the channel who joined just now.
func (w *world) joinedNow(channelId, userId int64) {
	w.t.Helper()
	w.exec("INSERT INTO channel_participants (channel_id, user_id, role, `date`) VALUES (?, ?, ?, UNIX_TIMESTAMP())", channelId, userId, channelperm.RoleMember)
}

func linkText(text string) *mtproto.Message {
	return mtproto.MakeTLMessage(&mtproto.Message{
		Message:  text,
		Entities: []*mtproto.MessageEntity{mtproto.MakeTLMessageEntityUrl(&mtproto.MessageEntity{Offset: 0, Length: int32(len(text))}).To_MessageEntity()},
	}).To_Message()
}

func (w *world) post(from, channelId int64, m *mtproto.Message) int32 {
	w.t.Helper()
	sent, err := w.sendOutgoing(from, channelId, dao.Outgoing{RandomId: randomId(), Message: m})
	if err != nil {
		w.t.Fatalf("send from %d: %v", from, err)
	}
	return sent[0].Message.GetId()
}

// TestAntispamSend: in a supergroup with the antispam on, the link of a
// member who joined just now is sent as any message, and its send writes
// the antispam's event, which the inbox consumer hands to biz channel to
// delete; an admin's link, an old member's, a plain text and an exempt
// member's stay, and a group without the antispam deletes nothing. The
// same long text in three groups within the window is spam from anyone
// checked, wherever the first two went. The events of the antispam go one
// lane, in the order of their sends, so once the last is handed over, none
// of the messages before it that stay was.
func TestAntispamSend(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	channels := &antispamChannels{}
	w.d.ChannelClient = channels
	owner, newbie, oldie, admin, vouched := w.account(), w.account(), w.account(), w.account(), w.account()
	id := w.newChannelOf(owner)
	w.exec("UPDATE channels SET antispam = 1 WHERE id = ?", id)
	w.joinedNow(id, newbie)
	w.member(id, oldie, channelperm.RoleMember)
	w.member(id, admin, channelperm.RoleAdmin)
	w.joinedNow(id, vouched)
	w.exec("INSERT INTO channel_antispam_exempt (channel_id, user_id, `date`) VALUES (?, ?, 1)", id, vouched)

	spam := w.post(newbie, id, linkText("https://spam.example"))
	if text, _, _ := w.channelMessage(id, spam); text != "https://spam.example" {
		t.Errorf("the spam was not sent: %q", text)
	}
	eventually(t, 10*time.Second, "the deletion asked of biz channel", func() bool {
		calls := channels.called(newbie, id)
		return len(calls) == 1 && slices.Equal(calls[0].Id, []int32{spam}) && calls[0].Rule == int32(antispam.RuleLink)
	})

	w.post(newbie, id, mtproto.MakeTLMessage(&mtproto.Message{Message: "hello"}).To_Message())
	w.post(admin, id, linkText("https://docs.example"))
	w.post(oldie, id, linkText("https://news.example"))
	w.post(vouched, id, linkText("https://shop.example"))
	quiet := w.newChannelOf(owner)
	w.joinedNow(quiet, newbie)
	w.post(newbie, quiet, linkText("https://spam.example"))

	// The same long text, by an old member: to two groups without the
	// antispam, then to this one.
	text := "Great deal on followers, write to me now for a discount"
	other1, other2 := w.newChannelOf(owner), w.newChannelOf(owner)
	w.member(other1, oldie, channelperm.RoleMember)
	w.member(other2, oldie, channelperm.RoleMember)
	w.post(oldie, other1, mtproto.MakeTLMessage(&mtproto.Message{Message: text}).To_Message())
	w.post(oldie, other2, mtproto.MakeTLMessage(&mtproto.Message{Message: "  great deal on followers, write to me now for a DISCOUNT"}).To_Message())
	third := w.post(oldie, id, mtproto.MakeTLMessage(&mtproto.Message{Message: text}).To_Message())
	eventually(t, 10*time.Second, "the cross-post handed over", func() bool {
		calls := channels.called(oldie, id)
		return len(calls) == 1 && slices.Equal(calls[0].Id, []int32{third}) && calls[0].Rule == int32(antispam.RuleCrossPost)
	})
	for _, c := range []struct {
		name          string
		user, channel int64
		want          int
	}{
		{"the new member's plain text", newbie, id, 1},
		{"the admin's link", admin, id, 0},
		{"the exempt member's link", vouched, id, 0},
		{"the group without the antispam", newbie, quiet, 0},
		{"the first groups of the cross-post", oldie, other1, 0},
		{"the first groups of the cross-post", oldie, other2, 0},
	} {
		if got := len(channels.called(c.user, c.channel)); got != c.want {
			t.Errorf("%s: %d deletions asked, want %d", c.name, got, c.want)
		}
	}

	var rows int
	// Its link, and the long text in each of three groups.
	if err := w.check.QueryRow("SELECT COUNT(*) FROM antispam_recent_posts WHERE user_id = ?", oldie).Scan(&rows); err != nil || rows != 4 {
		t.Errorf("the old member's recent posts %d, %v; want 4", rows, err)
	}
	// Notes older than the window go with the next send.
	w.exec("UPDATE antispam_recent_posts SET `date` = `date` - ? WHERE user_id = ?", antispam.CrossPostWindow+1, oldie)
	w.post(oldie, other1, mtproto.MakeTLMessage(&mtproto.Message{Message: "ok"}).To_Message())
	if err := w.check.QueryRow("SELECT COUNT(*) FROM antispam_recent_posts WHERE user_id = ?", oldie).Scan(&rows); err != nil || rows != 0 {
		t.Errorf("old notes left %d, %v", rows, err)
	}
}
