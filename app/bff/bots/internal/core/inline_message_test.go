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
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/dao"
	msg_client "github.com/teamgram/teamgram-server/app/messenger/msg/msg/client"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	message_client "github.com/teamgram/teamgram-server/app/service/biz/message/client"
	"github.com/teamgram/teamgram-server/app/service/biz/message/message"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// inlineUsers knows which users of the test are bots.
type inlineUsers struct {
	user_client.UserClient
	bots map[int64]bool
}

func (u inlineUsers) UserGetImmutableUser(_ context.Context, in *userpb.TLUserGetImmutableUser) (*mtproto.ImmutableUser, error) {
	data := &mtproto.UserData{Id: in.Id, AccessHash: 1, FirstName: "User"}
	if u.bots[in.Id] {
		data.Bot = mtproto.MakeTLBotData(&mtproto.BotData{}).To_BotData()
	}
	return mtproto.MakeTLImmutableUser(&mtproto.ImmutableUser{User: mtproto.MakeTLUserData(data).To_UserData()}).To_ImmutableUser(), nil
}

// inlineBoxes are the copies of the test's messages by user and id; the
// copies of one message share its dialog_message_id.
type inlineBoxes struct {
	message_client.MessageClient
	boxes map[[2]int64]*mtproto.MessageBox
}

func (b *inlineBoxes) put(user int64, id int32, box *mtproto.MessageBox) {
	box.UserId, box.MessageId = user, id
	b.boxes[[2]int64{user, int64(id)}] = box
}

func (b *inlineBoxes) MessageGetUserMessage(_ context.Context, in *message.TLMessageGetUserMessage) (*mtproto.MessageBox, error) {
	if box, ok := b.boxes[[2]int64{in.UserId, int64(in.Id)}]; ok {
		return box, nil
	}
	return nil, mtproto.ErrMessageIdInvalid
}

func (b *inlineBoxes) MessageGetPeerUserMessage(_ context.Context, in *message.TLMessageGetPeerUserMessage) (*mtproto.MessageBox, error) {
	mine, ok := b.boxes[[2]int64{in.UserId, int64(in.MsgId)}]
	if !ok {
		return nil, mtproto.ErrMsgIdInvalid
	}
	for k, box := range b.boxes {
		if k[0] == in.PeerUserId && box.DialogMessageId == mine.DialogMessageId {
			return box, nil
		}
	}
	return nil, mtproto.ErrMsgIdInvalid
}

// editsSeen keeps the edits msg is asked for.
type editsSeen struct {
	msg_client.MsgClient
	edits []*msgpb.TLMsgEditMessageV2
}

func (e *editsSeen) MsgEditMessageV2(_ context.Context, in *msgpb.TLMsgEditMessageV2) (*mtproto.Updates, error) {
	e.edits = append(e.edits, in)
	return mtproto.MakeEmptyUpdates(), nil
}

// The id of an inline message names the chat it is in: a box of a user, or
// the channel, by its id negated; the server's one DC.
func TestInlineMessageId(t *testing.T) {
	id := inlineMessageId(mtproto.MakeChatPeerUtil(77), 10, 6)
	if id.GetPredicateName() != mtproto.Predicate_inputBotInlineMessageID64 || id.GetOwnerId() != 10 || id.GetId_INT32() != 6 || id.GetDcId() != homeDcId {
		t.Errorf("a group's: %v", id)
	}
	if id = inlineMessageId(mtproto.MakeUserPeerUtil(20), 10, 6); id.GetOwnerId() != 10 {
		t.Errorf("a private chat's: %v", id)
	}
	if id = inlineMessageId(mtproto.MakeChannelPeerUtil(500), 10, 6); id.GetOwnerId() != -500 || id.GetId_INT32() != 6 {
		t.Errorf("a channel's: %v", id)
	}
	// The press of an inline message in a channel names the channel.
	p := &pressed{peer: mtproto.MakeChannelPeerUtil(500), botId: 20, inline: true}
	u, _, err := testCore(&dao.Dao{}, 10).callbackUpdate(1, p, 6, []byte("go"), "")
	if err != nil {
		t.Fatal(err)
	}
	if got := u.GetMsgId_FLAGINPUTBOTINLINEMESSAGEID(); got.GetOwnerId() != -500 || got.GetId_INT32() != 6 {
		t.Errorf("the press in a channel: %v", got)
	}
}

