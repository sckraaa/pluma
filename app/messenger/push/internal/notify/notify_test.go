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

package notify

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const (
	me    = int64(42) // the recipient
	anna  = int64(7)
	boris = int64(8)
	group = int64(100)
)

func users() []*mtproto.User {
	return []*mtproto.User{
		mtproto.MakeTLUser(&mtproto.User{Id: anna, FirstName: wrapperspb.String("Анна"), LastName: wrapperspb.String("Каренина")}).To_User(),
		mtproto.MakeTLUser(&mtproto.User{Id: boris, FirstName: wrapperspb.String("Борис")}).To_User(),
		mtproto.MakeTLUser(&mtproto.User{Id: me, FirstName: wrapperspb.String("Я")}).To_User(),
	}
}

func chats() []*mtproto.Chat {
	return []*mtproto.Chat{mtproto.MakeTLChat(&mtproto.Chat{Id: group, Title: "Дача"}).To_Chat()}
}

type msgOpt func(m *mtproto.Message)

func inGroup(m *mtproto.Message) { m.PeerId = mtproto.MakePeerChat(group) }
func from(id int64) msgOpt {
	return func(m *mtproto.Message) { m.FromId = mtproto.MakePeerUser(id) }
}
func text(s string) msgOpt { return func(m *mtproto.Message) { m.Message = s } }
func media(md *mtproto.MessageMedia) msgOpt {
	return func(m *mtproto.Message) { m.Media = md }
}
func album(id int64) msgOpt {
	return func(m *mtproto.Message) { m.GroupedId = wrapperspb.Int64(id) }
}
func forwarded(m *mtproto.Message) {
	m.FwdFrom = mtproto.MakeTLMessageFwdHeader(&mtproto.MessageFwdHeader{}).To_MessageFwdHeader()
}

func message(id int32, opts ...msgOpt) *mtproto.Message {
	m := &mtproto.Message{Id: id, PeerId: mtproto.MakePeerUser(anna), FromId: mtproto.MakePeerUser(anna), Date: 1}
	for _, o := range opts {
		o(m)
	}
	return mtproto.MakeTLMessage(m).To_Message()
}

func service(id int32, a *mtproto.MessageAction, opts ...msgOpt) *mtproto.Message {
	m := &mtproto.Message{Id: id, PeerId: mtproto.MakePeerChat(group), FromId: mtproto.MakePeerUser(anna), Action: a}
	for _, o := range opts {
		o(m)
	}
	return mtproto.MakeTLMessageService(m).To_Message()
}

func updates(ms ...*mtproto.Message) *mtproto.Updates {
	var list []*mtproto.Update
	for _, m := range ms {
		list = append(list, mtproto.MakeTLUpdateNewMessage(&mtproto.Update{Message_MESSAGE: m}).To_Update())
	}
	return mtproto.MakeTLUpdates(&mtproto.Updates{Updates: list, Users: users(), Chats: chats()}).To_Updates()
}

func document(attrs ...*mtproto.DocumentAttribute) *mtproto.MessageMedia {
	return mtproto.MakeTLMessageMediaDocument(&mtproto.MessageMedia{
		Document: mtproto.MakeTLDocument(&mtproto.Document{Attributes: attrs}).To_Document(),
	}).To_MessageMedia()
}

func one(t *testing.T, ups *mtproto.Updates) *Notification {
	t.Helper()
	ns := FromUpdates(me, ups)
	if len(ns) != 1 {
		t.Fatalf("%d notifications, want 1: %+v", len(ns), ns)
	}
	return ns[0]
}

type android struct {
	LocKey  string           `json:"loc_key"`
	LocArgs []string         `json:"loc_args"`
	Custom  map[string]int64 `json:"custom"`
	UserId  int64            `json:"user_id"`
}

