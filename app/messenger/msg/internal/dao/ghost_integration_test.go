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
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
)

// newGhost writes the account of a ghost (ADR 0048).
func (w *world) newGhost() int64 {
	w.t.Helper()
	id := newUser()
	w.exec("INSERT INTO users (id, access_hash, phone, country_code, first_name, ghost) VALUES (?, 1, ?, 'US', 'Support', 1)", id, fmt.Sprint("1888", id))
	return id
}

// mention sends from owner a message that mentions userId by name.
func (w *world) mention(owner, channelId, userId int64) (dao.ChannelSent, []int64, error) {
	w.t.Helper()
	m := mtproto.MakeTLMessage(&mtproto.Message{Message: "ask Support"}).To_Message()
	m.Entities = []*mtproto.MessageEntity{mtproto.MakeTLMessageEntityMentionName(&mtproto.MessageEntity{Offset: 4, Length: 7, UserId_INT64: userId}).To_MessageEntity()}
	var (
		sent      []dao.ChannelSent
		mentioned []int64
	)
	_, err := w.d.RunBox(context.Background(), owner, nil, func(b *userbox.Box) error {
		var err error
		sent, mentioned, _, err = w.d.SendChannel(b, channelId, []dao.Outgoing{{RandomId: time.Now().UnixNano(), Message: m}}, "")
		return err
	})
	if err != nil {
		return dao.ChannelSent{}, nil, err
	}
	return sent[0], mentioned, nil
}

// channelTraces are what the members could see of a join: the messages,
// the counters, the recent actions.
type channelTraces struct {
	messages, log        int
	participants, admins int32
}

func (w *world) channelTraces(channelId int64) channelTraces {
	w.t.Helper()
	var c channelTraces
	if err := w.check.QueryRow("SELECT COUNT(*) FROM channel_messages WHERE channel_id = ?", channelId).Scan(&c.messages); err != nil {
		w.t.Fatal(err)
	}
	if err := w.check.QueryRow("SELECT COUNT(*) FROM channel_admin_log WHERE channel_id = ?", channelId).Scan(&c.log); err != nil {
		w.t.Fatal(err)
	}
	if err := w.check.QueryRow("SELECT participants_count, admins_count FROM channels WHERE id = ?", channelId).Scan(&c.participants, &c.admins); err != nil {
		w.t.Fatal(err)
	}
	return c
}

// ghostRow is the ghost's row in the channel: state, hidden,
// available_min_id; ok false for none.
func (w *world) ghostRow(channelId, userId int64) (state int32, hidden bool, minId int32, ok bool) {
	w.t.Helper()
	err := w.check.QueryRow("SELECT state, hidden, available_min_id FROM channel_participants WHERE channel_id = ? AND user_id = ?", channelId, userId).Scan(&state, &hidden, &minId)
	if err != nil {
		return 0, false, 0, false
	}
	return state, hidden, minId, true
}

// joinedHiddenEvents are the inbox events that tell recipient it joined
// the channel hidden.
func (w *world) joinedHiddenEvents(recipient, channelId int64) []*inbox.TLInboxChannelJoinedHiddenToInbox {
	w.t.Helper()
	rows, err := w.check.Query("SELECT payload FROM mq_outbox WHERE recipient_id = ? AND method = ?", recipient, "inbox.TL_inbox_channelJoinedHiddenToInbox")
	if err != nil {
		w.t.Fatal(err)
	}
	defer rows.Close()
	var out []*inbox.TLInboxChannelJoinedHiddenToInbox
	for rows.Next() {
		var data []byte
		if err = rows.Scan(&data); err != nil {
			w.t.Fatal(err)
		}
		ev := new(inbox.TLInboxChannelJoinedHiddenToInbox)
		if err = jsonx.Unmarshal(data, ev); err != nil {
			w.t.Fatal(err)
		}
		if ev.ChannelId == channelId {
			out = append(out, ev)
		}
	}
	if err = rows.Err(); err != nil {
		w.t.Fatal(err)
	}
	return out
}