// TestEditInlineBotMessage: Ann sent a message via the bot to a group; Bob
// pressed its button, and the bot edits it by the id of Bob's copy. The
// edit is Ann's, of her copy, and changes what the bot gave.
func TestEditInlineBotMessage(t *testing.T) {
	const ann, bob, bot, other, group = int64(10), int64(11), int64(20), int64(21), int64(77)
	markup := func(text string) *mtproto.ReplyMarkup {
		return mtproto.MakeTLReplyInlineMarkup(&mtproto.ReplyMarkup{Rows: []*mtproto.KeyboardButtonRow{
			mtproto.MakeTLKeyboardButtonRow(&mtproto.KeyboardButtonRow{Buttons: []*mtproto.KeyboardButton{
				mtproto.MakeTLKeyboardButtonCallback(&mtproto.KeyboardButton{Text: text, Data: []byte("go")}).To_KeyboardButton(),
			}}).To_KeyboardButtonRow(),
		}}).To_ReplyMarkup()
	}
	sent := func(via int64) *mtproto.Message {
		return mtproto.MakeTLMessage(&mtproto.Message{FromId: mtproto.MakePeerUser(ann), PeerId: mtproto.MakePeerChat(group),
			Message: "first", ViaBotId: wrapperspb.Int64(via), ReplyMarkup: markup("Go")}).To_Message()
	}
	inGroup := mtproto.MakeDialogId(ann, mtproto.PEER_CHAT, group)
	boxes := &inlineBoxes{boxes: map[[2]int64]*mtproto.MessageBox{}}
	for user, id := range map[int64]int32{ann: 9, bob: 6} {
		boxes.put(user, id, &mtproto.MessageBox{SenderUserId: ann, DialogId1: inGroup.A, DialogId2: inGroup.B, DialogMessageId: 2, Message: sent(bot)})
	}
	// A message via another bot, and a forward of the bot's message.
	boxes.put(bob, 7, &mtproto.MessageBox{SenderUserId: ann, DialogId1: inGroup.A, DialogId2: inGroup.B, DialogMessageId: 3, Message: sent(other)})
	boxes.put(ann, 10, &mtproto.MessageBox{SenderUserId: ann, DialogId1: inGroup.A, DialogId2: inGroup.B, DialogMessageId: 3, Message: sent(other)})
	forward := sent(bot)
	forward.FwdFrom = mtproto.MakeTLMessageFwdHeader(&mtproto.MessageFwdHeader{FromId: mtproto.MakePeerUser(ann)}).To_MessageFwdHeader()
	boxes.put(ann, 11, &mtproto.MessageBox{SenderUserId: ann, DialogId1: inGroup.A, DialogId2: inGroup.B, DialogMessageId: 4, Message: forward})

	msgs := &editsSeen{}
	d := &dao.Dao{UserClient: inlineUsers{bots: map[int64]bool{bot: true, other: true}}, MessageClient: boxes, MsgClient: msgs}
	b := testCore(d, bot)
	edit := func(c *BotsCore, owner int64, id int32, text string) error {
		_, err := c.MessagesEditInlineBotMessage(&mtproto.TLMessagesEditInlineBotMessage{
			Id:      inlineMessageId(mtproto.MakeChatPeerUtil(group), owner, id),
			Message: wrapperspb.String(text),
		})
		return err
	}

	if err := edit(b, bob, 6, "second"); err != nil {
		t.Fatal(err)
	}
	if len(msgs.edits) != 1 {
		t.Fatalf("%d edits", len(msgs.edits))
	}
	e := msgs.edits[0]
	m := e.GetNewMessage().GetMessage()
	if e.UserId != ann || e.AuthKeyId != 0 || e.PeerType != mtproto.PEER_CHAT || e.PeerId != group || e.EditType != msgpb.EditTypeViaBot ||
		e.GetDstMessage().GetMessageId() != 9 || m.GetMessage() != "second" || m.GetViaBotId().GetValue() != bot ||
		m.GetEditDate() == nil || m.GetReplyMarkup().GetRows()[0].GetButtons()[0].GetText() != "Go" {
		t.Fatalf("the edit: %v", e)
	}
	// The sender's own id names the same message.
	if err := edit(b, ann, 9, "third"); err != nil || len(msgs.edits) != 2 || msgs.edits[1].GetDstMessage().GetMessageId() != 9 {
		t.Errorf("by the sender's id: %v", err)
	}

	for name, err := range map[string]error{
		"nothing changed":       edit(b, bob, 6, "first"),
		"another bot's message": edit(b, bob, 7, "x"),
		"another bot":           edit(testCore(d, other), bob, 6, "x"),
		"a forward":             edit(b, ann, 11, "x"),
		"no such message":       edit(b, bob, 99, "x"),
		"an empty text":         edit(b, bob, 6, ""),
		"a user":                edit(testCore(d, bob), bob, 6, "x"),
		"an id of the old kind": func() error {
			_, err := b.MessagesEditInlineBotMessage(&mtproto.TLMessagesEditInlineBotMessage{Id: mtproto.MakeTLInputBotInlineMessageID(&mtproto.InputBotInlineMessageID{DcId: 1, Id_INT64: 6}).To_InputBotInlineMessageID(), Message: wrapperspb.String("x")})
			return err
		}(),
	} {
		want := mtproto.ErrMessageIdInvalid
		switch name {
		case "nothing changed":
			want = mtproto.ErrMessageNotModified
		case "an empty text":
			want = mtproto.ErrMessageEmpty
		case "a user":
			want = mtproto.ErrUserBotRequired
		}
		if !errors.Is(err, want) {
			t.Errorf("%s: %v, want %v", name, err, want)
		}
	}
	if len(msgs.edits) != 2 {
		t.Errorf("%d edits after the refused ones", len(msgs.edits))
	}
}