func androidOf(t *testing.T, n *Notification, hide bool) android {
	t.Helper()
	b, err := Android(n, hide)
	if err != nil {
		t.Fatal(err)
	}
	var a android
	if err := json.Unmarshal(b, &a); err != nil {
		t.Fatalf("%s: %v", b, err)
	}
	if a.Custom == nil || a.UserId != me {
		t.Fatalf("no custom or user_id: %s", b)
	}
	return a
}

func iosOf(t *testing.T, n *Notification, lang string, hide Hide) (*IOSPush, map[string]any) {
	t.Helper()
	p, err := IOS(n, lang, hide)
	if err != nil || p == nil {
		t.Fatalf("IOS: %v %v", p, err)
	}
	var m map[string]any
	if err := json.Unmarshal(p.Payload, &m); err != nil {
		t.Fatal(err)
	}
	return p, m
}

func alertOf(m map[string]any) (string, string) {
	aps, _ := m["aps"].(map[string]any)
	alert, _ := aps["alert"].(map[string]any)
	title, _ := alert["title"].(string)
	body, _ := alert["body"].(string)
	return title, body
}

func TestPrivateText(t *testing.T) {
	n := one(t, updates(message(5, text("привет"))))
	a := androidOf(t, n, false)
	if a.LocKey != "MESSAGE_TEXT" || !reflect.DeepEqual(a.LocArgs, []string{"Анна Каренина", "привет"}) ||
		!reflect.DeepEqual(a.Custom, map[string]int64{"from_id": anna, "msg_id": 5}) {
		t.Fatalf("android: %+v", a)
	}

	p, m := iosOf(t, n, "ru", HideNone)
	title, body := alertOf(m)
	if title != "Анна Каренина" || body != "привет" || m["from_id"] != "7" || m["msg_id"] != "5" || m["loc-key"] != nil {
		t.Fatalf("ios: %s", p.Payload)
	}
	aps := m["aps"].(map[string]any)
	if aps["sound"] != "default" || aps["category"] != "r" || aps["thread-id"] != "7" || !p.Alert || !p.Sound || p.Placeholder != "Новое сообщение" {
		t.Fatalf("ios aps: %s %+v", p.Payload, p)
	}
	if strings.Contains(p.Placeholder, "привет") {
		t.Fatal("the placeholder tells the text")
	}
}

func TestGroupText(t *testing.T) {
	n := one(t, updates(message(9, inGroup, from(boris), text("едем?"))))
	a := androidOf(t, n, false)
	if a.LocKey != "CHAT_MESSAGE_TEXT" || !reflect.DeepEqual(a.LocArgs, []string{"Борис", "Дача", "едем?"}) ||
		!reflect.DeepEqual(a.Custom, map[string]int64{"chat_id": group, "chat_from_id": boris, "msg_id": 9}) {
		t.Fatalf("android: %+v", a)
	}
	_, m := iosOf(t, n, "ru", HideNone)
	title, body := alertOf(m)
	// Only chat_id: with from_id iOS would put it into the private chat.
	if title != "Дача" || body != "Борис: едем?" || m["chat_id"] != "100" || m["from_id"] != nil ||
		m["aps"].(map[string]any)["thread-id"] != "-100" {
		t.Fatalf("ios: %v", m)
	}
}

func TestNoNotification(t *testing.T) {
	out := message(1, text("я пишу"))
	out.Out = true
	for name, ups := range map[string]*mtproto.Updates{
		"outgoing":       updates(out),
		"own":            updates(message(2, inGroup, from(me), text("я"))),
		"private action": updates(service(3, mtproto.MakeTLMessageActionChatEditPhoto(nil).To_MessageAction(), func(m *mtproto.Message) { m.PeerId = mtproto.MakePeerUser(anna) })),
		"channel":        updates(message(4, func(m *mtproto.Message) { m.PeerId = mtproto.MakePeerChannel(5) })),
		"other update":   mtproto.MakeTLUpdates(&mtproto.Updates{Updates: []*mtproto.Update{mtproto.MakeTLUpdateUserTyping(nil).To_Update()}}).To_Updates(),
	} {
		if ns := FromUpdates(me, ups); len(ns) != 0 {
			t.Errorf("%s: %+v", name, ns)
		}
	}
}

