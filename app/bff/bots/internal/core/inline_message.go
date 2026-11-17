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
	"time"
	"unicode/utf16"

	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/biz/message/message"
	"github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/botgames"
	"github.com/teamgram/teamgram-server/pkg/botmarkup"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// The messages users send via a bot, as the bot names them
// (inputBotInlineMessageID64, "a sent inline message from the perspective
// of a bot", core.telegram.org/constructor/inputBotInlineMessageID64): the
// bot gets the id with the press of a button of such a message
// (updateInlineBotCallbackQuery) and edits the message and the scores of
// its game by it (messages.editInlineBotMessage, setInlineGameScore,
// getInlineGameHighScores; task 106, ADR 0068 p. 7).
//
// The id is the server's own and opaque to the bot (no client reads it:
// the four clients only carry it in their schema):
//   - a message of a private chat or a basic group: owner_id is a user who
//     has a copy of it and id the copy's id in that user's box;
//   - a message of a channel or a supergroup: owner_id is the channel's id
//     negated and id the message's id in the channel.
//
// dc_id is this server's one DC ("DC ID to use when working with this
// inline message", the constructor's page), access_hash 0: the bot may
// edit any message sent via itself, which the via_bot_id of the message
// says, so the id needs no secret.

// homeDcId is the DC of the server (this_dc of help.getConfig,
// teamgramd/bin/config.json).
const homeDcId = 1

// Limits of a message's text and a media's caption in UTF-16 code units, as
// the clients count them: message_length_max of help.getConfig
// (teamgramd/bin/config.json) and the caption limit everybody has
// (caption_length_limit_premium, ADR 0041; app/bff/messages caption.go).
const (
	maxInlineText    = 4096
	maxInlineCaption = 4096
)

// inlineMessageId is the id of message msgId of peer as userId names it,
// for the bot it was sent via.
func inlineMessageId(peer *mtproto.PeerUtil, userId int64, msgId int32) *mtproto.InputBotInlineMessageID {
	owner := userId
	if peer.IsChannel() {
		owner = -peer.PeerId
	}
	return mtproto.MakeTLInputBotInlineMessageID64(&mtproto.InputBotInlineMessageID{
		DcId:     homeDcId,
		OwnerId:  owner,
		Id_INT32: msgId,
	}).To_InputBotInlineMessageID()
}

// inlineMessage is a message an inline message id names: the copy of the
// user owner, the message of a channel by its author.
type inlineMessage struct {
	owner int64
	// peer is the chat as owner names it.
	peer *mtproto.PeerUtil
	// box is owner's copy, with the message's id in owner's box (in the
	// channel for a channel's).
	box *mtproto.MessageBox
}

func (m *inlineMessage) id() int32                 { return m.box.GetMessageId() }
func (m *inlineMessage) message() *mtproto.Message { return m.box.GetMessage() }

// findInlineMessage finds the message in names; MESSAGE_ID_INVALID for an
// id the server never gave or a message gone since.
func (c *BotsCore) findInlineMessage(in *mtproto.InputBotInlineMessageID) (*inlineMessage, error) {
	if in.GetPredicateName() != mtproto.Predicate_inputBotInlineMessageID64 || in.GetId_INT32() <= 0 || in.GetOwnerId() == 0 {
		return nil, mtproto.ErrMessageIdInvalid
	}
	id := in.GetId_INT32()
	if in.GetOwnerId() < 0 {
		return c.findInlineChannelMessage(-in.GetOwnerId(), id)
	}
	owner := in.GetOwnerId()
	box, err := c.svcCtx.MessageGetUserMessage(c.ctx, &message.TLMessageGetUserMessage{UserId: owner, Id: id})
	if err != nil {
		if errors.Is(err, mtproto.ErrMessageIdInvalid) || errors.Is(err, mtproto.ErrMsgIdInvalid) {
			return nil, mtproto.ErrMessageIdInvalid
		}
		return nil, err
	}
	peer, ok := dialogPeer(owner, box)
	if !ok {
		return nil, mtproto.ErrMessageIdInvalid
	}
	return &inlineMessage{owner: owner, peer: peer, box: box}, nil
}