// TestMentionBringsGhost: a mention of the ghost in a supergroup it is not
// in makes it a hidden member in the same transaction as the mention row,
// with the whole history, and tells its box by an inbox event with the
// read mark before the mention; nothing the members see changes. A second
// mention finds it in. A ghost banned from the group stays out, its
// mention row written all the same.
func TestMentionBringsGhost(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	w.d.ChannelClient = ghostChannels{}
	owner, support := newUser(), w.newGhost()
	id := w.newChannelOf(owner)
	w.exec("UPDATE channels SET participants_count = 1, admins_count = 1, hidden_prehistory = 1 WHERE id = ?", id)
	w.sendChannel(owner, id, "before", 0)
	before := w.channelTraces(id)

	sent, mentioned, err := w.mention(owner, id, support)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(mentioned, support) {
		t.Errorf("mentioned %v lacks the ghost: its push would not be marked", mentioned)
	}
	state, hidden, minId, ok := w.ghostRow(id, support)
	if !ok || state != channelperm.StateIn || !hidden || minId != 0 {
		t.Errorf("the ghost's row: state %d hidden %v available_min_id %d (%v)", state, hidden, minId, ok)
	}
	after := w.channelTraces(id)
	if after.messages != before.messages+1 || after.participants != before.participants || after.admins != before.admins || after.log != before.log {
		t.Errorf("traces %+v after the mention, want %+v with the one message more", after, before)
	}
	var mentions int
	if err = w.check.QueryRow("SELECT COUNT(*) FROM channel_message_mentions WHERE channel_id = ? AND user_id = ? AND message_id = ?", id, support, sent.Message.GetId()).Scan(&mentions); err != nil || mentions != 1 {
		t.Errorf("mention rows %d, %v", mentions, err)
	}
	// The inbox event, applied: the ghost's dialog shows the mention
	// unread, what came before read, and the chat is muted for it.
	eventually(t, 10*time.Second, "the ghost's dialog", func() bool {
		var n int
		_ = w.check.QueryRow("SELECT COUNT(*) FROM dialogs WHERE user_id = ? AND peer_type = ? AND peer_id = ? AND deleted = 0", support, mtproto.PEER_CHANNEL, id).Scan(&n)
		return n == 1
	})
	if d := w.dialog(support, mtproto.PEER_CHANNEL, id); d.TopMessage != sent.Message.GetId() || d.ReadInboxMaxId != sent.Message.GetId()-1 {
		t.Errorf("the ghost's dialog %+v, want top %d read up to %d", d, sent.Message.GetId(), sent.Message.GetId()-1)
	}
	if !w.users.isMuted(support, id) {
		t.Error("the chat the ghost was called into is not muted for it")
	}

	// A second mention finds the ghost in: its row is not written again.
	w.exec("UPDATE channel_participants SET `date` = 1 WHERE channel_id = ? AND user_id = ?", id, support)
	if _, _, err = w.mention(owner, id, support); err != nil {
		t.Fatal(err)
	}
	var date int64
	if err = w.check.QueryRow("SELECT `date` FROM channel_participants WHERE channel_id = ? AND user_id = ?", id, support).Scan(&date); err != nil || date != 1 {
		t.Errorf("the ghost's row written again by a second mention: date %d, %v", date, err)
	}

	banned := w.newChannelOf(owner)
	w.exec("INSERT INTO channel_participants (channel_id, user_id, state, banned_rights) VALUES (?, ?, ?, ?)", banned, support, channelperm.StateKicked, mtproto.BANNED_VIEW_MESSAGES)
	if _, _, err = w.mention(owner, banned, support); err != nil {
		t.Fatal(err)
	}
	if state, _, _, _ := w.ghostRow(banned, support); state != channelperm.StateKicked {
		t.Errorf("the banned ghost's row: state %d", state)
	}
	if evs := w.joinedHiddenEvents(support, banned); len(evs) != 0 {
		t.Errorf("the banned ghost was brought in: %v", evs)
	}

	// Anyone else mentioned is left as it is.
	bob := newUser()
	if _, _, err = w.mention(owner, id, bob); err != nil {
		t.Fatal(err)
	}
	if _, _, _, ok := w.ghostRow(id, bob); ok {
		t.Error("a mention made a plain user a member")
	}
}

