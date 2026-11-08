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
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const (
	replySelf  = int64(1001)
	replyOther = int64(1002)
	replyThird = int64(1003)
	replyChat  = int64(77)
)

// fakeReplySource is the sender's box: messages by id, the groups that
// forbid forwards and the authors whose privacy hides them.
type fakeReplySource struct {
	boxes      map[int32]*mtproto.MessageBox
	noforwards map[int64]bool
	hidden     map[int64]string
	err        error
}

func (s *fakeReplySource) message(id int32) (*mtproto.MessageBox, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.boxes[id], nil
}

func (s *fakeReplySource) chatNoforwards(chatId int64) (bool, error) {
	return s.noforwards[chatId], nil
}

func (s *fakeReplySource) authorOrigin(box *mtproto.MessageBox) (*mtproto.MessageFwdHeader, error) {
	h := &mtproto.MessageFwdHeader{Date: box.GetMessage().GetDate()}
	if name, ok := s.hidden[box.SenderUserId]; ok {
		h.FromName = wrapperspb.String(name)
	} else {
		h.FromId = mtproto.MakePeerUser(box.SenderUserId)
	}
	return mtproto.MakeTLMessageFwdHeader(h).To_MessageFwdHeader(), nil
}

// replyBox is message id of the sender's box in the dialog with peer, sent
// by sender.
func replyBox(id int32, sender int64, peerType int32, peerId int64, m *mtproto.Message) *mtproto.MessageBox {
	did := mtproto.MakeDialogId(replySelf, peerType, peerId)
	m.Id = id
	if m.Date == 0 {
		m.Date = 1700000000
	}
	if m.PredicateName == "" {
		m = mtproto.MakeTLMessage(m).To_Message()
	}
	return &mtproto.MessageBox{
		UserId:       replySelf,
		MessageId:    id,
		SenderUserId: sender,
		PeerType:     peerType,
		PeerId:       peerId,
		DialogId1:    did.A,
		DialogId2:    did.B,
		Message:      m,
	}
}

func photoMedia() *mtproto.MessageMedia {
	return mtproto.MakeTLMessageMediaPhoto(&mtproto.MessageMedia{
		Photo_FLAGPHOTO: mtproto.MakeTLPhoto(&mtproto.Photo{Id: 5}).To_Photo(),
	}).To_MessageMedia()
}

func newReplySource() *fakeReplySource {
	return &fakeReplySource{
		boxes: map[int32]*mtproto.MessageBox{
			// The private chat with replyOther: its message with a photo.
			10: replyBox(10, replyOther, mtproto.PEER_USER, replyOther, &mtproto.Message{
				Message: "look at this", Media: photoMedia(),
				Entities: []*mtproto.MessageEntity{
					entity(mtproto.Predicate_messageEntityBold, 0, 4),
					// Kinds a quote does not keep.
					entity(mtproto.Predicate_messageEntityUrl, 5, 2),
					entity(mtproto.Predicate_messageEntityMentionName, 8, 4),
				},
			}),
			// The sender's own text in the private chat with replyOther.
			11: replyBox(11, replySelf, mtproto.PEER_USER, replyOther, &mtproto.Message{Message: "mine"}),
			// The group: a message of replyThird.
			20: replyBox(20, replyThird, mtproto.PEER_CHAT, replyChat, &mtproto.Message{Message: "group text"}),
			// Saved Messages: a forward from replyThird.
			30: replyBox(30, replySelf, mtproto.PEER_USER, replySelf, &mtproto.Message{
				Message: "saved",
				FwdFrom: mtproto.MakeTLMessageFwdHeader(&mtproto.MessageFwdHeader{
					FromId:         mtproto.MakePeerUser(replyThird),
					Date:           1600000000,
					SavedFromPeer:  mtproto.MakePeerUser(replyThird),
					SavedFromMsgId: wrapperspb.Int32(4),
				}).To_MessageFwdHeader(),
			}),
			// A protected message.
			40: replyBox(40, replyOther, mtproto.PEER_USER, replyOther, &mtproto.Message{Message: "secret", Noforwards: true}),
			// A service message.
			50: replyBox(50, replyThird, mtproto.PEER_CHAT, replyChat, mtproto.MakeTLMessageService(&mtproto.Message{
				Action: mtproto.MakeMessageActionChatEditTitle("t"),
			}).To_Message()),
		},
		noforwards: map[int64]bool{},
		hidden:     map[int64]string{},
	}
}