// dialogPeer is the chat of a copy in owner's box, by its dialog: the other
// user of a private chat (owner for Saved Messages) or the basic group.
func dialogPeer(owner int64, box *mtproto.MessageBox) (*mtproto.PeerUtil, bool) {
	did := mtproto.DialogID{A: box.GetDialogId1(), B: box.GetDialogId2()}
	switch {
	case did.A == -int64(mtproto.PEER_CHAT):
		return mtproto.MakeChatPeerUtil(did.B), did.B > 0
	case did.A > 0 && (did.A == owner || did.B == owner):
		return mtproto.MakeUserPeerUtil(mtproto.GetPeerIdByDialogId(owner, did)), true
	}
	return nil, false
}

// findInlineChannelMessage finds message id of the channel, read as its
// author reads it: the bot need not be a member, and the author edits it.
func (c *BotsCore) findInlineChannelMessage(channelId int64, id int32) (*inlineMessage, error) {
	author, err := c.svcCtx.ChannelPostAuthor(c.ctx, channelId, id)
	if err != nil {
		return nil, err
	}
	if author == 0 {
		return nil, mtproto.ErrMessageIdInvalid
	}
	messages, err := c.svcCtx.Channel.ChannelGetMessages(c.ctx, &channelpb.TLChannelGetMessages{SelfId: author, ChannelId: channelId, Id: []int32{id}})
	switch {
	case errors.Is(err, mtproto.ErrChannelPrivate), errors.Is(err, mtproto.ErrChannelInvalid):
		return nil, mtproto.ErrMessageIdInvalid
	case err != nil:
		return nil, err
	}
	if len(messages.GetDatas()) != 1 || messages.GetDatas()[0].GetPredicateName() != mtproto.Predicate_message {
		return nil, mtproto.ErrMessageIdInvalid
	}
	return &inlineMessage{
		owner: author,
		peer:  mtproto.MakeChannelPeerUtil(channelId),
		box: &mtproto.MessageBox{
			UserId:       author,
			MessageId:    id,
			SenderUserId: author,
			PeerType:     mtproto.PEER_CHANNEL,
			PeerId:       channelId,
			Message:      messages.GetDatas()[0],
		},
	}, nil
}

// senderCopy is the sender's copy of m, which an edit changes (msg edits a
// message of a private chat or a group in its sender's box and the other
// copies after it). The copy of a channel's message is the message.
// MESSAGE_ID_INVALID when the sender no longer has it.
func (c *BotsCore) senderCopy(m *inlineMessage) (*inlineMessage, error) {
	sender := m.box.GetSenderUserId()
	if m.peer.IsChannel() || sender == m.owner {
		return m, nil
	}
	box, err := c.svcCtx.MessageGetPeerUserMessage(c.ctx, &message.TLMessageGetPeerUserMessage{UserId: m.owner, PeerUserId: sender, MsgId: m.id()})
	if err != nil {
		if errors.Is(err, mtproto.ErrMessageIdInvalid) || errors.Is(err, mtproto.ErrMsgIdInvalid) {
			return nil, mtproto.ErrMessageIdInvalid
		}
		return nil, err
	}
	peer, ok := dialogPeer(sender, box)
	if !ok || box.GetSenderUserId() != sender || box.GetDialogMessageId() != m.box.GetDialogMessageId() {
		return nil, mtproto.ErrMessageIdInvalid
	}
	return &inlineMessage{owner: sender, peer: peer, box: box}, nil
}

// findInlineGameMessage is the message with a game of the calling bot that
// in names, for its scores: the sender's copy when the sender has it, else
// the copy in names. The game is the bot's (a forward of the game keeps it
// a game of the bot, which the press of its card names: pressedGame), not
// the via_bot_id. MESSAGE_ID_INVALID for another message.
func (c *BotsCore) findInlineGameMessage(in *mtproto.InputBotInlineMessageID) (*gameMessage, int32, error) {
	found, err := c.findInlineMessage(in)
	if err != nil {
		return nil, 0, err
	}
	if sender, err := c.senderCopy(found); err == nil {
		found = sender
	} else if !errors.Is(err, mtproto.ErrMessageIdInvalid) {
		return nil, 0, err
	}
	m := &gameMessage{owner: found.owner, peer: found.peer, message: found.message()}
	if err = c.withGame(m, found.id()); err != nil {
		return nil, 0, err
	}
	if m.game.BotId != c.MD.UserId {
		return nil, 0, mtproto.ErrMessageIdInvalid
	}
	return m, found.id(), nil
}

