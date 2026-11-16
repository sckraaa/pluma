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

package service

import (
	"context"
	"encoding/base64"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/business/internal/svc"

	grpcmd "google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// served are the methods of the seven services with a handler of their own.
var served = map[string]bool{
	"AccountUpdateBusinessWorkHours": true,
	"AccountUpdateBusinessLocation":  true,
	"AccountUpdateBusinessIntro":     true,
	"AccountCreateBusinessChatLink":  true,
	"AccountEditBusinessChatLink":    true,
	"AccountDeleteBusinessChatLink":  true,
	"AccountGetBusinessChatLinks":    true,
	"AccountResolveBusinessChatLink": true,

	"MessagesGetQuickReplies":          true,
	"MessagesGetQuickReplyMessages":    true,
	"MessagesCheckQuickReplyShortcut":  true,
	"MessagesEditQuickReplyShortcut":   true,
	"MessagesReorderQuickReplies":      true,
	"MessagesDeleteQuickReplyShortcut": true,

	"AccountUpdateConnectedBot":       true,
	"AccountGetConnectedBots":         true,
	"AccountToggleConnectedBotPaused": true,
	"AccountDisablePeerConnectedBot":  true,
}

// The services of Business the session routes to bff (session.yaml): one
// Service serves them all.
var services = []reflect.Type{
	reflect.TypeOf((*mtproto.RPCBusinessOpeningHoursServer)(nil)).Elem(),
	reflect.TypeOf((*mtproto.RPCBusinessLocationServer)(nil)).Elem(),
	reflect.TypeOf((*mtproto.RPCBusinessGreetingServer)(nil)).Elem(),
	reflect.TypeOf((*mtproto.RPCBusinessIntroServer)(nil)).Elem(),
	reflect.TypeOf((*mtproto.RPCBusinessChatLinksServer)(nil)).Elem(),
	reflect.TypeOf((*mtproto.RPCBusinessQuickReplyServer)(nil)).Elem(),
	reflect.TypeOf((*mtproto.RPCBusinessConnectedBotsServer)(nil)).Elem(),
}

// A method not served yet answers ERR_ENTERPRISE_IS_BLOCKED (400), which
// the clients do not retry, never gRPC Unimplemented (500).
func TestNotServed(t *testing.T) {
	s := New(&svc.ServiceContext{})
	v := reflect.ValueOf(s)
	n := 0
	for _, iface := range services {
		if !reflect.TypeOf(s).Implements(iface) {
			t.Fatalf("Service does not serve %s", iface.Name())
		}
		for i := range iface.NumMethod() {
			m := iface.Method(i)
			n++
			if served[m.Name] {
				continue
			}
			req := reflect.New(m.Type.In(1).Elem())
			out := v.MethodByName(m.Name).Call([]reflect.Value{reflect.ValueOf(context.Background()), req})
			err, _ := out[1].Interface().(error)
			if !errors.Is(err, mtproto.ErrEnterpriseIsBlocked) {
				t.Errorf("%s: %v, want ERR_ENTERPRISE_IS_BLOCKED", m.Name, err)
			}
		}
	}
	if n != 23 {
		t.Errorf("%d methods in the seven services, want 23", n)
	}
}

// callerCtx is the context of a request of userID, a bot or not.
func callerCtx(t *testing.T, userID int64, bot bool) context.Context {
	t.Helper()
	buf, err := proto.Marshal(&metadata.RpcMetadata{UserId: userID, IsBot: bot, PermAuthKeyId: 7})
	if err != nil {
		t.Fatal(err)
	}
	return grpcmd.NewIncomingContext(context.Background(), grpcmd.Pairs("rpc_metadata", base64.StdEncoding.EncodeToString(buf)))
}

func weekly(pairs ...int32) []*mtproto.BusinessWeeklyOpen {
	var out []*mtproto.BusinessWeeklyOpen
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, mtproto.MakeTLBusinessWeeklyOpen(&mtproto.BusinessWeeklyOpen{StartMinute: pairs[i], EndMinute: pairs[i+1]}).To_BusinessWeeklyOpen())
	}
	return out
}