func inputUser(id int64) *mtproto.InputPeer {
	return mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: id}).To_InputPeer()
}

func inputChat(id int64) *mtproto.InputPeer {
	return mtproto.MakeTLInputPeerChat(&mtproto.InputPeer{ChatId: id}).To_InputPeer()
}

func replyToMessage(id int32, peer *mtproto.InputPeer) *mtproto.InputReplyTo {
	return mtproto.MakeTLInputReplyToMessage(&mtproto.InputReplyTo{ReplyToMsgId: id, ReplyToPeerId: peer}).To_InputReplyTo()
}

var (
	toChat  = &mtproto.PeerUtil{PeerType: mtproto.PEER_CHAT, PeerId: replyChat}
	toOther = &mtproto.PeerUtil{PeerType: mtproto.PEER_USER, PeerId: replyOther}
	toSaved = &mtproto.PeerUtil{PeerType: mtproto.PEER_USER, PeerId: replySelf}
)

// msgId is the message id the header names.
func msgId(h *mtproto.MessageReplyHeader) int32 {
	return h.GetReplyToMsgId_FLAGINT32().GetValue()
}

// noIds tells whether a header names no message, chat or thread, in none
// of the three id fields (FixData restores the flag from any of them).
func noIds(h *mtproto.MessageReplyHeader) bool {
	return h.GetReplyToMsgId() == 0 && h.GetReplyToMsgId_INT32() == 0 && h.GetReplyToMsgId_FLAGINT32() == nil &&
		h.GetReplyToPeerId() == nil && h.GetReplyToTopId() == nil
}

// A reply in the same chat carries the id and the quote as the client
// sent them, from reply_to_msg_id and from inputReplyToMessage with or
// without the chat; a reply to a story is left to the caller.
func TestSameChatReply(t *testing.T) {
	src := newReplySource()
	h, err := makeReplyHeader(src, replySelf, toOther, wrapperspb.Int32(7), nil)
	if err != nil || msgId(h) != 7 || h.GetReplyToMsgId() != 7 || h.GetReplyFrom() != nil {
		t.Fatalf("legacy: %v, %v", h, err)
	}

	in := replyToMessage(8, nil)
	in.QuoteText = wrapperspb.String("anything")
	in.QuoteOffset = wrapperspb.Int32(3)
	h, err = makeReplyHeader(src, replySelf, toOther, nil, in)
	if err != nil || msgId(h) != 8 || !h.GetQuote() || h.GetQuoteText().GetValue() != "anything" || h.GetQuoteOffset().GetValue() != 3 {
		t.Fatalf("inputReplyToMessage: %v, %v", h, err)
	}

	// reply_to_peer_id naming the destination is a reply in this chat.
	for _, c := range []struct {
		peer *mtproto.PeerUtil
		in   *mtproto.InputReplyTo
	}{
		{toOther, replyToMessage(10, inputUser(replyOther))},
		{toChat, replyToMessage(20, inputChat(replyChat))},
		{toSaved, replyToMessage(30, mtproto.MakeTLInputPeerSelf(nil).To_InputPeer())},
	} {
		h, err = makeReplyHeader(src, replySelf, c.peer, nil, c.in)
		if err != nil || msgId(h) != c.in.ReplyToMsgId || h.GetReplyFrom() != nil || h.GetReplyToPeerId() != nil {
			t.Errorf("to %v: %v, %v", c.peer, h, err)
		}
	}

	story := mtproto.MakeTLInputReplyToStory(&mtproto.InputReplyTo{Peer: inputUser(replyOther), StoryId: 1}).To_InputReplyTo()
	if h, err = makeReplyHeader(src, replySelf, toOther, nil, story); h != nil || err != nil {
		t.Errorf("story: %v, %v", h, err)
	}
	if h, err = makeReplyHeader(src, replySelf, toOther, nil, nil); h != nil || err != nil {
		t.Errorf("no reply: %v, %v", h, err)
	}
}

