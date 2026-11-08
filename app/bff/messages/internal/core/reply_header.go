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
	"errors"
	"unicode/utf16"

	"github.com/teamgram/proto/mtproto"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/app/service/biz/message/message"

	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// Errors of a reply to a message of another chat, as tdlib understands
// them (MessagesManager.cpp, 25227-25236 and 28148-28158): the message
// cannot be replied to, or the manual quote is not in it.
var (
	errReplyMessageIdInvalid = status.Error(mtproto.ErrBadRequest, "REPLY_MESSAGE_ID_INVALID")
	errQuoteTextInvalid      = status.Error(mtproto.ErrBadRequest, "QUOTE_TEXT_INVALID")
)

// QuoteLengthMax is the longest quote of a reply in UTF-16 code units, the
// unit the clients cut quotes in (Android ChatActivity.ReplyQuote.from); the
// clients take it from quote_length_max of help.getAppConfig.
const QuoteLengthMax = 1024

// replySource reads what the reply header of a message is built from.
type replySource interface {
	// message returns the message id of the sender's box, or nil when the
	// box has no such message.
	message(id int32) (*mtproto.MessageBox, error)
	// chatNoforwards tells whether the basic group forbids forwarding its
	// messages.
	chatNoforwards(chatId int64) (bool, error)
	// authorOrigin is the header of a forward of the message in box, which
	// is not a forward itself, by the sender: the author, or the author's
	// name when their forward privacy hides the account, and the date.
	authorOrigin(box *mtproto.MessageBox) (*mtproto.MessageFwdHeader, error)
}

// coreReplySource reads the sources of a reply of c's user.
type coreReplySource struct {
	c *MessagesCore
}

func (s coreReplySource) message(id int32) (*mtproto.MessageBox, error) {
	box, err := s.c.svcCtx.Dao.MessageClient.MessageGetUserMessage(s.c.ctx, &message.TLMessageGetUserMessage{
		UserId: s.c.MD.UserId,
		Id:     id,
	})
	if errors.Is(err, mtproto.ErrMessageIdInvalid) {
		return nil, nil
	}
	return box, err
}

func (s coreReplySource) chatNoforwards(chatId int64) (bool, error) {
	chat, err := s.c.svcCtx.Dao.ChatClient.Client().ChatGetMutableChat(s.c.ctx, &chatpb.TLChatGetMutableChat{
		ChatId: chatId,
	})
	if err != nil {
		return false, err
	}
	return chat.Noforwards(), nil
}

func (s coreReplySource) authorOrigin(box *mtproto.MessageBox) (*mtproto.MessageFwdHeader, error) {
	return s.c.forwardOrigin(box)
}

// makeReplyHeader is the reply header of a message c's user sends to peer
// (a user or a basic group, the user itself as PEER_USER), from the
// legacy reply_to_msg_id or from reply_to; nil when the message replies to
// no message (a reply to a story is built by the caller).
func (c *MessagesCore) makeReplyHeader(peer *mtproto.PeerUtil, replyToMsgId *wrapperspb.Int32Value, replyTo *mtproto.InputReplyTo) (*mtproto.MessageReplyHeader, error) {
	h, err := makeReplyHeader(coreReplySource{c: c}, c.MD.UserId, peer, replyToMsgId, replyTo)
	if err != nil {
		return nil, err
	}
	return replyWithTopic(h, peer, replyTo), nil
}

// replyWithTopic carries top_msg_id of a reply sent to a channel (a topic
// of a forum, core.telegram.org/api/forum) to biz, which resolves it (task
// 060); it means nothing in the other chats.
func replyWithTopic(h *mtproto.MessageReplyHeader, peer *mtproto.PeerUtil, replyTo *mtproto.InputReplyTo) *mtproto.MessageReplyHeader {
	if h != nil && peer.PeerType == mtproto.PEER_CHANNEL && replyTo.GetTopMsgId() != nil {
		h.ReplyToTopId = replyTo.GetTopMsgId()
	}
	return h
}

