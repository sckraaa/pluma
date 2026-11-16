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
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"unicode/utf16"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/translation/internal/config"
	"github.com/teamgram/teamgram-server/app/bff/translation/internal/svc"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	dialogpb "github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	messagepb "github.com/teamgram/teamgram-server/app/service/biz/message/message"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/translate"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const (
	me          = int64(1001)
	friend      = int64(1002)
	stranger    = int64(1003)
	groupId     = int64(501)
	channelId   = int64(77)
	channelHash = int64(4242)
)

// upperEngine answers a plain prompt of the hy-mt1.5 profile with the upper
// case of its text. A tagged prompt gets the same, without <target>, so
// such an item goes by pieces and keeps only its verbatim entities.
type upperEngine struct{ calls atomic.Int32 }

func (e *upperEngine) Translate(_ context.Context, req translate.Request) (string, error) {
	e.calls.Add(1)
	return strings.ToUpper(req.Prompt[strings.Index(req.Prompt, "\n\n")+2:]), nil
}

type memCache struct {
	mu   sync.Mutex
	rows map[translate.Key]*translate.CachedTranslation
}

func (c *memCache) CachedTranslations(_ context.Context, keys []translate.Key) (map[translate.Key]*translate.CachedTranslation, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	found := make(map[translate.Key]*translate.CachedTranslation)
	for _, k := range keys {
		if t, ok := c.rows[k]; ok {
			found[k] = t
		}
	}
	return found, nil
}

func (c *memCache) PutTranslation(_ context.Context, key translate.Key, _, _ string, t *translate.CachedTranslation) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rows[key] = t
	return nil
}

type fakeMessages struct {
	boxes map[int32]*mtproto.MessageBox
	err   error
	asked [][]int32
}

func (f *fakeMessages) MessageGetUserMessageList(_ context.Context, in *messagepb.TLMessageGetUserMessageList) (*messagepb.Vector_MessageBox, error) {
	f.asked = append(f.asked, in.GetIdList())
	if f.err != nil {
		return nil, f.err
	}
	out := &messagepb.Vector_MessageBox{}
	for _, id := range in.GetIdList() {
		if b, ok := f.boxes[id]; ok && in.GetUserId() == b.GetUserId() {
			out.Datas = append(out.Datas, b)
		}
	}
	return out, nil
}

type fakeChannels struct {
	mc       *channelpb.MutableChannel
	messages map[int32]*mtproto.Message
	getErr   error
}

func (f *fakeChannels) ChannelGetMutableChannel(_ context.Context, in *channelpb.TLChannelGetMutableChannel) (*channelpb.MutableChannel, error) {
	if f.mc == nil || f.mc.GetChannel().GetId() != in.GetChannelId() {
		return nil, mtproto.ErrChannelInvalid
	}
	return f.mc, nil
}

func (f *fakeChannels) ChannelGetMessages(_ context.Context, in *channelpb.TLChannelGetMessages) (*channelpb.Vector_Message, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	out := &channelpb.Vector_Message{}
	for _, id := range in.GetId() {
		if m, ok := f.messages[id]; ok {
			out.Datas = append(out.Datas, m)
		}
	}
	return out, nil
}

type fakeDialogs struct {
	have map[translate.TranslationPeer]bool
}

func (f *fakeDialogs) DialogGetDialogById(_ context.Context, in *dialogpb.TLDialogGetDialogById) (*dialogpb.DialogExt, error) {
	if !f.have[translate.TranslationPeer{Type: in.GetPeerType(), Id: in.GetPeerId()}] {
		// As the dialog service answers a dialog it does not have.
		return nil, mtproto.ErrPeerIdInvalid
	}
	return &dialogpb.DialogExt{}, nil
}

type fakeUsers struct{ exist []int64 }

func (f *fakeUsers) UserGetUserDataListByIdList(_ context.Context, in *userpb.TLUserGetUserDataListByIdList) (*userpb.Vector_UserData, error) {
	out := &userpb.Vector_UserData{}
	for _, id := range in.GetUserIdList() {
		if slices.Contains(f.exist, id) {
			out.Datas = append(out.Datas, &mtproto.UserData{Id: id})
		}
	}
	return out, nil
}