// A reply to a message of another chat names neither the message nor the
// chat; it shows the author, the media without the caption and the text as
// an automatic quote.
func TestOtherChatReply(t *testing.T) {
	src := newReplySource()
	h, err := makeReplyHeader(src, replySelf, toChat, nil, replyToMessage(10, inputUser(replyOther)))
	if err != nil {
		t.Fatal(err)
	}
	if !noIds(h) {
		t.Errorf("ids: %v", h)
	}
	if h.GetPredicateName() != mtproto.Predicate_messageReplyHeader {
		t.Errorf("predicate %q", h.GetPredicateName())
	}
	from := h.GetReplyFrom()
	if from.GetFromId().GetUserId() != replyOther || from.GetDate() != 1700000000 || from.GetFromName() != nil {
		t.Errorf("reply_from: %v", from)
	}
	if h.GetReplyMedia().GetPredicateName() != mtproto.Predicate_messageMediaPhoto {
		t.Errorf("reply_media: %v", h.GetReplyMedia())
	}
	if h.GetQuote() || h.GetQuoteText().GetValue() != "look at this" || h.GetQuoteOffset() != nil {
		t.Errorf("quote: %v %v %v", h.GetQuote(), h.GetQuoteText(), h.GetQuoteOffset())
	}
	if es := h.GetQuoteEntities(); !slices.Equal(predicates(es), []string{mtproto.Predicate_messageEntityBold}) || es[0].GetLength() != 4 {
		t.Errorf("quote entities: %v", predicates(es))
	}

	// Plain text has no media; the sender's own message names them.
	h, err = makeReplyHeader(src, replySelf, toChat, nil, replyToMessage(11, inputUser(replyOther)))
	if err != nil || !noIds(h) || h.GetReplyMedia() != nil || h.GetReplyFrom().GetFromId().GetUserId() != replySelf ||
		h.GetQuoteText().GetValue() != "mine" {
		t.Errorf("own text: %v, %v", h, err)
	}

	// A text with a link preview shows the preview.
	src.boxes[15] = replyBox(15, replyOther, mtproto.PEER_USER, replyOther, &mtproto.Message{
		Message: "see https://example.com",
		Media: mtproto.MakeTLMessageMediaWebPage(&mtproto.MessageMedia{
			Webpage: mtproto.MakeTLWebPage(&mtproto.WebPage{Id: 6}).To_WebPage(),
		}).To_MessageMedia(),
		Entities: []*mtproto.MessageEntity{entity(mtproto.Predicate_messageEntityUrl, 4, 19)},
	})
	h, err = makeReplyHeader(src, replySelf, toChat, nil, replyToMessage(15, inputUser(replyOther)))
	if err != nil || !noIds(h) || h.GetReplyMedia().GetWebpage().GetId() != 6 ||
		h.GetQuoteText().GetValue() != "see https://example.com" || len(h.GetQuoteEntities()) != 0 {
		t.Errorf("link preview: %v, %v", h, err)
	}

	// From a group to a private chat; the author's privacy hides them.
	src.hidden[replyThird] = "Third User"
	h, err = makeReplyHeader(src, replySelf, toOther, nil, replyToMessage(20, inputChat(replyChat)))
	if err != nil || !noIds(h) || h.GetReplyFrom().GetFromId() != nil || h.GetReplyFrom().GetFromName().GetValue() != "Third User" {
		t.Errorf("hidden author: %v, %v", h, err)
	}

	// From the sender's own Saved Messages, named by self or by the
	// author: the origin of the forward, without the Saved Messages part.
	for _, peer := range []*mtproto.InputPeer{mtproto.MakeTLInputPeerSelf(nil).To_InputPeer(), inputUser(replyThird)} {
		h, err = makeReplyHeader(src, replySelf, toChat, nil, replyToMessage(30, peer))
		if err != nil || !noIds(h) {
			t.Fatalf("saved by %v: %v, %v", peer, h, err)
		}
		from := h.GetReplyFrom()
		if from.GetFromId().GetUserId() != replyThird || from.GetDate() != 1600000000 ||
			from.GetSavedFromPeer() != nil || from.GetSavedFromMsgId() != nil {
			t.Errorf("saved by %v: reply_from %v", peer, from)
		}
	}
	// The original keeps its forward header.
	if src.boxes[30].Message.FwdFrom.SavedFromPeer == nil {
		t.Error("the original's forward header changed")
	}
}