func TestContents(t *testing.T) {
	sticker := mtproto.MakeTLDocumentAttributeSticker(&mtproto.DocumentAttribute{Alt: "😀"}).To_DocumentAttribute()
	gif := mtproto.MakeTLDocumentAttributeAnimated(nil).To_DocumentAttribute()
	video := mtproto.MakeTLDocumentAttributeVideo(&mtproto.DocumentAttribute{}).To_DocumentAttribute()
	round := mtproto.MakeTLDocumentAttributeVideo(&mtproto.DocumentAttribute{RoundMessage: true}).To_DocumentAttribute()
	voice := mtproto.MakeTLDocumentAttributeAudio(&mtproto.DocumentAttribute{Voice: true}).To_DocumentAttribute()
	music := mtproto.MakeTLDocumentAttributeAudio(&mtproto.DocumentAttribute{}).To_DocumentAttribute()
	poll := func(quiz bool) *mtproto.MessageMedia {
		return mtproto.MakeTLMessageMediaPoll(&mtproto.MessageMedia{Poll: mtproto.MakeTLPoll(&mtproto.Poll{
			Question_TEXTWITHENTITIES: mtproto.MakeTLTextWithEntities(&mtproto.TextWithEntities{Text: "Куда?"}).To_TextWithEntities(),
			Quiz:                      quiz,
		}).To_Poll()}).To_MessageMedia()
	}
	for _, tc := range []struct {
		media *mtproto.MessageMedia
		key   string
		args  []string
		ios   string
	}{
		{mtproto.MakeTLMessageMediaPhoto(&mtproto.MessageMedia{}).To_MessageMedia(), "MESSAGE_PHOTO", nil, "📷 Фото"},
		{document(sticker), "MESSAGE_STICKER", []string{"😀"}, "😀 Стикер"},
		{document(gif, video), "MESSAGE_GIF", nil, "GIF"},
		{document(video), "MESSAGE_VIDEO", nil, "📹 Видео"},
		{document(round), "MESSAGE_ROUND", nil, "📹 Видеосообщение"},
		{document(voice), "MESSAGE_AUDIO", nil, "🎤 Голосовое сообщение"},
		{document(music), "MESSAGE_DOC", nil, "📎 Файл"},
		{document(), "MESSAGE_DOC", nil, "📎 Файл"},
		{mtproto.MakeTLMessageMediaGeo(&mtproto.MessageMedia{}).To_MessageMedia(), "MESSAGE_GEO", nil, "📍 Геопозиция"},
		{mtproto.MakeTLMessageMediaGeoLive(&mtproto.MessageMedia{}).To_MessageMedia(), "MESSAGE_GEOLIVE", nil, "📍 Трансляция геопозиции"},
		{mtproto.MakeTLMessageMediaContact(&mtproto.MessageMedia{FirstName: "Иван", LastName: "Петров"}).To_MessageMedia(), "MESSAGE_CONTACT", []string{"Иван Петров"}, "👤 Контакт: Иван Петров"},
		{poll(false), "MESSAGE_POLL", []string{"Куда?"}, "📊 Куда?"},
		{poll(true), "MESSAGE_QUIZ", []string{"Куда?"}, "❓ Куда?"},
		{mtproto.MakeTLMessageMediaDice(&mtproto.MessageMedia{Emoticon: "🎲"}).To_MessageMedia(), "MESSAGE_TEXT", []string{"🎲"}, "🎲"},
	} {
		n := one(t, updates(message(1, media(tc.media))))
		a := androidOf(t, n, false)
		if want := append([]string{"Анна Каренина"}, tc.args...); a.LocKey != tc.key || !reflect.DeepEqual(a.LocArgs, want) {
			t.Errorf("%s: android %s %q, want %s %q", tc.key, a.LocKey, a.LocArgs, tc.key, want)
		}
		if _, m := iosOf(t, n, "ru", HideNone); func() string { _, b := alertOf(m); return b }() != tc.ios {
			t.Errorf("%s: ios %v, want %q", tc.key, m["aps"], tc.ios)
		}
	}

	// A photo with a caption.
	n := one(t, updates(message(1, media(mtproto.MakeTLMessageMediaPhoto(&mtproto.MessageMedia{}).To_MessageMedia()), text("море"))))
	if _, m := iosOf(t, n, "en", HideNone); func() string { _, b := alertOf(m); return b }() != "📷 Photo, море" {
		t.Errorf("caption: %v", m["aps"])
	}
	if _, m := iosOf(t, n, "en", HideNone); m["aps"].(map[string]any)["category"] != "m" {
		t.Errorf("media category: %v", m["aps"])
	}
}

