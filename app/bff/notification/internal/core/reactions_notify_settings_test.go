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
	"context"
	"encoding/binary"
	"errors"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/notification/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/notification/internal/svc"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"

	"github.com/zeromicro/go-zero/core/logx"
)

const reactionsUser = 77

// fakeReactionSettings is biz user: it answers get with stored and set with
// the settings it was given, and keeps the requests.
type fakeReactionSettings struct {
	user_client.UserClient
	stored *mtproto.ReactionsNotifySettings
	gets   []*userpb.TLUserGetReactionsNotifySettings
	sets   []*userpb.TLUserSetReactionsNotifySettings
}

func (f *fakeReactionSettings) UserGetReactionsNotifySettings(_ context.Context, in *userpb.TLUserGetReactionsNotifySettings) (*mtproto.ReactionsNotifySettings, error) {
	f.gets = append(f.gets, in)
	return f.stored, nil
}

func (f *fakeReactionSettings) UserSetReactionsNotifySettings(_ context.Context, in *userpb.TLUserSetReactionsNotifySettings) (*mtproto.ReactionsNotifySettings, error) {
	f.sets = append(f.sets, in)
	return in.GetSettings(), nil
}

func reactionsCore(users user_client.UserClient) *NotificationCore {
	ctx := context.Background()
	return &NotificationCore{
		ctx:    ctx,
		svcCtx: &svc.ServiceContext{Dao: &dao.Dao{UserClient: users}},
		Logger: logx.WithContext(ctx),
		MD:     &metadata.RpcMetadata{UserId: reactionsUser},
	}
}

func fromContacts() *mtproto.ReactionNotificationsFrom {
	return mtproto.MakeTLReactionNotificationsFromContacts(nil).To_ReactionNotificationsFrom()
}

func fromAll() *mtproto.ReactionNotificationsFrom {
	return mtproto.MakeTLReactionNotificationsFromAll(nil).To_ReactionNotificationsFrom()
}

func localSound(title, data string) *mtproto.NotificationSound {
	return mtproto.MakeTLNotificationSoundLocal(&mtproto.NotificationSound{Title: title, Data: data}).To_NotificationSound()
}

// telegramDefaults are what Telegram answers a fresh account.
func telegramDefaults() *mtproto.ReactionsNotifySettings {
	return mtproto.MakeTLReactionsNotifySettings(&mtproto.ReactionsNotifySettings{
		MessagesNotifyFrom:  fromContacts(),
		StoriesNotifyFrom:   fromContacts(),
		PollVotesNotifyFrom: fromContacts(),
		Sound:               localSound("default", "default"),
		ShowPreviews:        mtproto.BoolTrue,
	}).To_ReactionsNotifySettings()
}

// account.getReactionsNotifySettings answers the account's settings as biz
// keeps them.
func TestGetReactionsNotifySettings(t *testing.T) {
	users := &fakeReactionSettings{stored: telegramDefaults()}
	r, err := reactionsCore(users).AccountGetReactionsNotifySettings(&mtproto.TLAccountGetReactionsNotifySettings{})
	if err != nil {
		t.Fatal(err)
	}
	if r != users.stored {
		t.Errorf("answered %+v, want what biz keeps", r)
	}
	if len(users.gets) != 1 || users.gets[0].GetUserId() != reactionsUser {
		t.Errorf("biz asked %v, want the settings of user %d", users.gets, reactionsUser)
	}
}

// Only users have reaction notification settings: a bot gets
// BOT_METHOD_INVALID, as for the other user-only methods.
func TestReactionsNotifySettingsNotForBots(t *testing.T) {
	users := &fakeReactionSettings{stored: telegramDefaults()}
	c := reactionsCore(users)
	c.MD.IsBot = true
	if _, err := c.AccountGetReactionsNotifySettings(&mtproto.TLAccountGetReactionsNotifySettings{}); !errors.Is(err, mtproto.ErrBotMethodInvalid) {
		t.Errorf("get by a bot: %v, want BOT_METHOD_INVALID", err)
	}
	if _, err := c.AccountSetReactionsNotifySettings(&mtproto.TLAccountSetReactionsNotifySettings{Settings: telegramDefaults()}); !errors.Is(err, mtproto.ErrBotMethodInvalid) {
		t.Errorf("set by a bot: %v, want BOT_METHOD_INVALID", err)
	}
	if len(users.gets)+len(users.sets) != 0 {
		t.Errorf("a bot reached biz: %d gets, %d sets", len(users.gets), len(users.sets))
	}
}