type setting struct {
	peer     translate.TranslationPeer
	disabled bool
}

type fakeSettings struct {
	set []setting
	err error
}

func (f *fakeSettings) SetTranslationsDisabled(_ context.Context, userId int64, peer translate.TranslationPeer, disabled bool) error {
	if f.err != nil {
		return f.err
	}
	if userId != me {
		return errors.New("another user")
	}
	f.set = append(f.set, setting{peer, disabled})
	return nil
}

type fixture struct {
	svc      *svc.ServiceContext
	engine   *upperEngine
	messages *fakeMessages
	channels *fakeChannels
	settings *fakeSettings
}

// newFixture is the translation with an engine (on) or without one.
func newFixture(t *testing.T, on bool) *fixture {
	t.Helper()
	c := config.Config{Translate: translate.Config{URL: "http://translate:8080", Model: "hy-mt"}}
	if !on {
		c.Translate.URL = ""
	}
	c.Translate = c.Translate.WithDefaults()
	f := &fixture{
		engine:   &upperEngine{},
		messages: &fakeMessages{boxes: map[int32]*mtproto.MessageBox{}},
		channels: &fakeChannels{},
		settings: &fakeSettings{},
	}
	f.svc = &svc.ServiceContext{
		Config:   c,
		Settings: f.settings,
		Messages: f.messages,
		Channels: f.channels,
		Dialogs: &fakeDialogs{have: map[translate.TranslationPeer]bool{
			{Type: mtproto.PEER_USER, Id: friend}:  true,
			{Type: mtproto.PEER_CHAT, Id: groupId}: true,
		}},
		Users: &fakeUsers{exist: []int64{me, friend}},
	}
	if on {
		profile, err := translate.NewProfile(c.Translate.Profile, c.Translate.SlotContext())
		if err != nil {
			t.Fatal(err)
		}
		s, err := translate.StartScheduler(c.Translate, profile, f.engine, &memCache{rows: map[translate.Key]*translate.CachedTranslation{}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(s.Stop)
		f.svc.Profile, f.svc.Translator = profile, s
	}
	return f
}

func (f *fixture) core(bot bool) *TranslationCore {
	return &TranslationCore{
		ctx:    context.Background(),
		svcCtx: f.svc,
		Logger: logx.WithContext(context.Background()),
		MD:     &metadata.RpcMetadata{UserId: me, IsBot: bot},
	}
}

func text(s string, ents ...*mtproto.MessageEntity) *mtproto.TextWithEntities {
	return mtproto.MakeTLTextWithEntities(&mtproto.TextWithEntities{Text: s, Entities: ents}).To_TextWithEntities()
}

func textMessage(id int32, s string, ents ...*mtproto.MessageEntity) *mtproto.Message {
	return mtproto.MakeTLMessage(&mtproto.Message{Id: id, Message: s, Entities: ents}).To_Message()
}

// box is the caller's copy of message id in the dialog of peerType/peerId
// as msg stores it: a private message names the receiver in the
// receiver's copy (dialogOf).
func box(id int32, peerType int32, peerId, sender int64, m *mtproto.Message) *mtproto.MessageBox {
	return mtproto.MakeTLMessageBox(&mtproto.MessageBox{UserId: me, MessageId: id, SenderUserId: sender, PeerType: peerType, PeerId: peerId, Message: m}).To_MessageBox()
}

func texts(out []*mtproto.TextWithEntities) []string {
	s := make([]string, len(out))
	for i, t := range out {
		s[i] = t.GetText()
	}
	return s
}

func inputUser(id int64) *mtproto.InputPeer {
	return mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: id, AccessHash: id + 1}).To_InputPeer()
}

