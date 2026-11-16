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
	"encoding/binary"
	"errors"
	"hash/fnv"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/dao"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/app/service/biz/message/message"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/botgames"
	"github.com/teamgram/teamgram-server/pkg/botstore"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
)

// The games of bots (task 106, ADR 0068 p. 7, core.telegram.org/api/bots/games):
// the press of a game's button, the games of inline results, and the
// messages messages.setGameScore and messages.getGameHighScores name.

// pressedGame is the short name of the game whose button was pressed in
// p's message: the message carries a game and its keyboard starts with the
// game's button (botgames.HasButton), DATA_INVALID otherwise. The game's
// bot answers the press. The message names it, by its sender or via_bot_id
// (pressedMessage); a game that came to the chat as a forward names its bot
// only by the game, and its press goes to that bot as the press of an
// inline message, which a bot gets in any chat. A forward has no button
// (a forward keeps only a keyboard of links, ReplyMarkup.ToForwardMessage),
// and iOS presses the game by its card all the same
// (ChatMessageGameBubbleContentNode.swift:30-37).
func (c *BotsCore) pressedGame(p *pressed) (string, error) {
	media := p.message.GetMedia()
	forward := p.message.GetFwdFrom() != nil
	if media.GetPredicateName() != mtproto.Predicate_messageMediaGame || (!forward && !botgames.HasButton(p.message.GetReplyMarkup())) {
		return "", mtproto.ErrDataInvalid
	}
	game, err := c.svcCtx.Bots.GameOf(c.ctx, media.GetGame().GetId())
	if err != nil {
		c.Logger.Errorf("messages.getBotCallbackAnswer - game: %v", err)
		return "", mtproto.ErrInternalServerError
	}
	if game == nil {
		return "", mtproto.ErrDataInvalid
	}
	if game.BotId != p.botId {
		if !forward {
			return "", mtproto.ErrDataInvalid
		}
		p.botId, p.inline = game.BotId, true
	}
	return game.ShortName, nil
}

// inlineGame is the game a game result of an inline answer names, as a
// message carries it; MEDIA_INVALID for a game removed since.
func (c *BotsCore) inlineGame(ref dao.GameRef) (*mtproto.Game, error) {
	game, err := c.svcCtx.Bots.GameById(c.ctx, ref.Id, ref.AccessHash)
	if err != nil {
		c.Logger.Errorf("inline game %d: %v", ref.Id, err)
		return nil, mtproto.ErrInternalServerError
	}
	if game == nil {
		return nil, mtproto.ErrMediaInvalid
	}
	tl, err := botgames.TL(c.ctx, c.svcCtx.MediaClient, game)
	if err != nil {
		c.Logger.Errorf("inline game %d: %v", ref.Id, err)
		return nil, mtproto.ErrInternalServerError
	}
	return tl, nil
}

// gameMessage is a message with a game as c's account sees it.
type gameMessage struct {
	peer    *mtproto.PeerUtil
	message *mtproto.Message
	game    *botstore.Game
}