// The edit of a game keeps the game and its button first.
func TestInlineEditOfAGame(t *testing.T) {
	c := testCore(&dao.Dao{}, 20)
	play := mtproto.MakeTLKeyboardButtonGame(&mtproto.KeyboardButton{Text: "Spiele Jump"}).To_KeyboardButton()
	stored := mtproto.MakeTLMessage(&mtproto.Message{
		Media: mtproto.MakeTLMessageMediaGame(&mtproto.MessageMedia{Game: mtproto.MakeTLGame(&mtproto.Game{Id: 7, Title: "Jump"}).To_Game()}).To_MessageMedia(),
		ReplyMarkup: mtproto.MakeTLReplyInlineMarkup(&mtproto.ReplyMarkup{Rows: []*mtproto.KeyboardButtonRow{
			mtproto.MakeTLKeyboardButtonRow(&mtproto.KeyboardButtonRow{Buttons: []*mtproto.KeyboardButton{play}}).To_KeyboardButtonRow(),
			mtproto.MakeTLKeyboardButtonRow(&mtproto.KeyboardButtonRow{Buttons: []*mtproto.KeyboardButton{
				mtproto.MakeTLKeyboardButtonUrl(&mtproto.KeyboardButton{Text: "Rules", Url: "https://example.org/rules"}).To_KeyboardButton(),
			}}).To_KeyboardButtonRow(),
		}}).To_ReplyMarkup(),
	}).To_Message()

	// An empty keyboard keeps the game's button, in the words the sender saw.
	empty := mtproto.MakeTLReplyInlineMarkup(&mtproto.ReplyMarkup{}).To_ReplyMarkup()
	out, err := c.inlineEdit(stored, &mtproto.TLMessagesEditInlineBotMessage{ReplyMarkup: empty})
	if err != nil {
		t.Fatal(err)
	}
	if rows := out.GetReplyMarkup().GetRows(); len(rows) != 1 || len(rows[0].GetButtons()) != 1 || rows[0].GetButtons()[0].GetText() != "Spiele Jump" ||
		out.GetMedia().GetGame().GetId() != 7 {
		t.Errorf("the game after an empty keyboard: %v", out)
	}
	// A keyboard without the game's button first, and new media, are refused.
	noGame := mtproto.MakeTLReplyInlineMarkup(&mtproto.ReplyMarkup{Rows: []*mtproto.KeyboardButtonRow{
		mtproto.MakeTLKeyboardButtonRow(&mtproto.KeyboardButtonRow{Buttons: []*mtproto.KeyboardButton{
			mtproto.MakeTLKeyboardButtonUrl(&mtproto.KeyboardButton{Text: "Rules", Url: "https://example.org/rules"}).To_KeyboardButton(),
		}}).To_KeyboardButtonRow(),
	}}).To_ReplyMarkup()
	if _, err = c.inlineEdit(stored, &mtproto.TLMessagesEditInlineBotMessage{ReplyMarkup: noGame}); !errors.Is(err, mtproto.ErrButtonTypeInvalid) {
		t.Errorf("a keyboard without the game's button: %v", err)
	}
	photo := mtproto.MakeTLInputMediaPhoto(&mtproto.InputMedia{Id_INPUTPHOTO: mtproto.MakeTLInputPhoto(&mtproto.InputPhoto{Id: 5, AccessHash: 50}).To_InputPhoto()}).To_InputMedia()
	if _, err = c.inlineEdit(stored, &mtproto.TLMessagesEditInlineBotMessage{Media: photo}); !errors.Is(err, mtproto.ErrMediaInvalid) {
		t.Errorf("new media of a game: %v", err)
	}
}