// forwardTopicHeader names the topic of a forward to a channel: top_msg_id
// (Android SendMessagesHelper.java:2508-2513), or that of reply_to; biz
// resolves it (task 060). nil for no topic.
func forwardTopicHeader(to *mtproto.PeerUtil, topMsgId *wrapperspb.Int32Value, replyTo *mtproto.InputReplyTo) *mtproto.MessageReplyHeader {
	if to.PeerType != mtproto.PEER_CHANNEL {
		return nil
	}
	if topMsgId == nil {
		topMsgId = replyTo.GetTopMsgId()
	}
	if topMsgId == nil {
		return nil
	}
	return mtproto.MakeTLMessageReplyHeader(&mtproto.MessageReplyHeader{ReplyToTopId: topMsgId}).To_MessageReplyHeader()
}

// makeReplyHeader is the reply header of a message selfId sends to peer.
//
// A reply to a message of the same chat carries the message id, which msg
// turns into each recipient's own id. reply_to_peer_id names the chat of a
// message of another chat (core.telegram.org/constructor/inputReplyToMessage):
// a private chat, a basic group or Saved Messages of the sender, where the
// message is looked up by the sender's own id; from Saved Messages Android
// names the author of the message instead (SendMessagesHelper.java:4927).
// Such a reply names neither the chat nor the message: those exist only for
// the sender, and tdlib expects no id for these chats
// (RepliedMessageInfo.cpp:197-209). The header shows the origin of the
// message as a forward of it would, its media and a quote, the same for
// every reader (otherChatReply).
func makeReplyHeader(src replySource, selfId int64, peer *mtproto.PeerUtil, replyToMsgId *wrapperspb.Int32Value, replyTo *mtproto.InputReplyTo) (*mtproto.MessageReplyHeader, error) {
	if replyToMsgId != nil {
		return sameChatReply(replyToMsgId.GetValue(), nil), nil
	}
	if replyTo.GetPredicateName() != mtproto.Predicate_inputReplyToMessage {
		return nil, nil
	}
	if replyTo.GetReplyToPeerId() == nil {
		return sameChatReply(replyTo.GetReplyToMsgId(), replyTo), nil
	}

	source := mtproto.FromInputPeer2(selfId, replyTo.GetReplyToPeerId())
	if source.PeerType == mtproto.PEER_CHANNEL && peer.PeerType == mtproto.PEER_CHANNEL && source.PeerId == peer.PeerId {
		// A message of another topic of the same forum: the clients name
		// the forum itself (tdesktop data_histories.cpp:75-77, Android
		// SendMessagesHelper.java:4947-4953), and biz keeps the new message
		// in the topic it is written to (task 060).
		header := sameChatReply(replyTo.GetReplyToMsgId(), replyTo)
		header.ReplyToPeerId = mtproto.MakePeerChannel(peer.PeerId)
		return header, nil
	}
	switch source.PeerType {
	case mtproto.PEER_SELF:
		source.PeerType = mtproto.PEER_USER
	case mtproto.PEER_USER, mtproto.PEER_CHAT:
	default:
		return nil, errReplyMessageIdInvalid
	}
	box, err := src.message(replyTo.GetReplyToMsgId())
	if err != nil {
		return nil, err
	}
	if box == nil {
		return nil, errReplyMessageIdInvalid
	}
	// The dialog of a copy is in dialog_id1/2; its peer_id is the chat as
	// its sender addressed it.
	in := mtproto.DialogID{A: box.DialogId1, B: box.DialogId2}
	saved := mtproto.MakeDialogId(selfId, mtproto.PEER_USER, selfId)
	named := in == mtproto.MakeDialogId(selfId, source.PeerType, source.PeerId)
	if !named && (in != saved || !savedAuthor(box.GetMessage(), selfId, source)) {
		return nil, errReplyMessageIdInvalid
	}
	if in == mtproto.MakeDialogId(selfId, peer.PeerType, peer.PeerId) {
		return sameChatReply(replyTo.GetReplyToMsgId(), replyTo), nil
	}
	return otherChatReply(src, selfId, box, replyTo)
}

// sameChatReply is the header of a reply to the message id of the same
// chat, with the quote the client chose.
func sameChatReply(id int32, replyTo *mtproto.InputReplyTo) *mtproto.MessageReplyHeader {
	header := mtproto.MakeTLMessageReplyHeader(&mtproto.MessageReplyHeader{
		ReplyToMsgId:           id,
		ReplyToMsgId_INT32:     id,
		ReplyToMsgId_FLAGINT32: mtproto.MakeFlagsInt32(id),
	}).To_MessageReplyHeader()
	if replyTo.GetQuoteText() != nil {
		header.Quote = true
		header.QuoteText = replyTo.GetQuoteText()
		header.QuoteEntities = replyTo.GetQuoteEntities()
		header.QuoteOffset = replyTo.GetQuoteOffset()
	}
	// A reply to one answer or one checklist item (api/poll, api/todo).
	header.TodoItemId = replyTo.GetTodoItemId()
	header.PollOption = replyTo.GetPollOption()
	return header
}