func TestAlbumAndForwards(t *testing.T) {
	photo := media(mtproto.MakeTLMessageMediaPhoto(&mtproto.MessageMedia{}).To_MessageMedia())
	n := one(t, updates(message(1, photo, album(77)), message(2, photo, album(77)), message(3, photo, album(77))))
	if a := androidOf(t, n, false); a.LocKey != "MESSAGES" || !reflect.DeepEqual(a.LocArgs, []string{"Анна Каренина"}) || a.Custom["msg_id"] != 1 || n.Count != 3 {
		t.Fatalf("album: %+v %d", a, n.Count)
	}
	n = one(t, updates(message(1, inGroup, photo, album(77)), message(2, inGroup, photo, album(77))))
	if a := androidOf(t, n, false); a.LocKey != "CHAT_MESSAGES" || !reflect.DeepEqual(a.LocArgs, []string{"Анна Каренина", "Дача"}) {
		t.Fatalf("group album: %+v", a)
	}
	// Two albums are two notifications.
	if ns := FromUpdates(me, updates(message(1, photo, album(1)), message(2, photo, album(2)))); len(ns) != 2 {
		t.Fatalf("two albums: %d", len(ns))
	}

	n = one(t, updates(message(1, text("a"), forwarded), message(2, text("b"), forwarded)))
	if a := androidOf(t, n, false); a.LocKey != "MESSAGE_FWDS" || !reflect.DeepEqual(a.LocArgs, []string{"Анна Каренина", "2"}) {
		t.Fatalf("forwards: %+v", a)
	}
	if _, m := iosOf(t, n, "ru", HideNone); func() string { _, b := alertOf(m); return b }() != "Пересланные сообщения: 2" {
		t.Fatalf("ios forwards: %v", m["aps"])
	}
}