// Settings the server cannot keep are SETTINGS_INVALID and never reach biz.
func TestSetReactionsNotifySettingsInvalid(t *testing.T) {
	valid := func(edit func(s *mtproto.ReactionsNotifySettings)) *mtproto.ReactionsNotifySettings {
		s := telegramDefaults()
		edit(s)
		return s
	}
	unknownFrom := &mtproto.ReactionNotificationsFrom{Constructor: 0x12345678}
	long := strings.Repeat("я", userpb.NotificationSoundTextMaxLen+1)
	for name, s := range map[string]*mtproto.ReactionsNotifySettings{
		"no settings":           nil,
		"settings of no kind":   {Sound: localSound("default", "default"), ShowPreviews: mtproto.BoolTrue},
		"no sound":              valid(func(s *mtproto.ReactionsNotifySettings) { s.Sound = nil }),
		"no show_previews":      valid(func(s *mtproto.ReactionsNotifySettings) { s.ShowPreviews = nil }),
		"show_previews no Bool": valid(func(s *mtproto.ReactionsNotifySettings) { s.ShowPreviews = &mtproto.Bool{} }),
		"an unknown messages":   valid(func(s *mtproto.ReactionsNotifySettings) { s.MessagesNotifyFrom = unknownFrom }),
		"an unknown stories":    valid(func(s *mtproto.ReactionsNotifySettings) { s.StoriesNotifyFrom = unknownFrom }),
		"an unknown poll votes": valid(func(s *mtproto.ReactionsNotifySettings) { s.PollVotesNotifyFrom = unknownFrom }),
		"an unknown sound": valid(func(s *mtproto.ReactionsNotifySettings) {
			s.Sound = &mtproto.NotificationSound{Constructor: 0x12345678, Title: "x"}
		}),
		"a long title":      valid(func(s *mtproto.ReactionsNotifySettings) { s.Sound = localSound(long, "default") }),
		"a long data":       valid(func(s *mtproto.ReactionsNotifySettings) { s.Sound = localSound("default", long) }),
		"a title not UTF-8": valid(func(s *mtproto.ReactionsNotifySettings) { s.Sound = localSound("\xff\xfe", "default") }),
	} {
		users := &fakeReactionSettings{}
		_, err := reactionsCore(users).AccountSetReactionsNotifySettings(&mtproto.TLAccountSetReactionsNotifySettings{Settings: s})
		if !errors.Is(err, mtproto.ErrSettingsInvalid) {
			t.Errorf("%s: %v, want SETTINGS_INVALID", name, err)
		}
		if len(users.sets) != 0 {
			t.Errorf("%s reached biz", name)
		}
	}

	// The longest title and data are kept, counted in characters.
	users := &fakeReactionSettings{}
	most := strings.Repeat("я", userpb.NotificationSoundTextMaxLen)
	if _, err := reactionsCore(users).AccountSetReactionsNotifySettings(&mtproto.TLAccountSetReactionsNotifySettings{
		Settings: valid(func(s *mtproto.ReactionsNotifySettings) { s.Sound = localSound(most, most) }),
	}); err != nil || len(users.sets) != 1 {
		t.Errorf("a title and data of %d characters: %v", userpb.NotificationSoundTextMaxLen, err)
	}
}

// setRequest is account.setReactionsNotifySettings as a client sends it:
// with #71e4ea58 messages from all, stories absent, poll votes from
// contacts; with #56e34970 the same without poll votes. The sound is
// notificationSoundLocal{"Default", "Default"}, previews off.
func setRequest(settings uint32) []byte {
	x := mtproto.NewEncodeBuf(128)
	x.Int(int32(mtproto.CRC32_account_setReactionsNotifySettings))
	x.UInt(settings)
	if settings == uint32(mtproto.CRC32_reactionsNotifySettings_71e4ea58) {
		x.UInt(1<<0 | 1<<2)
		x.Int(int32(mtproto.CRC32_reactionNotificationsFromAll))
		x.Int(int32(mtproto.CRC32_reactionNotificationsFromContacts))
	} else {
		x.UInt(1 << 0)
		x.Int(int32(mtproto.CRC32_reactionNotificationsFromAll))
	}
	x.Int(int32(mtproto.CRC32_notificationSoundLocal))
	x.String("Default")
	x.String("Default")
	x.Int(int32(mtproto.CRC32_boolFalse))
	return x.GetBuf()
}