// savedAuthor tells whether p is the author of m, a message of selfId's
// Saved Messages, as Android names it (MessageObject.getSenderId): the
// user itself, or the sender or the chat a forward came from.
func savedAuthor(m *mtproto.Message, selfId int64, p *mtproto.PeerUtil) bool {
	if p.PeerType == mtproto.PEER_USER && p.PeerId == selfId {
		return true
	}
	for _, author := range []*mtproto.Peer{m.GetFwdFrom().GetFromId(), m.GetFwdFrom().GetSavedFromPeer()} {
		if author == nil {
			continue
		}
		if a := mtproto.FromPeer(author); a.PeerType == p.PeerType && a.PeerId == p.PeerId {
			return true
		}
	}
	return false
}

// otherChatReply is the header of a reply to the message in box, of
// another chat of the sender selfId, as tdlib expects it
// (RepliedMessageInfo.cpp:148-211, MessageQuote.cpp:140-144):
//   - reply_from is the origin a forward of the message would show:
//     its author, or the author's name when their forward privacy hides
//     the account, and its date; the origin of a forward is its own;
//   - reply_media is the media of the message without its caption, the
//     link preview of a text; a plain text has none;
//   - the quote is the one the client chose, which must be a part of the
//     text, or else the beginning of the text up to QuoteLengthMax without
//     the quote flag; either keeps only the entities a quote may have.
//
// A protected message, or one of a group that forbids forwards, cannot be
// replied to from another chat (core.telegram.org/constructor/
// inputReplyToMessage); nor can a service message, which cannot be
// forwarded either.
func otherChatReply(src replySource, selfId int64, box *mtproto.MessageBox, replyTo *mtproto.InputReplyTo) (*mtproto.MessageReplyHeader, error) {
	m := box.GetMessage()
	if m.GetPredicateName() != mtproto.Predicate_message {
		return nil, errReplyMessageIdInvalid
	}
	if m.GetNoforwards() {
		return nil, mtproto.ErrChatForwardsRestricted
	}
	if box.DialogId1 == -int64(mtproto.PEER_CHAT) {
		restricted, err := src.chatNoforwards(box.DialogId2)
		if err != nil {
			return nil, err
		}
		if restricted {
			return nil, mtproto.ErrChatForwardsRestricted
		}
	}

	header := &mtproto.MessageReplyHeader{}
	if quote := replyTo.GetQuoteText().GetValue(); quote != "" {
		offset, n := findQuote(m.GetMessage(), quote, replyTo.GetQuoteOffset().GetValue())
		if offset < 0 {
			return nil, errQuoteTextInvalid
		}
		header.Quote = true
		header.QuoteText = wrapperspb.String(quote)
		header.QuoteEntities = quoteEntities(replyTo.GetQuoteEntities(), n, false)
		if offset > 0 {
			header.QuoteOffset = wrapperspb.Int32(int32(offset))
		}
	} else if text, entities := automaticQuote(m.GetMessage(), m.GetEntities()); text != "" {
		header.QuoteText = wrapperspb.String(text)
		header.QuoteEntities = entities
	}

	var err error
	if header.ReplyFrom, err = replyOrigin(src, selfId, box); err != nil {
		return nil, err
	}
	if media := m.GetMedia(); media != nil && media.GetPredicateName() != mtproto.Predicate_messageMediaEmpty {
		header.ReplyMedia = media
	}
	return mtproto.MakeTLMessageReplyHeader(header).To_MessageReplyHeader(), nil
}