func TestGroupActions(t *testing.T) {
	act := func(a *mtproto.MessageAction) *mtproto.Updates { return updates(service(11, a)) }
	for _, tc := range []struct {
		ups  *mtproto.Updates
		key  string
		args []string
		ios  string
	}{
		{act(mtproto.MakeTLMessageActionChatCreate(&mtproto.MessageAction{Title_STRING: "Дача", Users: []int64{anna, me}}).To_MessageAction()),
			"CHAT_CREATED", []string{"Анна Каренина", "Дача"}, "Анна Каренина пригласил(а) вас в группу"},
		{act(mtproto.MakeTLMessageActionChatAddUser(&mtproto.MessageAction{Users: []int64{me}}).To_MessageAction()),
			"CHAT_ADD_YOU", []string{"Анна Каренина", "Дача"}, "Анна Каренина пригласил(а) вас в группу"},
		{act(mtproto.MakeTLMessageActionChatAddUser(&mtproto.MessageAction{Users: []int64{boris}}).To_MessageAction()),
			"CHAT_ADD_MEMBER", []string{"Анна Каренина", "Дача", "Борис"}, "Анна Каренина добавил(а) Борис"},
		{act(mtproto.MakeTLMessageActionChatAddUser(&mtproto.MessageAction{Users: []int64{anna}}).To_MessageAction()),
			"CHAT_RETURNED", []string{"Анна Каренина", "Дача"}, "Анна Каренина вернулся(-ась) в группу"},
		{act(mtproto.MakeTLMessageActionChatDeleteUser(&mtproto.MessageAction{UserId: me}).To_MessageAction()),
			"CHAT_DELETE_YOU", []string{"Анна Каренина", "Дача"}, "Анна Каренина удалил(а) вас из группы"},
		{act(mtproto.MakeTLMessageActionChatDeleteUser(&mtproto.MessageAction{UserId: boris}).To_MessageAction()),
			"CHAT_DELETE_MEMBER", []string{"Анна Каренина", "Дача", "Борис"}, "Анна Каренина удалил(а) Борис"},
		{act(mtproto.MakeTLMessageActionChatDeleteUser(&mtproto.MessageAction{UserId: anna}).To_MessageAction()),
			"CHAT_LEFT", []string{"Анна Каренина", "Дача"}, "Анна Каренина покинул(а) группу"},
		{act(mtproto.MakeTLMessageActionChatJoinedByLink(&mtproto.MessageAction{}).To_MessageAction()),
			"CHAT_JOINED", []string{"Анна Каренина", "Дача"}, "Анна Каренина присоединился(-ась) к группе"},
		{act(mtproto.MakeTLMessageActionChatEditTitle(&mtproto.MessageAction{Title_STRING: "Дача 2"}).To_MessageAction()),
			"CHAT_TITLE_EDITED", []string{"Анна Каренина", "Дача 2"}, "Анна Каренина изменил(а) название группы на «Дача 2»"},
		{act(mtproto.MakeTLMessageActionChatEditPhoto(&mtproto.MessageAction{}).To_MessageAction()),
			"CHAT_PHOTO_EDITED", []string{"Анна Каренина", "Дача"}, "Анна Каренина изменил(а) фото группы"},
	} {
		n := one(t, tc.ups)
		if a := androidOf(t, n, false); a.LocKey != tc.key || !reflect.DeepEqual(a.LocArgs, tc.args) || a.Custom["chat_id"] != group {
			t.Errorf("%s: android %+v", tc.key, a)
		}
		if _, m := iosOf(t, n, "ru", HideNone); func() string { _, b := alertOf(m); return b }() != tc.ios {
			t.Errorf("%s: ios %v", tc.key, m["aps"])
		}
	}
}

func TestPinned(t *testing.T) {
	pin := mtproto.MakeTLMessageActionPinMessage(nil).To_MessageAction()
	reply := func(m *mtproto.Message) {
		m.ReplyTo = mtproto.MakeTLMessageReplyHeader(&mtproto.MessageReplyHeader{ReplyToMsgId: 3}).To_MessageReplyHeader()
	}
	private := func(m *mtproto.Message) { m.PeerId = mtproto.MakePeerUser(anna) }

	n := one(t, updates(service(12, pin, private, reply)))
	if n.Kind != KindPinned || n.PinnedId != 3 || !n.NeedsPinned() {
		t.Fatalf("pinned: %+v", n)
	}
	// Before the lookup, or when it fails: no text.
	if a := androidOf(t, n, false); a.LocKey != "PINNED_NOTEXT" || !reflect.DeepEqual(a.LocArgs, []string{"Анна Каренина", ""}) {
		t.Fatalf("pinned, no text: %+v", a)
	}
	n.SetPinned(message(3, text("встреча в 7")))
	if a := androidOf(t, n, false); a.LocKey != "PINNED_TEXT" || !reflect.DeepEqual(a.LocArgs, []string{"Анна Каренина", "встреча в 7"}) {
		t.Fatalf("pinned text: %+v", a)
	}
	if _, m := iosOf(t, n, "ru", HideNone); func() string { _, b := alertOf(m); return b }() != "Анна Каренина закрепил(а) «встреча в 7»" {
		t.Fatalf("ios pinned: %v", m["aps"])
	}

	n = one(t, updates(service(13, pin, reply)))
	n.SetPinned(message(3, media(mtproto.MakeTLMessageMediaPhoto(&mtproto.MessageMedia{}).To_MessageMedia())))
	if a := androidOf(t, n, false); a.LocKey != "PINNED_PHOTO" || !reflect.DeepEqual(a.LocArgs, []string{"Анна Каренина", "Дача"}) || a.Custom["chat_from_id"] != anna {
		t.Fatalf("group pinned photo: %+v", a)
	}
}