func inputChannel(hash int64) *mtproto.InputPeer {
	return mtproto.MakeTLInputPeerChannel(&mtproto.InputPeer{ChannelId: channelId, AccessHash: hash}).To_InputPeer()
}

func wantError(t *testing.T, what string, err, want error) {
	t.Helper()
	if !isError(err, want) {
		t.Errorf("%s: %v, want %v", what, err, want)
	}
}

// One item for each one asked, in its order; an empty text is an empty
// item, translated by nobody.
func TestTranslateTextByTexts(t *testing.T) {
	f := newFixture(t, true)
	res, err := f.core(false).MessagesTranslateText(&mtproto.TLMessagesTranslateText{
		Text_FLAGVECTORTEXTWITHENTITIES: []*mtproto.TextWithEntities{text("hello"), text(""), text("good morning")},
		ToLang:                          "pt-BR",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := texts(res.GetResult()); !slices.Equal(got, []string{"HELLO", "", "GOOD MORNING"}) {
		t.Fatalf("result %q", got)
	}
	if res.GetPredicateName() != mtproto.Predicate_messages_translateResult {
		t.Fatalf("answer %s", res.GetPredicateName())
	}
	if n := f.engine.calls.Load(); n != 2 {
		t.Fatalf("%d calls of the engine, want 2", n)
	}
}

// The messages of a private chat by id, in the order asked: a photo
// without a caption, a message of another dialog of the caller and a
// message that is not there are empty items, and the text of the other
// dialog is never given.
func TestTranslateTextByMessages(t *testing.T) {
	f := newFixture(t, true)
	photo := mtproto.MakeTLMessage(&mtproto.Message{Id: 2, Media: mtproto.MakeTLMessageMediaPhoto(&mtproto.MessageMedia{}).To_MessageMedia()}).To_Message()
	f.messages.boxes[1] = box(1, mtproto.PEER_USER, me, friend, textMessage(1, "first"))
	f.messages.boxes[2] = box(2, mtproto.PEER_USER, friend, me, photo)
	f.messages.boxes[3] = box(3, mtproto.PEER_USER, me, stranger, textMessage(3, "secret of another chat"))
	f.messages.boxes[5] = box(5, mtproto.PEER_USER, friend, me, textMessage(5, "mine"))

	res, err := f.core(false).MessagesTranslateText(&mtproto.TLMessagesTranslateText{
		Peer:   inputUser(friend),
		Id:     []int32{5, 3, 1, 4, 2, 1},
		ToLang: "en",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := texts(res.GetResult()); !slices.Equal(got, []string{"MINE", "", "FIRST", "", "", "FIRST"}) {
		t.Fatalf("result %q", got)
	}
	// The ids are read once each, in one call.
	if len(f.messages.asked) != 1 || !slices.Equal(f.messages.asked[0], []int32{1, 2, 3, 4, 5}) {
		t.Fatalf("read %v", f.messages.asked)
	}
}

// The entities of a stored message come back in place: a mention and code
// are kept as they were.
func TestTranslateTextKeepsVerbatimEntities(t *testing.T) {
	f := newFixture(t, true)
	mention := mtproto.MakeTLMessageEntityMentionName(&mtproto.MessageEntity{Offset: 6, Length: 4, UserId_INT64: friend}).To_MessageEntity()
	code := mtproto.MakeTLMessageEntityCode(&mtproto.MessageEntity{Offset: 16, Length: 6}).To_MessageEntity()
	f.messages.boxes[7] = box(7, mtproto.PEER_CHAT, groupId, friend, textMessage(7, "hello Anna, run ls -la now", mention, code))

	res, err := f.core(false).MessagesTranslateText(&mtproto.TLMessagesTranslateText{
		Peer:   mtproto.MakeTLInputPeerChat(&mtproto.InputPeer{ChatId: groupId}).To_InputPeer(),
		Id:     []int32{7},
		ToLang: "en",
	})
	if err != nil {
		t.Fatal(err)
	}
	out := res.GetResult()[0]
	got := map[string]string{}
	units := utf16Of(out.GetText())
	for _, e := range out.GetEntities() {
		got[e.GetPredicateName()] = string(utf16Decode(units[e.GetOffset() : e.GetOffset()+e.GetLength()]))
	}
	if got[mtproto.Predicate_messageEntityMentionName] != "Anna" || got[mtproto.Predicate_messageEntityCode] != "ls -la" {
		t.Fatalf("translation %q with entities %v", out.GetText(), got)
	}
}

// A mention a client typed by input user names its user by id in the
// translation; a mention of a user that does not exist is dropped.
func TestTranslateTextResolvesInputMentions(t *testing.T) {
	f := newFixture(t, true)
	in := text("ask Anna or Bob",
		mtproto.MakeTLInputMessageEntityMentionName(&mtproto.MessageEntity{Offset: 4, Length: 4,
			UserId_INPUTUSER: mtproto.MakeTLInputUser(&mtproto.InputUser{UserId: friend, AccessHash: 1}).To_InputUser()}).To_MessageEntity(),
		mtproto.MakeTLInputMessageEntityMentionName(&mtproto.MessageEntity{Offset: 12, Length: 3,
			UserId_INPUTUSER: mtproto.MakeTLInputUser(&mtproto.InputUser{UserId: 999999, AccessHash: 1}).To_InputUser()}).To_MessageEntity(),
	)
	res, err := f.core(false).MessagesTranslateText(&mtproto.TLMessagesTranslateText{
		Text_FLAGVECTORTEXTWITHENTITIES: []*mtproto.TextWithEntities{in},
		ToLang:                          "en",
	})
	if err != nil {
		t.Fatal(err)
	}
	ents := res.GetResult()[0].GetEntities()
	if len(ents) != 1 || ents[0].GetPredicateName() != mtproto.Predicate_messageEntityMentionName || ents[0].GetUserId_INT64() != friend {
		t.Fatalf("entities %v", ents)
	}
}

// A post of a channel the caller reads; a channel it cannot read, or one
// named with another access hash, is PEER_ID_INVALID.
func TestTranslateTextChannel(t *testing.T) {
	f := newFixture(t, true)
	f.channels.mc = &channelpb.MutableChannel{
		Channel:      &channelpb.ImmutableChannel{Id: channelId, AccessHash: channelHash, Broadcast: true},
		Participants: []*channelpb.ImmutableChannelParticipant{{ChannelId: channelId, UserId: me, State: channelpb.StateIn}},
	}
	f.channels.messages = map[int32]*mtproto.Message{10: textMessage(10, "news of the day")}
	c := f.core(false)
	res, err := c.MessagesTranslateText(&mtproto.TLMessagesTranslateText{Peer: inputChannel(channelHash), Id: []int32{10, 11}, ToLang: "en"})
	if err != nil {
		t.Fatal(err)
	}
	if got := texts(res.GetResult()); !slices.Equal(got, []string{"NEWS OF THE DAY", ""}) {
		t.Fatalf("result %q", got)
	}

	_, err = c.MessagesTranslateText(&mtproto.TLMessagesTranslateText{Peer: inputChannel(channelHash + 1), Id: []int32{10}, ToLang: "en"})
	wantError(t, "another access hash", err, mtproto.ErrPeerIdInvalid)

	f.channels.mc.Participants = nil // a private channel the caller is not in
	_, err = c.MessagesTranslateText(&mtproto.TLMessagesTranslateText{Peer: inputChannel(channelHash), Id: []int32{10}, ToLang: "en"})
	wantError(t, "a private channel", err, mtproto.ErrPeerIdInvalid)

	f.channels.mc.Participants = []*channelpb.ImmutableChannelParticipant{{ChannelId: channelId, UserId: me, State: channelpb.StateIn}}
	f.channels.getErr = mtproto.ErrChannelPrivate
	_, err = c.MessagesTranslateText(&mtproto.TLMessagesTranslateText{Peer: inputChannel(channelHash), Id: []int32{10}, ToLang: "en"})
	wantError(t, "channel.getMessages refuses", err, mtproto.ErrPeerIdInvalid)
}

// The errors of the table of brief 101, "Пределы и ошибки".
func TestTranslateTextErrors(t *testing.T) {
	f := newFixture(t, true)
	long := strings.Repeat("a", translate.DefaultMaxItemLength+1)
	many := make([]int32, translate.DefaultMaxItems+1)
	for i := range many {
		many[i] = int32(i + 1)
	}
	for _, tc := range []struct {
		name string
		bot  bool
		in   *mtproto.TLMessagesTranslateText
		want error
	}{
		{"a bot", true, &mtproto.TLMessagesTranslateText{Text_FLAGVECTORTEXTWITHENTITIES: []*mtproto.TextWithEntities{text("hi")}, ToLang: "en"}, mtproto.ErrBotMethodInvalid},
		{"neither peer nor text", false, &mtproto.TLMessagesTranslateText{ToLang: "en"}, mtproto.ErrInputTextEmpty},
		{"a peer without ids", false, &mtproto.TLMessagesTranslateText{Peer: inputUser(friend), ToLang: "en"}, mtproto.ErrMsgIdInvalid},
		{"no dialog", false, &mtproto.TLMessagesTranslateText{Peer: inputUser(stranger), Id: []int32{1}, ToLang: "en"}, mtproto.ErrPeerIdInvalid},
		{"a group the caller is not in", false, &mtproto.TLMessagesTranslateText{Peer: mtproto.MakeTLInputPeerChat(&mtproto.InputPeer{ChatId: groupId + 1}).To_InputPeer(), Id: []int32{1}, ToLang: "en"}, mtproto.ErrPeerIdInvalid},
		{"an unknown language", false, &mtproto.TLMessagesTranslateText{Text_FLAGVECTORTEXTWITHENTITIES: []*mtproto.TextWithEntities{text("hi")}, ToLang: "xx"}, translate.ErrToLangInvalid},
		{"Norwegian, which the model lacks", false, &mtproto.TLMessagesTranslateText{Text_FLAGVECTORTEXTWITHENTITIES: []*mtproto.TextWithEntities{text("hi")}, ToLang: "nb"}, translate.ErrToLangInvalid},
		{"an item too long", false, &mtproto.TLMessagesTranslateText{Text_FLAGVECTORTEXTWITHENTITIES: []*mtproto.TextWithEntities{text(long)}, ToLang: "en"}, translate.ErrInputTextTooLong},
		{"too many ids", false, &mtproto.TLMessagesTranslateText{Peer: inputUser(friend), Id: many, ToLang: "en"}, translate.ErrInputTextTooLong},
	} {
		_, err := f.core(tc.bot).MessagesTranslateText(tc.in)
		wantError(t, tc.name, err, tc.want)
	}
	if n := f.engine.calls.Load(); n != 0 {
		t.Fatalf("%d calls of the engine for refused requests", n)
	}
}

// A service that fails on the way is FLOOD_WAIT_X, never 500.
func TestTranslateTextReadFailure(t *testing.T) {
	f := newFixture(t, true)
	f.messages.err = errors.New("message service down")
	_, err := f.core(false).MessagesTranslateText(&mtproto.TLMessagesTranslateText{Peer: inputUser(friend), Id: []int32{1}, ToLang: "en"})
	wantError(t, "a failed read", err, translate.FloodWait(internalWait))
}

// Without an engine nothing is translated, as before the task.
func TestTranslationOff(t *testing.T) {
	f := newFixture(t, false)
	c := f.core(false)
	_, err := c.MessagesTranslateText(&mtproto.TLMessagesTranslateText{Text_FLAGVECTORTEXTWITHENTITIES: []*mtproto.TextWithEntities{text("hi")}, ToLang: "en"})
	wantError(t, "translateText", err, mtproto.ErrEnterpriseIsBlocked)
	_, err = c.MessagesComposeMessageWithAI(&mtproto.TLMessagesComposeMessageWithAI{Text: text("hi"), TranslateToLang: wrapperspb.String("en")})
	wantError(t, "composeMessageWithAI", err, mtproto.ErrEnterpriseIsBlocked)
	// The bar of a chat is the reader's choice, kept without an engine.
	if _, err = c.MessagesTogglePeerTranslations(&mtproto.TLMessagesTogglePeerTranslations{Disabled: true, Peer: inputUser(friend)}); err != nil {
		t.Fatalf("togglePeerTranslations: %v", err)
	}
}

// The translation form of the iOS screen is served; every form of the AI
// editor is refused with ERR_ENTERPRISE_IS_BLOCKED.
func TestComposeMessageWithAI(t *testing.T) {
	f := newFixture(t, true)
	c := f.core(false)
	res, err := c.MessagesComposeMessageWithAI(&mtproto.TLMessagesComposeMessageWithAI{Text: text("good night"), TranslateToLang: wrapperspb.String("de")})
	if err != nil {
		t.Fatal(err)
	}
	if res.GetResultText().GetText() != "GOOD NIGHT" || res.GetDiffText() != nil {
		t.Fatalf("answer %v", res)
	}
	tone := mtproto.MakeTLInputAiComposeToneDefault(&mtproto.InputAiComposeTone{}).To_InputAiComposeTone()
	for _, tc := range []struct {
		name string
		in   *mtproto.TLMessagesComposeMessageWithAI
	}{
		{"proofread", &mtproto.TLMessagesComposeMessageWithAI{Proofread: true, Text: text("hi")}},
		{"emojify", &mtproto.TLMessagesComposeMessageWithAI{Emojify: true, Text: text("hi"), TranslateToLang: wrapperspb.String("de")}},
		{"a tone", &mtproto.TLMessagesComposeMessageWithAI{Text: text("hi"), TranslateToLang: wrapperspb.String("de"), Tone: tone}},
		{"a tone of an older layer", &mtproto.TLMessagesComposeMessageWithAI{Text: text("hi"), ChangeTone: wrapperspb.String("formal")}},
		{"nothing asked", &mtproto.TLMessagesComposeMessageWithAI{Text: text("hi")}},
	} {
		_, err := c.MessagesComposeMessageWithAI(tc.in)
		wantError(t, tc.name, err, mtproto.ErrEnterpriseIsBlocked)
	}
	_, err = f.core(true).MessagesComposeMessageWithAI(&mtproto.TLMessagesComposeMessageWithAI{Text: text("hi"), TranslateToLang: wrapperspb.String("de")})
	wantError(t, "a bot", err, mtproto.ErrBotMethodInvalid)
	_, err = c.MessagesComposeMessageWithAI(&mtproto.TLMessagesComposeMessageWithAI{Text: text("hi"), TranslateToLang: wrapperspb.String("xx")})
	wantError(t, "an unknown language", err, translate.ErrToLangInvalid)
}

// The bar of a chat is hidden and shown again for a user, a group and a
// channel; a chat that is not the caller's is PEER_ID_INVALID.
func TestTogglePeerTranslations(t *testing.T) {
	f := newFixture(t, true)
	f.channels.mc = &channelpb.MutableChannel{Channel: &channelpb.ImmutableChannel{Id: channelId, AccessHash: channelHash, Broadcast: true, Username: "news"}}
	c := f.core(false)
	for _, tc := range []struct {
		peer     *mtproto.InputPeer
		disabled bool
		want     translate.TranslationPeer
	}{
		{inputUser(friend), true, translate.TranslationPeer{Type: mtproto.PEER_USER, Id: friend}},
		{mtproto.MakeTLInputPeerSelf(nil).To_InputPeer(), true, translate.TranslationPeer{Type: mtproto.PEER_USER, Id: me}},
		{mtproto.MakeTLInputPeerChat(&mtproto.InputPeer{ChatId: groupId}).To_InputPeer(), false, translate.TranslationPeer{Type: mtproto.PEER_CHAT, Id: groupId}},
		// A public channel the caller reads without joining.
		{inputChannel(channelHash), true, translate.TranslationPeer{Type: mtproto.PEER_CHANNEL, Id: channelId}},
	} {
		res, err := c.MessagesTogglePeerTranslations(&mtproto.TLMessagesTogglePeerTranslations{Disabled: tc.disabled, Peer: tc.peer})
		if err != nil || res.GetPredicateName() != mtproto.Predicate_boolTrue {
			t.Fatalf("%v: %v, %v", tc.want, res, err)
		}
		if last := f.settings.set[len(f.settings.set)-1]; last != (setting{tc.want, tc.disabled}) {
			t.Fatalf("stored %+v, want %v %v", last, tc.want, tc.disabled)
		}
	}
	stored := len(f.settings.set)

	_, err := c.MessagesTogglePeerTranslations(&mtproto.TLMessagesTogglePeerTranslations{Peer: inputUser(stranger)})
	wantError(t, "no dialog", err, mtproto.ErrPeerIdInvalid)
	_, err = c.MessagesTogglePeerTranslations(&mtproto.TLMessagesTogglePeerTranslations{Peer: inputChannel(channelHash + 1)})
	wantError(t, "another access hash", err, mtproto.ErrPeerIdInvalid)
	f.channels.mc.Channel.Username = ""
	_, err = c.MessagesTogglePeerTranslations(&mtproto.TLMessagesTogglePeerTranslations{Peer: inputChannel(channelHash)})
	wantError(t, "a private channel", err, mtproto.ErrPeerIdInvalid)
	f.channels.mc.Channel.Username, f.channels.mc.Channel.Deleted = "news", true
	_, err = c.MessagesTogglePeerTranslations(&mtproto.TLMessagesTogglePeerTranslations{Peer: inputChannel(channelHash)})
	wantError(t, "a deleted channel", err, mtproto.ErrPeerIdInvalid)
	_, err = c.MessagesTogglePeerTranslations(&mtproto.TLMessagesTogglePeerTranslations{Peer: mtproto.MakeTLInputPeerEmpty(nil).To_InputPeer()})
	wantError(t, "an empty peer", err, mtproto.ErrPeerIdInvalid)
	_, err = f.core(true).MessagesTogglePeerTranslations(&mtproto.TLMessagesTogglePeerTranslations{Peer: inputUser(friend)})
	wantError(t, "a bot", err, mtproto.ErrBotMethodInvalid)
	if len(f.settings.set) != stored {
		t.Fatalf("a refused toggle was stored: %v", f.settings.set[stored:])
	}

	// A failed write is FLOOD_WAIT_X, never 500.
	f.settings.err = errors.New("database down")
	_, err = c.MessagesTogglePeerTranslations(&mtproto.TLMessagesTogglePeerTranslations{Peer: inputUser(friend)})
	wantError(t, "a failed write", err, translate.FloodWait(internalWait))
}

type fakeToggler struct {
	in *mtproto.TLChannelsToggleAutotranslation
}

func (f *fakeToggler) ChannelsToggleAutotranslation(_ context.Context, in *mtproto.TLChannelsToggleAutotranslation) (*mtproto.Updates, error) {
	f.in = in
	return mtproto.MakeTLUpdates(&mtproto.Updates{}).To_Updates(), nil
}

// channels.toggleAutotranslation goes on to the channels area.
func TestToggleAutotranslationDelegates(t *testing.T) {
	f := newFixture(t, false)
	toggler := &fakeToggler{}
	f.svc.Autotranslation = toggler
	in := &mtproto.TLChannelsToggleAutotranslation{Enabled: mtproto.BoolTrue}
	if _, err := f.core(false).ChannelsToggleAutotranslation(in); err != nil || toggler.in != in {
		t.Fatalf("not delegated: %v", err)
	}
}

func utf16Of(s string) []uint16     { return utf16.Encode([]rune(s)) }
func utf16Decode(u []uint16) []rune { return utf16.Decode(u) }