// replyOrigin is reply_from of a reply by selfId to the message in box: the
// origin of a forward, without what it says of the Saved Messages it was
// saved to, or the author of the message. The user's own message names
// them: their privacy hides them from others, not from themselves (tdlib
// MessageOrigin::hide_sender_if_needed reads the name others see).
func replyOrigin(src replySource, selfId int64, box *mtproto.MessageBox) (*mtproto.MessageFwdHeader, error) {
	if fwd := box.GetMessage().GetFwdFrom(); fwd != nil {
		origin := proto.Clone(fwd).(*mtproto.MessageFwdHeader)
		origin.SavedOut = false
		origin.SavedFromPeer = nil
		origin.SavedFromMsgId = nil
		origin.SavedFromId = nil
		origin.SavedFromName = nil
		origin.SavedDate = nil
		return origin, nil
	}
	if box.GetSenderUserId() == selfId {
		return mtproto.MakeTLMessageFwdHeader(&mtproto.MessageFwdHeader{
			FromId: mtproto.MakePeerUser(selfId),
			Date:   box.GetMessage().GetDate(),
		}).To_MessageFwdHeader(), nil
	}
	return src.authorOrigin(box)
}

// quoteEntityTypes are the entities a quote keeps, as tdlib keeps them
// (MessageQuote::remove_unallowed_quote_entities, which logs an error for
// any other in a reply header) and Android copies them into its quote
// (ChatActivity.ReplyQuote.update).
var quoteEntityTypes = map[string]bool{
	mtproto.Predicate_messageEntityBold:        true,
	mtproto.Predicate_messageEntityItalic:      true,
	mtproto.Predicate_messageEntityUnderline:   true,
	mtproto.Predicate_messageEntityStrike:      true,
	mtproto.Predicate_messageEntitySpoiler:     true,
	mtproto.Predicate_messageEntityCustomEmoji: true,
}

// findQuote is the offset of quote in text and its length, both in UTF-16
// code units, or -1 when text has no such part or the quote is longer than
// QuoteLengthMax. quote_offset is a hint, not the exact place: Android
// sends the offset of the selection with the selection trimmed of
// whitespace (ChatActivity.ReplyQuote.update, SendMessagesHelper's
// quote_offset = replyQuote.start), and the clients look for the quote
// nearest to the offset (tdlib MessageQuote::search_quote, Android
// MessageObject.findQuoteStart); findQuote looks as tdlib does.
func findQuote(text, quote string, offset int32) (int, int) {
	q := utf16.Encode([]rune(quote))
	t := utf16.Encode([]rune(text))
	if len(q) == 0 || len(q) > QuoteLengthMax || len(q) > len(t) {
		return -1, 0
	}
	last := len(t) - len(q)
	at := func(i int) bool {
		if i < 0 || i > last {
			return false
		}
		for j, c := range q {
			if t[i+j] != c {
				return false
			}
		}
		return true
	}
	p := min(max(int(offset), 0), len(t)-1)
	for i := 0; p-i >= 0 || p+i+1 <= last; i++ {
		if at(p - i) {
			return p - i, len(q)
		}
		if at(p + i + 1) {
			return p + i + 1, len(q)
		}
	}
	return -1, 0
}

// automaticQuote is the beginning of text up to QuoteLengthMax UTF-16 code
// units, never inside a surrogate pair, with its entities cut to it.
func automaticQuote(text string, entities []*mtproto.MessageEntity) (string, []*mtproto.MessageEntity) {
	t := utf16.Encode([]rune(text))
	n := len(t)
	if n > QuoteLengthMax {
		n = QuoteLengthMax
		if utf16.IsSurrogate(rune(t[n-1])) && t[n-1] < 0xdc00 {
			n--
		}
	}
	if n == 0 {
		return "", nil
	}
	return string(utf16.Decode(t[:n])), quoteEntities(entities, n, true)
}

// quoteEntities are the entities of a quote n UTF-16 code units long out
// of entities: only the types a quote keeps, and only those within it; an
// entity running past the end is cut to it when cut is set and dropped
// otherwise, as tdlib drops it (MessageQuote::search_quote).
func quoteEntities(entities []*mtproto.MessageEntity, n int, cut bool) []*mtproto.MessageEntity {
	var kept []*mtproto.MessageEntity
	for _, e := range entities {
		if !quoteEntityTypes[e.GetPredicateName()] || e.GetOffset() < 0 || e.GetLength() <= 0 || int(e.GetOffset()) >= n {
			continue
		}
		end := int64(e.GetOffset()) + int64(e.GetLength())
		if end > int64(n) && !cut {
			continue
		}
		e = proto.Clone(e).(*mtproto.MessageEntity)
		if end > int64(n) {
			e.Length = int32(n) - e.GetOffset()
		}
		kept = append(kept, e)
	}
	return kept
}