func TestReadHistory(t *testing.T) {
	u := mtproto.MakeTLUpdateReadHistoryInbox(&mtproto.Update{Peer_PEER: mtproto.MakePeerChat(group), MaxId: 40}).To_Update()
	n := one(t, mtproto.MakeTLUpdates(&mtproto.Updates{Updates: []*mtproto.Update{u}}).To_Updates())
	if a := androidOf(t, n, false); a.LocKey != "READ_HISTORY" || len(a.LocArgs) != 0 || !reflect.DeepEqual(a.Custom, map[string]int64{"chat_id": group, "max_id": 40}) {
		t.Fatalf("android: %+v", a)
	}
	p, m := iosOf(t, n, "ru", HideNone)
	if p.Alert || m["loc-key"] != "READ_HISTORY" || m["max_id"] != "40" || m["chat_id"] != "100" || m["aps"] != nil {
		t.Fatalf("ios: %s", p.Payload)
	}
}

func TestHidingAndFlags(t *testing.T) {
	m := message(5, text("секрет"))
	m.Silent, m.Mentioned = true, true
	n := one(t, updates(m))
	if a := androidOf(t, n, true); a.LocKey != "MESSAGE_NOTEXT" || !reflect.DeepEqual(a.LocArgs, []string{"Анна Каренина"}) || a.Custom["silent"] != 1 || a.Custom["mention"] != 1 {
		t.Fatalf("android hidden: %+v", a)
	}
	p, ios := iosOf(t, n, "en", HideText)
	if title, body := alertOf(ios); title != "Анна Каренина" || body != "New message" || ios["silent"] != "1" || p.Sound || strings.Contains(string(p.Payload), "секрет") {
		t.Fatalf("ios hidden text: %s", p.Payload)
	}
	if _, ios = iosOf(t, n, "ru", HideAll); func() bool { t, b := alertOf(ios); return t != "" || b != "Новое сообщение" }() {
		t.Fatalf("ios locked: %v", ios["aps"])
	}
	if _, ok := ios["aps"].(map[string]any)["sound"]; ok {
		t.Fatal("a silent message plays a sound")
	}
}

func TestServiceKinds(t *testing.T) {
	muted := &Notification{Kind: KindMuted, UserId: me, PeerId: anna, MsgId: 5}
	if a := androidOf(t, muted, false); a.LocKey != "MESSAGE_MUTED" || a.Custom["from_id"] != anna {
		t.Fatalf("muted: %+v", a)
	}
	if p, err := IOS(muted, "ru", HideNone); err != nil || p != nil {
		t.Fatalf("muted on iOS: %v %v", p, err)
	}
	revoke := &Notification{Kind: KindSessionRevoke, UserId: me}
	if a := androidOf(t, revoke, false); a.LocKey != "SESSION_REVOKE" || len(a.Custom) != 0 {
		t.Fatalf("revoke: %+v", a)
	}
	if p, m := iosOf(t, revoke, "ru", HideNone); p.Alert || m["loc-key"] != "SESSION_REVOKE" {
		t.Fatalf("ios revoke: %s", p.Payload)
	}
}

func TestLongText(t *testing.T) {
	n := one(t, updates(message(1, text(strings.Repeat("я", 3000)))))
	if utf8.RuneCountInString(n.Text) != MaxTextLength || !strings.HasSuffix(n.Text, "…") {
		t.Fatalf("text of %d runes", utf8.RuneCountInString(n.Text))
	}
}