// The automatic quote is cut to QuoteLengthMax UTF-16 code units, before a
// surrogate pair the limit would split, with its entities cut to it.
func TestAutomaticQuoteLimit(t *testing.T) {
	text := strings.Repeat("a", QuoteLengthMax-1) + "😀tail"
	src := newReplySource()
	src.boxes[12] = replyBox(12, replyOther, mtproto.PEER_USER, replyOther, &mtproto.Message{
		Message: text,
		Entities: []*mtproto.MessageEntity{
			entity(mtproto.Predicate_messageEntityItalic, 0, 2),
			entity(mtproto.Predicate_messageEntityBold, QuoteLengthMax-3, 10),
			entity(mtproto.Predicate_messageEntityUnderline, QuoteLengthMax, 3),
		},
	})
	h, err := makeReplyHeader(src, replySelf, toChat, nil, replyToMessage(12, inputUser(replyOther)))
	if err != nil {
		t.Fatal(err)
	}
	quote := h.GetQuoteText().GetValue()
	if quote != strings.Repeat("a", QuoteLengthMax-1) {
		t.Errorf("quote of %d code units, ends %q", len(utf16.Encode([]rune(quote))), quote[len(quote)-4:])
	}
	es := h.GetQuoteEntities()
	if len(es) != 2 || es[0].GetLength() != 2 || es[1].GetOffset() != QuoteLengthMax-3 || es[1].GetLength() != 2 {
		t.Errorf("entities %v", es)
	}
	if src.boxes[12].Message.Entities[1].GetLength() != 10 {
		t.Error("the original's entities changed")
	}

	// A text that fits is quoted whole, its last pair included.
	if q, _ := automaticQuote(strings.Repeat("a", QuoteLengthMax-2)+"😀", nil); q != strings.Repeat("a", QuoteLengthMax-2)+"😀" {
		t.Errorf("whole: %q", q)
	}
	if q, es := automaticQuote("", nil); q != "" || es != nil {
		t.Errorf("empty: %q %v", q, es)
	}
}