// New media of an inline message is a file the bot was given; nothing is
// uploaded or fetched for it.
func TestInlineEditMedia(t *testing.T) {
	c := testCore(&dao.Dao{MediaClient: fakeFiles{}}, 20)
	stored := mtproto.MakeTLMessage(&mtproto.Message{Message: "look"}).To_Message()
	photo := func(hash int64) *mtproto.InputMedia {
		return mtproto.MakeTLInputMediaPhoto(&mtproto.InputMedia{Spoiler: true, Id_INPUTPHOTO: mtproto.MakeTLInputPhoto(&mtproto.InputPhoto{Id: 5, AccessHash: hash}).To_InputPhoto()}).To_InputMedia()
	}
	out, err := c.inlineEdit(stored, &mtproto.TLMessagesEditInlineBotMessage{Media: photo(50)})
	if err != nil {
		t.Fatal(err)
	}
	if out.GetMedia().GetPredicateName() != mtproto.Predicate_messageMediaPhoto || out.GetMedia().GetPhoto_FLAGPHOTO().GetId() != 5 || !out.GetMedia().GetSpoiler() || out.GetMessage() != "look" {
		t.Errorf("the photo: %v", out)
	}
	if _, err = c.inlineEdit(stored, &mtproto.TLMessagesEditInlineBotMessage{Media: photo(51)}); !errors.Is(err, mtproto.ErrPhotoInvalid) {
		t.Errorf("a photo without its access hash: %v", err)
	}
	external := mtproto.MakeTLInputMediaPhotoExternal(&mtproto.InputMedia{Url: "https://example.org/a.jpg"}).To_InputMedia()
	if _, err = c.inlineEdit(stored, &mtproto.TLMessagesEditInlineBotMessage{Media: external}); !errors.Is(err, mtproto.ErrMediaInvalid) {
		t.Errorf("a photo by URL: %v", err)
	}
	long := make([]rune, maxInlineCaption+1)
	for i := range long {
		long[i] = 'a'
	}
	if _, err = c.inlineEdit(out, &mtproto.TLMessagesEditInlineBotMessage{Message: wrapperspb.String(string(long))}); !errors.Is(err, mtproto.ErrMediaCaptionTooLong) {
		t.Errorf("a long caption: %v", err)
	}
}