// A request of either constructor decodes, reaches biz as sent, and only
// #56e34970, which has no poll votes field, keeps the stored poll votes
// setting.
func TestSetReactionsNotifySettingsConstructors(t *testing.T) {
	for _, tc := range []struct {
		constructor   mtproto.TLConstructor
		keepPollVotes bool
		pollVotes     *mtproto.ReactionNotificationsFrom
	}{
		{mtproto.CRC32_reactionsNotifySettings_71e4ea58, false, fromContacts()},
		{mtproto.CRC32_reactionsNotifySettings_56e34970, true, nil},
	} {
		d := mtproto.NewDecodeBuf(setRequest(uint32(tc.constructor)))
		o := d.Object()
		if err := d.GetError(); err != nil {
			t.Fatalf("%x: %v", uint32(tc.constructor), err)
		}
		req, ok := o.(*mtproto.TLAccountSetReactionsNotifySettings)
		if !ok {
			t.Fatalf("%x decoded to %T", uint32(tc.constructor), o)
		}

		users := &fakeReactionSettings{}
		if _, err := reactionsCore(users).AccountSetReactionsNotifySettings(req); err != nil {
			t.Fatalf("%x: %v", uint32(tc.constructor), err)
		}
		if len(users.sets) != 1 {
			t.Fatalf("%x: %d sets reached biz", uint32(tc.constructor), len(users.sets))
		}
		set := users.sets[0]
		s := set.GetSettings()
		switch {
		case set.GetUserId() != reactionsUser:
			t.Errorf("%x: set for user %d", uint32(tc.constructor), set.GetUserId())
		case mtproto.FromBool(set.GetKeepPollVotes()) != tc.keepPollVotes:
			t.Errorf("%x: keep_poll_votes %v, want %v", uint32(tc.constructor), mtproto.FromBool(set.GetKeepPollVotes()), tc.keepPollVotes)
		case s.GetMessagesNotifyFrom().GetPredicateName() != mtproto.Predicate_reactionNotificationsFromAll,
			s.GetStoriesNotifyFrom() != nil,
			s.GetPollVotesNotifyFrom().GetPredicateName() != tc.pollVotes.GetPredicateName(),
			(s.GetPollVotesNotifyFrom() == nil) != (tc.pollVotes == nil):
			t.Errorf("%x: from %v, %v, %v", uint32(tc.constructor),
				s.GetMessagesNotifyFrom(), s.GetStoriesNotifyFrom(), s.GetPollVotesNotifyFrom())
		case s.GetSound().GetPredicateName() != mtproto.Predicate_notificationSoundLocal,
			s.GetSound().GetTitle() != "Default", s.GetSound().GetData() != "Default":
			t.Errorf("%x: the sound changed on the way", uint32(tc.constructor))
		case mtproto.FromBool(s.GetShowPreviews()) || s.GetShowPreviews() == nil:
			t.Errorf("%x: show_previews %v", uint32(tc.constructor), s.GetShowPreviews())
		}
	}
}

// The answer goes to a client of the layer 228 as #71e4ea58 and to one of
// the layer 223 as #56e34970, without poll votes.
func TestReactionsNotifySettingsReplyLayers(t *testing.T) {
	users := &fakeReactionSettings{stored: telegramDefaults()}
	c := reactionsCore(users)
	got, err := c.AccountGetReactionsNotifySettings(&mtproto.TLAccountGetReactionsNotifySettings{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := c.AccountSetReactionsNotifySettings(&mtproto.TLAccountSetReactionsNotifySettings{
		Settings: mtproto.MakeTLReactionsNotifySettings(&mtproto.ReactionsNotifySettings{
			StoriesNotifyFrom:   fromAll(),
			PollVotesNotifyFrom: fromAll(),
			Sound:               mtproto.MakeTLNotificationSoundRingtone(&mtproto.NotificationSound{Id: 42}).To_NotificationSound(),
			ShowPreviews:        mtproto.BoolFalse,
		}).To_ReactionsNotifySettings(),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		layer       int32
		constructor mtproto.TLConstructor
	}{
		{228, mtproto.CRC32_reactionsNotifySettings_71e4ea58},
		{223, mtproto.CRC32_reactionsNotifySettings_56e34970},
	} {
		for name, r := range map[string]*mtproto.ReactionsNotifySettings{"get": got, "set": set} {
			x := mtproto.NewEncodeBuf(128)
			if err := r.Encode(x, tc.layer); err != nil {
				t.Fatalf("%s at the layer %d: %v", name, tc.layer, err)
			}
			b := x.GetBuf()
			if len(b) < 4 || mtproto.TLConstructor(int32(binary.LittleEndian.Uint32(b))) != tc.constructor {
				t.Fatalf("%s at the layer %d: % x, want the constructor %x", name, tc.layer, b, uint32(tc.constructor))
			}
			back := &mtproto.ReactionsNotifySettings{}
			if err := back.Decode(mtproto.NewDecodeBuf(b)); err != nil {
				t.Fatalf("%s at the layer %d: %v", name, tc.layer, err)
			}
			wantPollVotes := r.GetPollVotesNotifyFrom().GetPredicateName()
			if tc.layer == 223 {
				wantPollVotes = ""
			}
			if back.GetMessagesNotifyFrom().GetPredicateName() != r.GetMessagesNotifyFrom().GetPredicateName() ||
				back.GetStoriesNotifyFrom().GetPredicateName() != r.GetStoriesNotifyFrom().GetPredicateName() ||
				back.GetPollVotesNotifyFrom().GetPredicateName() != wantPollVotes ||
				back.GetSound().GetPredicateName() != r.GetSound().GetPredicateName() ||
				back.GetSound().GetTitle() != r.GetSound().GetTitle() ||
				back.GetSound().GetData() != r.GetSound().GetData() ||
				back.GetSound().GetId() != r.GetSound().GetId() ||
				back.GetShowPreviews().GetPredicateName() != r.GetShowPreviews().GetPredicateName() {
				t.Errorf("%s at the layer %d came back as %+v", name, tc.layer, back)
			}
		}
	}
}