// A manual quote is kept with the quote flag when it is a part of the text,
// with the offset where it is and the entities a quote keeps within it.
func TestManualQuote(t *testing.T) {
	src := newReplySource()
	src.boxes[13] = replyBox(13, replyOther, mtproto.PEER_USER, replyOther, &mtproto.Message{Message: "😀 hello world"})
	in := replyToMessage(13, inputUser(replyOther))
	in.QuoteText = wrapperspb.String("world")
	in.QuoteOffset = wrapperspb.Int32(9) // the emoji is two code units
	in.QuoteEntities = []*mtproto.MessageEntity{
		entity(mtproto.Predicate_messageEntityBold, 0, 5),
		entity(mtproto.Predicate_messageEntitySpoiler, 1, 2),
		// A kind a quote does not keep, and entities out of the quote.
		entity(mtproto.Predicate_messageEntityCode, 0, 5),
		entity(mtproto.Predicate_messageEntityItalic, 3, 3),
		entity(mtproto.Predicate_messageEntityStrike, 5, 1),
		entity(mtproto.Predicate_messageEntityUnderline, -1, 2),
	}
	h, err := makeReplyHeader(src, replySelf, toChat, nil, in)
	if err != nil {
		t.Fatal(err)
	}
	if !noIds(h) || !h.GetQuote() || h.GetQuoteText().GetValue() != "world" || h.GetQuoteOffset().GetValue() != 9 || h.GetReplyFrom() == nil {
		t.Errorf("manual quote: %v", h)
	}
	if got := predicates(h.GetQuoteEntities()); !slices.Equal(got, []string{mtproto.Predicate_messageEntityBold, mtproto.Predicate_messageEntitySpoiler}) {
		t.Errorf("manual quote entities: %v", got)
	}

	// The offset is where to look: Android sends the selection's start
	// with the selection trimmed of the whitespace it began with, and the
	// quote nearest to the offset is the one meant.
	for _, c := range []struct {
		text, quote  string
		offset, want int32
	}{
		{"😀 hello world", "world", 8, 9},      // the selection began on the space
		{"😀 hello\n\n world", "world", 8, 11}, // and on the line breaks
		{"😀 hello world", "😀 hel", 0, 0},      // at the start, no offset in the header
		{"😀 hello world", "hello", -1, 3},     // a negative offset is the start
		{"😀 hello world", "world", 100, 9},    // past the end
		{"one two one two", "two", 9, 12},     // the nearer of two
		{"one two one two", "two", 7, 4},      // the nearer, before the offset
		{"look at this", "this", 0, 8},        // wherever it is
	} {
		src.boxes[13].Message.Message = c.text
		in = replyToMessage(13, inputUser(replyOther))
		in.QuoteText = wrapperspb.String(c.quote)
		in.QuoteOffset = wrapperspb.Int32(c.offset)
		if h, err = makeReplyHeader(src, replySelf, toChat, nil, in); err != nil || !h.GetQuote() ||
			h.GetQuoteText().GetValue() != c.quote || h.GetQuoteOffset().GetValue() != c.want || (c.want == 0) != (h.GetQuoteOffset() == nil) {
			t.Errorf("%q of %q at %d: %v, %v, want offset %d", c.quote, c.text, c.offset, h.GetQuoteOffset(), err, c.want)
		}
	}
}

// The errors of a reply to a message of another chat.
func TestOtherChatReplyErrors(t *testing.T) {
	long := strings.Repeat("b", QuoteLengthMax+1)
	src := newReplySource()
	src.boxes[14] = replyBox(14, replyOther, mtproto.PEER_USER, replyOther, &mtproto.Message{Message: long})
	quote := func(id int32, text string, offset int32) *mtproto.InputReplyTo {
		in := replyToMessage(id, inputUser(replyOther))
		in.QuoteText = wrapperspb.String(text)
		in.QuoteOffset = wrapperspb.Int32(offset)
		return in
	}
	for name, c := range map[string]struct {
		peer *mtproto.PeerUtil
		in   *mtproto.InputReplyTo
		want error
	}{
		"no such message":        {toChat, replyToMessage(99, inputUser(replyOther)), errReplyMessageIdInvalid},
		"message of other chat":  {toChat, replyToMessage(20, inputUser(replyOther)), errReplyMessageIdInvalid},
		"named as destination":   {toOther, replyToMessage(20, inputUser(replyOther)), errReplyMessageIdInvalid},
		"saved, not the author":  {toChat, replyToMessage(30, inputUser(replyOther)), errReplyMessageIdInvalid},
		"channel":                {toChat, replyToMessage(10, mtproto.MakeTLInputPeerChannel(&mtproto.InputPeer{ChannelId: 5}).To_InputPeer()), errReplyMessageIdInvalid},
		"service message":        {toOther, replyToMessage(50, inputChat(replyChat)), errReplyMessageIdInvalid},
		"protected message":      {toChat, replyToMessage(40, inputUser(replyOther)), mtproto.ErrChatForwardsRestricted},
		"quote not in text":      {toChat, quote(10, "nothing", 0), errQuoteTextInvalid},
		"quote longer than text": {toChat, quote(10, "look at this!", 0), errQuoteTextInvalid},
		"quote too long":         {toChat, quote(14, long, 0), errQuoteTextInvalid},
	} {
		h, err := makeReplyHeader(src, replySelf, c.peer, nil, c.in)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: %v, %v, want %v", name, h, err, c.want)
		}
	}

	src.noforwards[replyChat] = true
	if _, err := makeReplyHeader(src, replySelf, toOther, nil, replyToMessage(20, inputChat(replyChat))); !errors.Is(err, mtproto.ErrChatForwardsRestricted) {
		t.Errorf("group forbids forwards: %v", err)
	}
	// A group forbidding forwards does not stop replies in it.
	if h, err := makeReplyHeader(src, replySelf, toChat, nil, replyToMessage(20, inputChat(replyChat))); err != nil || msgId(h) != 20 {
		t.Errorf("same group: %v, %v", h, err)
	}

	src.err = errors.New("unavailable")
	if _, err := makeReplyHeader(src, replySelf, toChat, nil, replyToMessage(10, inputUser(replyOther))); err != src.err {
		t.Errorf("read error: %v", err)
	}
}