// inlineEdit is stored, the message of an inline message, after the bot's
// edit in: what messages.editMessage changes of a message (its text,
// entities, media and keyboard), with what a bot may give a message sent
// via it.
//   - The keyboard is inline (reply_markup: "Reply markup for inline
//     keyboards", core.telegram.org/method/messages.editInlineBotMessage),
//     checked by botmarkup as the keyboards of inline results; a game keeps
//     its button first, and the game stays the media (botgames.Markup).
//     Without reply_markup the keyboard stays, as messages.editMessage
//     keeps it (whether Telegram drops it then is not verified).
//   - Media is a photo or a document the bot was given (their access hash),
//     as Bot API's editMessageMedia allows an inline message: "new media
//     can't be uploaded; use a previously uploaded file via its file_id"
//     (core.telegram.org/bots/api#editmessagemedia). Media by URL is not
//     served: the server does not fetch a bot's URLs (inlineResult);
//     MEDIA_INVALID, as for any other media.
//   - An edit that changes nothing is MESSAGE_NOT_MODIFIED (the method's
//     errors).
func (c *BotsCore) inlineEdit(stored *mtproto.Message, in *mtproto.TLMessagesEditInlineBotMessage) (*mtproto.Message, error) {
	if in.GetRichMessage() != nil {
		// Rich messages are not served (messages.getRichMessage).
		return nil, mtproto.ErrInputRequestInvalid
	}
	out := proto.Clone(stored).(*mtproto.Message)
	game := stored.GetMedia().GetPredicateName() == mtproto.Predicate_messageMediaGame

	if in.ReplyMarkup != nil {
		env := c.markupEnv()
		env.Game = game
		markup, err := botmarkup.Normalize(in.ReplyMarkup, env)
		if err != nil {
			return nil, err
		}
		if markup != nil && markup.GetPredicateName() != mtproto.Predicate_replyInlineMarkup {
			return nil, mtproto.ErrReplyMarkupInvalid
		}
		if game {
			if markup, err = gameMarkup(markup, stored, c.MD.LangCode); err != nil {
				return nil, err
			}
		}
		out.ReplyMarkup = markup
	}

	if in.Media != nil {
		if game {
			return nil, mtproto.ErrMediaInvalid
		}
		media, err := c.inlineEditMedia(in.Media)
		if err != nil {
			return nil, err
		}
		out.Media = media
	}

	if in.Message != nil {
		text := in.Message.GetValue()
		hasMedia := out.GetMedia() != nil && out.GetMedia().GetPredicateName() != mtproto.Predicate_messageMediaEmpty
		switch n := len(utf16.Encode([]rune(text))); {
		case text == "" && !hasMedia:
			return nil, mtproto.ErrMessageEmpty
		case captioned(out) && n > maxInlineCaption:
			return nil, mtproto.ErrMediaCaptionTooLong
		case n > maxInlineText:
			return nil, mtproto.ErrMessageTooLong
		}
		out.Message = text
	}
	out.Entities = inlineEditEntities(stored.GetEntities(), in)

	if sameContent(stored, out) {
		return nil, mtproto.ErrMessageNotModified
	}
	out.EditDate = wrapperspb.Int32(int32(time.Now().Unix()))
	out.EditHide = false
	return out, nil
}

// gameMarkup is the keyboard of a game message after the bot gave markup:
// it starts with the game's button (botgames.Markup). An empty one keeps
// the button the message has, in the language its sender saw it.
func gameMarkup(markup *mtproto.ReplyMarkup, stored *mtproto.Message, lang string) (*mtproto.ReplyMarkup, error) {
	if markup == nil || !hasAnyButton(markup) {
		if old := stored.GetReplyMarkup(); botgames.HasButton(old) {
			return mtproto.MakeTLReplyInlineMarkup(&mtproto.ReplyMarkup{
				Rows: []*mtproto.KeyboardButtonRow{mtproto.MakeTLKeyboardButtonRow(&mtproto.KeyboardButtonRow{
					Buttons: []*mtproto.KeyboardButton{old.GetRows()[0].GetButtons()[0]},
				}).To_KeyboardButtonRow()},
			}).To_ReplyMarkup(), nil
		}
		markup = nil
	}
	return botgames.Markup(markup, stored.GetMedia().GetGame().GetTitle(), lang)
}