// findGameMessage returns the message msgId of the chat as c's account
// names it (the bot in a private chat with it, the group, the
// supergroup), with its game; MESSAGE_ID_INVALID for a message the
// account does not have or one without a game.
func (c *BotsCore) findGameMessage(in *mtproto.InputPeer, msgId int32) (*gameMessage, error) {
	peer := c.chatPeer(in)
	m := &gameMessage{peer: peer}
	switch {
	case peer.IsUser(), peer.IsChat():
		box, err := c.svcCtx.MessageGetUserMessage(c.ctx, &message.TLMessageGetUserMessage{UserId: c.MD.UserId, Id: msgId})
		if err != nil {
			if errors.Is(err, mtproto.ErrMsgIdInvalid) {
				return nil, mtproto.ErrMessageIdInvalid
			}
			return nil, err
		}
		if (mtproto.DialogID{A: box.GetDialogId1(), B: box.GetDialogId2()}) != mtproto.MakeDialogId(c.MD.UserId, peer.PeerType, peer.PeerId) {
			return nil, mtproto.ErrMessageIdInvalid
		}
		m.message = box.GetMessage()
	case peer.IsChannel():
		messages, err := c.svcCtx.Channel.ChannelGetMessages(c.ctx, &channelpb.TLChannelGetMessages{SelfId: c.MD.UserId, ChannelId: peer.PeerId, Id: []int32{msgId}})
		if err != nil {
			return nil, err
		}
		if len(messages.GetDatas()) != 1 || messages.GetDatas()[0].GetPredicateName() != mtproto.Predicate_message {
			return nil, mtproto.ErrMessageIdInvalid
		}
		m.message = messages.GetDatas()[0]
	default:
		return nil, mtproto.ErrPeerIdInvalid
	}
	media := m.message.GetMedia()
	if media.GetPredicateName() != mtproto.Predicate_messageMediaGame {
		return nil, mtproto.ErrMessageIdInvalid
	}
	game, err := c.svcCtx.Bots.GameOf(c.ctx, media.GetGame().GetId())
	if err != nil {
		c.Logger.Errorf("game of message %d: %v", msgId, err)
		return nil, mtproto.ErrInternalServerError
	}
	if game == nil {
		return nil, mtproto.ErrMessageIdInvalid
	}
	m.game = game
	return m, nil
}

// player is the user a score is of: a user, not a bot, that exists;
// USER_ID_INVALID otherwise.
func (c *BotsCore) player(in *mtproto.InputUser) (int64, error) {
	target := mtproto.FromInputUser(c.MD.UserId, in)
	if !target.IsUser() {
		return 0, mtproto.ErrUserIdInvalid
	}
	u, err := c.svcCtx.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{Id: target.PeerId})
	if errors.Is(err, mtproto.ErrUserIdInvalid) {
		return 0, mtproto.ErrUserIdInvalid
	}
	if err != nil {
		return 0, err
	}
	if u.IsBot() || u.Deleted() {
		return 0, mtproto.ErrUserIdInvalid
	}
	return target.PeerId, nil
}

// playerInChat checks that the player of a score in a group or a
// supergroup is its member: the table of a chat is of its members, whom its
// other members see in getGameHighScores, and the message of the score is
// the player's message to the chat, which msg refuses from anyone else
// after the score was already stored. USER_ID_INVALID otherwise. A private
// chat has its own check, its user.
func (c *BotsCore) playerInChat(m *gameMessage, playerId int64) error {
	switch {
	case m.peer.IsChat():
		chat, err := c.svcCtx.Chat.ChatGetMutableChat(c.ctx, &chatpb.TLChatGetMutableChat{ChatId: m.peer.PeerId})
		if err != nil {
			return err
		}
		if p, in := chat.GetImmutableChatParticipant(playerId); !in || !p.IsChatMemberStateNormal() {
			return mtproto.ErrUserIdInvalid
		}
	case m.peer.IsChannel():
		mc, err := c.svcCtx.Channel.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: m.peer.PeerId, Id: []int64{playerId}})
		if err != nil {
			return err
		}
		if p, member := mc.GetParticipant(playerId); !member || p.GetState() != channelperm.StateIn {
			return mtproto.ErrUserIdInvalid
		}
	}
	return nil
}

// scoreRandomId is the random_id of the messageActionGameScore of a score:
// one message for the player's score of the game message, however often
// the bot repeats the same call.
func scoreRandomId(chat botstore.ScoreChat, msgId int32, gameId, userId int64, score int32) int64 {
	h := fnv.New64a()
	var b [len("game_score") + 1 + 8*4 + 4 + 4]byte
	n := copy(b[:], "game_score")
	b[n] = byte(chat.Type)
	n++
	for _, v := range []int64{chat.Id, chat.Id2, gameId, userId} {
		binary.BigEndian.PutUint64(b[n:], uint64(v))
		n += 8
	}
	binary.BigEndian.PutUint32(b[n:], uint32(msgId))
	binary.BigEndian.PutUint32(b[n+4:], uint32(score))
	_, _ = h.Write(b[:])
	return int64(h.Sum64() >> 1)
}