// Every update and result carrying such a reply carries the users it
// names: the author in reply_from and the user of a contact in
// reply_media.
func TestOtherChatReplyUsers(t *testing.T) {
	h := mtproto.MakeTLMessageReplyHeader(&mtproto.MessageReplyHeader{
		ReplyFrom: mtproto.MakeTLMessageFwdHeader(&mtproto.MessageFwdHeader{
			FromId: mtproto.MakePeerUser(replyOther), Date: 1, SavedFromPeer: mtproto.MakePeerUser(replyThird),
		}).To_MessageFwdHeader(),
		ReplyMedia: mtproto.MakeTLMessageMediaContact(&mtproto.MessageMedia{UserId: 4004}).To_MessageMedia(),
	}).To_MessageReplyHeader()
	ids := mtproto.NewIDListHelper()
	ids.PickByMessage(mtproto.MakeTLMessage(&mtproto.Message{PeerId: mtproto.MakePeerChat(replyChat), ReplyTo: h}).To_Message())
	for _, id := range []int64{replyOther, replyThird, 4004} {
		if !slices.Contains(ids.UserIdList, id) {
			t.Errorf("user %d not picked: %v", id, ids.UserIdList)
		}
	}
}

// The clients cut quotes by help.getAppConfig: the app config must give the
// limit the server checks.
func TestQuoteLimitIsTheAppConfigs(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "teamgramd", "bin", "app_config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err = json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if v, ok := config["quote_length_max"].(float64); !ok || v != QuoteLengthMax {
		t.Errorf("quote_length_max of app_config.json %v, the server's limit %d", config["quote_length_max"], QuoteLengthMax)
	}
}

// A reply to a channel carries top_msg_id to biz; to the other chats it means
// nothing. A forward to a channel names only the topic.
func TestReplyAndForwardTopic(t *testing.T) {
	channel := &mtproto.PeerUtil{PeerType: mtproto.PEER_CHANNEL, PeerId: 7}
	user := &mtproto.PeerUtil{PeerType: mtproto.PEER_USER, PeerId: 7}
	in := mtproto.MakeTLInputReplyToMessage(&mtproto.InputReplyTo{ReplyToMsgId: 3, TopMsgId: wrapperspb.Int32(9)}).To_InputReplyTo()
	h := func() *mtproto.MessageReplyHeader {
		return sameChatReply(3, nil)
	}
	if got := replyWithTopic(h(), channel, in); got.GetReplyToTopId().GetValue() != 9 {
		t.Errorf("to a channel: %v", got)
	}
	if got := replyWithTopic(h(), user, in); got.GetReplyToTopId() != nil {
		t.Errorf("to a user: %v", got)
	}
	if got := forwardTopicHeader(channel, wrapperspb.Int32(9), nil); got.GetReplyToTopId().GetValue() != 9 || got.GetReplyToMsgId() != 0 {
		t.Errorf("a forward to a topic: %v", got)
	}
	if got := forwardTopicHeader(user, wrapperspb.Int32(9), nil); got != nil {
		t.Errorf("a forward to a user: %v", got)
	}
}