func hours(timezone string, open ...int32) *mtproto.TLAccountUpdateBusinessWorkHours {
	return &mtproto.TLAccountUpdateBusinessWorkHours{
		BusinessWorkHours: mtproto.MakeTLBusinessWorkHours(&mtproto.BusinessWorkHours{TimezoneId: timezone, WeeklyOpen: weekly(open...)}).To_BusinessWorkHours(),
	}
}

func intro(title, description string, sticker *mtproto.InputDocument) *mtproto.TLAccountUpdateBusinessIntro {
	return &mtproto.TLAccountUpdateBusinessIntro{
		Intro: mtproto.MakeTLInputBusinessIntro(&mtproto.InputBusinessIntro{Title: title, Description: description, Sticker: sticker}).To_InputBusinessIntro(),
	}
}

// The requests the methods refuse before they read or write anything: the
// errors core.telegram.org lists for them, and those of the limits.
func TestProfileRefused(t *testing.T) {
	s := New(&svc.ServiceContext{})
	user := callerCtx(t, 1, false)
	bot := callerCtx(t, 2, true)
	point := func(lat, long float64) *mtproto.InputGeoPoint {
		return mtproto.MakeTLInputGeoPoint(&mtproto.InputGeoPoint{Lat: lat, Long: long}).To_InputGeoPoint()
	}
	location := func(address string, geo *mtproto.InputGeoPoint) *mtproto.TLAccountUpdateBusinessLocation {
		return &mtproto.TLAccountUpdateBusinessLocation{Address: wrapperspb.String(address), GeoPoint: geo}
	}
	for _, c := range []struct {
		name string
		call func() error
		want error
	}{
		{"hours of a bot", func() error {
			_, err := s.AccountUpdateBusinessWorkHours(bot, hours("Europe/Berlin", 540, 1080))
			return err
		}, mtproto.ErrBotMethodInvalid},
		{"location of a bot", func() error { _, err := s.AccountUpdateBusinessLocation(bot, location("Main St 1", nil)); return err }, mtproto.ErrBotMethodInvalid},
		{"intro of a bot", func() error { _, err := s.AccountUpdateBusinessIntro(bot, intro("Hi", "", nil)); return err }, mtproto.ErrBotMethodInvalid},

		{"no intervals", func() error { _, err := s.AccountUpdateBusinessWorkHours(user, hours("Europe/Berlin")); return err }, mtproto.ErrBusinessWorkHoursEmpty},
		{"no valid interval", func() error {
			_, err := s.AccountUpdateBusinessWorkHours(user, hours("Europe/Berlin", 100, 100, -5, 10, 0, 11521))
			return err
		}, mtproto.ErrBusinessWorkHoursPeriodInvalid},
		{"29 intervals", func() error {
			var open []int32
			for i := int32(0); i < 29; i++ {
				open = append(open, i*300, i*300+60)
			}
			_, err := s.AccountUpdateBusinessWorkHours(user, hours("Europe/Berlin", open...))
			return err
		}, mtproto.ErrBusinessWorkHoursPeriodInvalid},
		{"unknown time zone", func() error {
			_, err := s.AccountUpdateBusinessWorkHours(user, hours("Mars/Olympus", 540, 1080))
			return err
		}, mtproto.ErrTimezoneInvalid},
		{"empty time zone", func() error { _, err := s.AccountUpdateBusinessWorkHours(user, hours("", 540, 1080)); return err }, mtproto.ErrTimezoneInvalid},
		{"the host's time zone", func() error { _, err := s.AccountUpdateBusinessWorkHours(user, hours("Local", 540, 1080)); return err }, mtproto.ErrTimezoneInvalid},

		{"97 characters of address", func() error {
			_, err := s.AccountUpdateBusinessLocation(user, location(strings.Repeat("ж", 97), nil))
			return err
		}, mtproto.ErrAddressInvalid},
		{"latitude out of range", func() error {
			_, err := s.AccountUpdateBusinessLocation(user, location("Main St 1", point(91, 0)))
			return err
		}, mtproto.ErrGeoPointInvalid},
		{"longitude out of range", func() error {
			_, err := s.AccountUpdateBusinessLocation(user, location("Main St 1", point(0, -181)))
			return err
		}, mtproto.ErrGeoPointInvalid},

		{"33 units of title", func() error {
			_, err := s.AccountUpdateBusinessIntro(user, intro(strings.Repeat("a", 33), "", nil))
			return err
		}, mtproto.ErrTitleInvalid},
		{"title of 17 emoji, 34 units", func() error {
			_, err := s.AccountUpdateBusinessIntro(user, intro(strings.Repeat("\U0001F600", 17), "", nil))
			return err
		}, mtproto.ErrTitleInvalid},
		{"71 units of description", func() error {
			_, err := s.AccountUpdateBusinessIntro(user, intro("", strings.Repeat("b", 71), nil))
			return err
		}, mtproto.ErrDescriptionInvalid},
	} {
		if err := c.call(); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
}

func chatLink(message, title string, entities ...*mtproto.MessageEntity) *mtproto.InputBusinessChatLink {
	l := &mtproto.InputBusinessChatLink{Message: message, Entities: entities}
	if title != "" {
		l.Title = wrapperspb.String(title)
	}
	return mtproto.MakeTLInputBusinessChatLink(l).To_InputBusinessChatLink()
}

// The requests of the chat links refused before they read or write
// anything: a bot, a slug no link could have, a text or a title over the
// limits.
func TestChatLinksRefused(t *testing.T) {
	s := New(&svc.ServiceContext{})
	user := callerCtx(t, 1, false)
	bot := callerCtx(t, 2, true)
	bold := func(offset, length int32) *mtproto.MessageEntity {
		return mtproto.MakeTLMessageEntityBold(&mtproto.MessageEntity{Offset: offset, Length: length}).To_MessageEntity()
	}
	const slug = "AbCdEfGh12345678"
	for _, c := range []struct {
		name string
		call func() error
		want error
	}{
		{"create by a bot", func() error {
			_, err := s.AccountCreateBusinessChatLink(bot, &mtproto.TLAccountCreateBusinessChatLink{Link: chatLink("hi", "")})
			return err
		}, mtproto.ErrBotMethodInvalid},
		{"list of a bot", func() error {
			_, err := s.AccountGetBusinessChatLinks(bot, &mtproto.TLAccountGetBusinessChatLinks{})
			return err
		}, mtproto.ErrBotMethodInvalid},
		{"4097 units of text", func() error {
			_, err := s.AccountCreateBusinessChatLink(user, &mtproto.TLAccountCreateBusinessChatLink{Link: chatLink(strings.Repeat("a", 4097), "")})
			return err
		}, mtproto.ErrMessageTooLong},
		{"2049 emoji of text, 4098 units", func() error {
			_, err := s.AccountCreateBusinessChatLink(user, &mtproto.TLAccountCreateBusinessChatLink{Link: chatLink(strings.Repeat("\U0001F600", 2049), "")})
			return err
		}, mtproto.ErrMessageTooLong},
		{"33 units of title", func() error {
			_, err := s.AccountCreateBusinessChatLink(user, &mtproto.TLAccountCreateBusinessChatLink{Link: chatLink("hi", strings.Repeat("t", 33))})
			return err
		}, mtproto.ErrTitleInvalid},
		{"more entities than units", func() error {
			_, err := s.AccountCreateBusinessChatLink(user, &mtproto.TLAccountCreateBusinessChatLink{Link: chatLink("hi", "", bold(0, 1), bold(1, 1), bold(0, 2))})
			return err
		}, mtproto.ErrEntitiesTooLong},
		{"edit of an empty slug", func() error {
			_, err := s.AccountEditBusinessChatLink(user, &mtproto.TLAccountEditBusinessChatLink{Slug: "https://pluma.chat/m/", Link: chatLink("hi", "")})
			return err
		}, mtproto.ErrChatlinkSlugEmpty},
		{"edit of a malformed slug", func() error {
			_, err := s.AccountEditBusinessChatLink(user, &mtproto.TLAccountEditBusinessChatLink{Slug: "short", Link: chatLink("hi", "")})
			return err
		}, mtproto.ErrChatlinkSlugExpired},
		{"edit with a long title", func() error {
			_, err := s.AccountEditBusinessChatLink(user, &mtproto.TLAccountEditBusinessChatLink{Slug: slug, Link: chatLink("hi", strings.Repeat("t", 33))})
			return err
		}, mtproto.ErrTitleInvalid},
		{"delete by a bot", func() error {
			_, err := s.AccountDeleteBusinessChatLink(bot, &mtproto.TLAccountDeleteBusinessChatLink{Slug: slug})
			return err
		}, mtproto.ErrBotMethodInvalid},
		{"delete of an empty slug", func() error {
			_, err := s.AccountDeleteBusinessChatLink(user, &mtproto.TLAccountDeleteBusinessChatLink{Slug: " "})
			return err
		}, mtproto.ErrChatlinkSlugEmpty},
		{"resolve of an empty slug", func() error {
			_, err := s.AccountResolveBusinessChatLink(user, &mtproto.TLAccountResolveBusinessChatLink{Slug: ""})
			return err
		}, mtproto.ErrChatlinkSlugEmpty},
		{"resolve of a slug with a bad character", func() error {
			_, err := s.AccountResolveBusinessChatLink(user, &mtproto.TLAccountResolveBusinessChatLink{Slug: "AbCdEfGh1234567-"})
			return err
		}, mtproto.ErrChatlinkSlugExpired},
	} {
		if err := c.call(); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
}

// The requests of the connected bot refused before they read or write
// anything: a bot, a user that is no bot, recipients that name the caller,
// too many users or nothing, a chat that is no private chat with another
// user.
func TestConnectedBotRefused(t *testing.T) {
	s := New(&svc.ServiceContext{})
	user := callerCtx(t, 1, false)
	bot := callerCtx(t, 2, true)
	inputUser := func(id int64) *mtproto.InputUser {
		return mtproto.MakeTLInputUser(&mtproto.InputUser{UserId: id, AccessHash: 1}).To_InputUser()
	}
	self := mtproto.MakeTLInputUserSelf(nil).To_InputUser()
	recipients := func(r *mtproto.InputBusinessBotRecipients) *mtproto.InputBusinessBotRecipients {
		return mtproto.MakeTLInputBusinessBotRecipients(r).To_InputBusinessBotRecipients()
	}
	connect := func(b *mtproto.InputUser, r *mtproto.InputBusinessBotRecipients) *mtproto.TLAccountUpdateConnectedBot {
		return &mtproto.TLAccountUpdateConnectedBot{
			Rights:                                mtproto.MakeTLBusinessBotRights(&mtproto.BusinessBotRights{Reply: true}).To_BusinessBotRights(),
			Bot:                                   b,
			Recipients_INPUTBUSINESSBOTRECIPIENTS: r,
		}
	}
	many := make([]*mtproto.InputUser, 0, 101)
	for i := range 101 {
		many = append(many, inputUser(int64(100+i)))
	}
	peerUser := func(id int64) *mtproto.InputPeer {
		return mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: id, AccessHash: 1}).To_InputPeer()
	}
	peerSelf := mtproto.MakeTLInputPeerSelf(nil).To_InputPeer()
	peerChat := mtproto.MakeTLInputPeerChat(&mtproto.InputPeer{ChatId: 5}).To_InputPeer()
	for _, c := range []struct {
		name string
		call func() error
		want error
	}{
		{"connect by a bot", func() error {
			_, err := s.AccountUpdateConnectedBot(bot, connect(inputUser(3), recipients(&mtproto.InputBusinessBotRecipients{Contacts: true})))
			return err
		}, mtproto.ErrBotMethodInvalid},
		{"list of a bot", func() error {
			_, err := s.AccountGetConnectedBots(bot, &mtproto.TLAccountGetConnectedBots{})
			return err
		}, mtproto.ErrBotMethodInvalid},
		{"pause by a bot", func() error {
			_, err := s.AccountToggleConnectedBotPaused(bot, &mtproto.TLAccountToggleConnectedBotPaused{Peer: peerUser(3), Paused: mtproto.BoolTrue})
			return err
		}, mtproto.ErrBotMethodInvalid},
		{"disable by a bot", func() error {
			_, err := s.AccountDisablePeerConnectedBot(bot, &mtproto.TLAccountDisablePeerConnectedBot{Peer: peerUser(3)})
			return err
		}, mtproto.ErrBotMethodInvalid},

		{"connect no user", func() error {
			_, err := s.AccountUpdateConnectedBot(user, connect(mtproto.MakeTLInputUserEmpty(nil).To_InputUser(), recipients(&mtproto.InputBusinessBotRecipients{Contacts: true})))
			return err
		}, mtproto.ErrBotBusinessMissing},
		{"connect the caller", func() error {
			_, err := s.AccountUpdateConnectedBot(user, connect(self, recipients(&mtproto.InputBusinessBotRecipients{Contacts: true})))
			return err
		}, mtproto.ErrBotBusinessMissing},
		{"recipients without a chat", func() error {
			_, err := s.AccountUpdateConnectedBot(user, connect(inputUser(3), recipients(&mtproto.InputBusinessBotRecipients{})))
			return err
		}, mtproto.ErrBusinessRecipientsEmpty},
		{"no recipients at all", func() error {
			_, err := s.AccountUpdateConnectedBot(user, connect(inputUser(3), nil))
			return err
		}, mtproto.ErrBusinessRecipientsEmpty},
		{"recipients name the caller", func() error {
			_, err := s.AccountUpdateConnectedBot(user, connect(inputUser(3), recipients(&mtproto.InputBusinessBotRecipients{Users: []*mtproto.InputUser{self}})))
			return err
		}, mtproto.ErrUserIdInvalid},
		{"excluded users name the caller by id", func() error {
			_, err := s.AccountUpdateConnectedBot(user, connect(inputUser(3), recipients(&mtproto.InputBusinessBotRecipients{Contacts: true, ExcludeUsers: []*mtproto.InputUser{inputUser(1)}})))
			return err
		}, mtproto.ErrUserIdInvalid},
		{"101 users", func() error {
			_, err := s.AccountUpdateConnectedBot(user, connect(inputUser(3), recipients(&mtproto.InputBusinessBotRecipients{Users: many})))
			return err
		}, mtproto.ErrUsersTooMuch},

		{"pause in Saved Messages", func() error {
			_, err := s.AccountToggleConnectedBotPaused(user, &mtproto.TLAccountToggleConnectedBotPaused{Peer: peerSelf, Paused: mtproto.BoolTrue})
			return err
		}, mtproto.ErrPeerIdInvalid},
		{"pause in a group", func() error {
			_, err := s.AccountToggleConnectedBotPaused(user, &mtproto.TLAccountToggleConnectedBotPaused{Peer: peerChat, Paused: mtproto.BoolTrue})
			return err
		}, mtproto.ErrPeerIdInvalid},
		{"disable in the chat with the caller by id", func() error {
			_, err := s.AccountDisablePeerConnectedBot(user, &mtproto.TLAccountDisablePeerConnectedBot{Peer: peerUser(1)})
			return err
		}, mtproto.ErrPeerIdInvalid},
	} {
		if err := c.call(); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
}