func hasAnyButton(markup *mtproto.ReplyMarkup) bool {
	for _, row := range markup.GetRows() {
		if len(row.GetButtons()) > 0 {
			return true
		}
	}
	return false
}

// captioned tells whether the text of m is the caption of its media.
func captioned(m *mtproto.Message) bool {
	switch m.GetMedia().GetPredicateName() {
	case mtproto.Predicate_messageMediaPhoto, mtproto.Predicate_messageMediaDocument:
		return true
	}
	return false
}

// inlineEditMedia is the media of an edit: a photo or a document by its id
// and the access hash the bot was given; MEDIA_INVALID otherwise.
func (c *BotsCore) inlineEditMedia(in *mtproto.InputMedia) (*mtproto.MessageMedia, error) {
	switch in.GetPredicateName() {
	case mtproto.Predicate_inputMediaPhoto:
		ref := in.GetId_INPUTPHOTO()
		photo, err := c.svcCtx.MediaGetPhoto(c.ctx, &media.TLMediaGetPhoto{PhotoId: ref.GetId()})
		if err != nil || photo.GetPredicateName() != mtproto.Predicate_photo || photo.GetAccessHash() != ref.GetAccessHash() {
			return nil, mtproto.ErrPhotoInvalid
		}
		return mtproto.MakeTLMessageMediaPhoto(&mtproto.MessageMedia{Spoiler: in.GetSpoiler(), Photo_FLAGPHOTO: photo}).To_MessageMedia(), nil
	case mtproto.Predicate_inputMediaDocument:
		ref := in.GetId_INPUTDOCUMENT()
		document, err := c.svcCtx.MediaGetDocument(c.ctx, &media.TLMediaGetDocument{Id: ref.GetId()})
		if err != nil || document.GetPredicateName() != mtproto.Predicate_document || document.GetAccessHash() != ref.GetAccessHash() {
			return nil, mtproto.ErrDocumentInvalid
		}
		return mtproto.MakeTLMessageMediaDocument(&mtproto.MessageMedia{Spoiler: in.GetSpoiler(), Document: document}).To_MessageMedia(), nil
	}
	return nil, mtproto.ErrMediaInvalid
}

// inlineEditEntities is what the entities of the edited message start from,
// as for messages.editMessage (app/bff/messages editedEntities): a new text
// or new entities bring their own; otherwise the message keeps its entities
// but the automatic ones, which msg makes again.
func inlineEditEntities(old []*mtproto.MessageEntity, in *mtproto.TLMessagesEditInlineBotMessage) []*mtproto.MessageEntity {
	if in.Message != nil || in.Entities != nil {
		return in.Entities
	}
	return withoutAutoEntities(old)
}

func withoutAutoEntities(entities []*mtproto.MessageEntity) []*mtproto.MessageEntity {
	kept := make([]*mtproto.MessageEntity, 0, len(entities))
	for _, e := range entities {
		switch e.GetPredicateName() {
		case mtproto.Predicate_messageEntityUrl, mtproto.Predicate_messageEntityMention,
			mtproto.Predicate_messageEntityHashtag, mtproto.Predicate_messageEntityBotCommand:
			continue
		}
		kept = append(kept, e)
	}
	return kept
}

// sameContent tells whether the edit leaves what it may change as it was:
// the text, the entities but the automatic ones, the media and the
// keyboard.
func sameContent(stored, edited *mtproto.Message) bool {
	if stored.GetMessage() != edited.GetMessage() ||
		!proto.Equal(stored.GetMedia(), edited.GetMedia()) ||
		!proto.Equal(stored.GetReplyMarkup(), edited.GetReplyMarkup()) {
		return false
	}
	a, b := withoutAutoEntities(stored.GetEntities()), withoutAutoEntities(edited.GetEntities())
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !proto.Equal(a[i], b[i]) {
			return false
		}
	}
	return true
}