// scoreReply is where the player's messageActionGameScore goes: the chat
// as the player names it and the reply to the player's copy of the game
// message. The clients name the game of the score by the message it
// answers ("scored N in <game>": tdesktop history_item.cpp, the
// messageActionGameScore text; Android ActionUserScoredInGame "un1 scored
// %1$s in un2"). A player of a private chat or a group whose copy of the
// game message is gone gets the score without the reply.
type scoreReply struct {
	peer  *mtproto.PeerUtil
	reply *mtproto.MessageReplyHeader
}

func (c *BotsCore) gameScoreReply(m *gameMessage, msgId int32, playerId int64) (*scoreReply, error) {
	out := &scoreReply{}
	replyTo := msgId
	switch {
	case m.peer.IsUser():
		out.peer = mtproto.MakeUserPeerUtil(c.MD.UserId)
	case m.peer.IsChat():
		out.peer = mtproto.MakeChatPeerUtil(m.peer.PeerId)
	default:
		out.peer = mtproto.MakeChannelPeerUtil(m.peer.PeerId)
	}
	if !m.peer.IsChannel() {
		// The ids of private chats and groups are of each box.
		box, err := c.svcCtx.MessageGetPeerUserMessage(c.ctx, &message.TLMessageGetPeerUserMessage{UserId: c.MD.UserId, PeerUserId: playerId, MsgId: msgId})
		switch {
		case errors.Is(err, mtproto.ErrMsgIdInvalid), errors.Is(err, mtproto.ErrMessageIdInvalid):
			return out, nil
		case err != nil:
			return nil, err
		case (mtproto.DialogID{A: box.GetDialogId1(), B: box.GetDialogId2()}) != mtproto.MakeDialogId(playerId, out.peer.PeerType, out.peer.PeerId):
			return out, nil
		}
		replyTo = box.GetMessageId()
	}
	header := &mtproto.MessageReplyHeader{
		ReplyToMsgId:           replyTo,
		ReplyToMsgId_INT32:     replyTo,
		ReplyToMsgId_FLAGINT32: mtproto.MakeFlagsInt32(replyTo),
	}
	// In a topic of a forum the score stays in the topic of the game.
	if h := m.message.GetReplyTo(); m.peer.IsChannel() && h.GetForumTopic() {
		header.ForumTopic = true
		if top := h.GetReplyToTopId(); top != nil {
			header.ReplyToTopId = top
		} else {
			// The first message of the topic answers its creation.
			header.ReplyToTopId = h.GetReplyToMsgId_FLAGINT32()
		}
	}
	out.reply = mtproto.MakeTLMessageReplyHeader(header).To_MessageReplyHeader()
	return out, nil
}

// postScore posts messageActionGameScore(game_id, score) from the player
// to the chat of the game, as the player's own message: the clients write
// "you scored" by its sender (tdesktop history_item.cpp, the
// messageActionGameScore text). It goes to every session of the player; a
// repeat of the same score is RANDOM_ID_DUPLICATE, which means it was
// posted.
func (c *BotsCore) postScore(r *scoreReply, playerId, gameId int64, score int32, randomId int64) error {
	_, err := c.svcCtx.MsgSendMessageV2(c.ctx, &msgpb.TLMsgSendMessageV2{
		UserId:    playerId,
		AuthKeyId: 0,
		PeerType:  r.peer.PeerType,
		PeerId:    r.peer.PeerId,
		Message: []*msgpb.OutboxMessage{msgpb.MakeTLOutboxMessage(&msgpb.OutboxMessage{
			NoWebpage: true,
			RandomId:  randomId,
			Message: mtproto.MakeTLMessageService(&mtproto.Message{
				Out:     true,
				FromId:  mtproto.MakePeerUser(playerId),
				PeerId:  r.peer.ToPeer(),
				ReplyTo: r.reply,
				Date:    int32(time.Now().Unix()),
				Action: mtproto.MakeTLMessageActionGameScore(&mtproto.MessageAction{
					GameId: gameId,
					Score:  score,
				}).To_MessageAction(),
			}).To_Message(),
		}).To_OutboxMessage()},
	})
	if errors.Is(err, mtproto.ErrRandomIdDuplicate) {
		return nil
	}
	return err
}