// TestGhostMayJoin: the ghost comes in by its own message, reaction or vote
// where it may read without a row (a public supergroup, the comments of a
// public channel), once; not where it is in, nor in a private chat; a plain
// user never.
func TestGhostMayJoin(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	owner, support := newUser(), w.newGhost()
	group, channelId, discussion := w.newChannelOf(owner), w.newChannelOf(owner), w.newChannelOf(owner)
	w.linkedPost(owner, channelId, discussion)
	may := func(what string, userId, id int64, want bool) {
		t.Helper()
		got, err := w.d.GhostMayJoin(context.Background(), userId, id)
		if err != nil || got != want {
			t.Errorf("%s: %v, %v, want %v", what, got, err, want)
		}
	}
	may("a private group", support, group, false)
	may("the comments of a private channel", support, discussion, false)
	public := func(id int64) {
		w.exec("INSERT INTO username (username, peer_type, peer_id, editable, active, order2) VALUES (?, ?, ?, 1, 1, 0)", fmt.Sprintf("g%d", id), mtproto.PEER_CHANNEL, id)
	}
	public(group)
	public(channelId)
	may("a public group", support, group, true)
	may("the comments of a public channel", support, discussion, true)
	may("a plain user", owner+1_000_000, group, false)
	w.exec("INSERT INTO channel_participants (channel_id, user_id, state, hidden) VALUES (?, ?, 0, 1)", group, support)
	may("in already", support, group, false)
	w.exec("INSERT INTO channel_participants (channel_id, user_id, state) VALUES (?, ?, ?)", discussion, support, channelperm.StateKicked)
	may("banned", support, discussion, false)
}

// ghostChannels answers biz channel's view of a channel: the one the
// ghost's own join shows it.
type ghostChannels struct{ noChannels }

func (ghostChannels) ChannelGetMutableChannel(_ context.Context, in *channelpb.TLChannelGetMutableChannel) (*channelpb.MutableChannel, error) {
	participants := []*channelpb.ImmutableChannelParticipant{}
	for _, id := range in.Id {
		participants = append(participants, channelpb.MakeTLImmutableChannelParticipant(&channelpb.ImmutableChannelParticipant{
			ChannelId: in.ChannelId, UserId: id, State: channelpb.StateIn, Hidden: true,
		}).To_ImmutableChannelParticipant())
	}
	return channelpb.MakeTLMutableChannel(&channelpb.MutableChannel{
		Channel: channelpb.MakeTLImmutableChannel(&channelpb.ImmutableChannel{Id: in.ChannelId, AccessHash: in.ChannelId, Megagroup: true, Title: "t",
			Photo: mtproto.MakeTLPhotoEmpty(nil).To_Photo()}).To_ImmutableChannel(),
		Participants: participants,
	}).To_MutableChannel(), nil
}

// UserSetNotifySettings records a chat muted for its user.
func (f *fakeUsers) UserSetNotifySettings(_ context.Context, in *userpb.TLUserSetNotifySettings) (*mtproto.Bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.muted == nil {
		f.muted = map[[2]int64]bool{}
	}
	f.muted[[2]int64{in.UserId, in.PeerId}] = in.GetSettings().GetMuteUntil().GetValue() > int32(time.Now().Unix())
	return mtproto.BoolTrue, nil
}

func (f *fakeUsers) isMuted(userId, peerId int64) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.muted[[2]int64{userId, peerId}]
}
