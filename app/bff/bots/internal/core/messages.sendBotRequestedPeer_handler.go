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

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/dao"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
)

// maxRequestedPeers bounds max_quantity of a request-peer button (Bot API
// KeyboardButtonRequestUsers, max_quantity "1-10"); a button that names less
// than one asks for one.
const maxRequestedPeers = 10

// basicAdminRights are the rights an admin of a basic group holds: basic
// groups have no rights of their own per admin, and the clients take these
// (tdesktop data/data_chat.cpp:52-65, ChatData::defaultAdminRights; the
// creator adds add_admins).
const basicAdminRights = mtproto.ADMIN_OTHER | mtproto.ADMIN_CHANGE_INFO | mtproto.ADMIN_DELETE_MESSAGES |
	mtproto.ADMIN_BAN_USERS | mtproto.ADMIN_INVITE_USERS | mtproto.ADMIN_PIN_MESSAGES | mtproto.ADMIN_MANAGE_CALL

// MessagesSendBotRequestedPeer
// messages.sendBotRequestedPeer#6c5cf2a7 flags:# peer:InputPeer msg_id:flags.0?int webapp_req_id:flags.1?string button_id:int requested_peers:Vector<InputPeer> = Updates;
//
// The user answers a request-peer button of a bot with the peers it chose
// (core.telegram.org/api/bots/buttons): the button of a keyboard of the
// bot's message msg_id in the private chat with the bot, or the button the
// bot prepared for its mini app under webapp_req_id
// (bots.requestWebViewButton). At most max_quantity peers, each of the
// button's peer_type. The answer is one service message from the user to the
// bot whose copies differ (msg OutboxMessage.incoming_action): the user's is
// messageActionRequestedPeer(button_id, peers), the bot's
// messageActionRequestedPeerSentMe with the names, usernames and photos the
// button asked for (name_requested, username_requested, photo_requested),
// as the bot sees them.
//
// A chat for a button that wants the bot in it (bot_participant) or with
// admin rights (bot_admin_rights) gets the bot added or promoted first, as
// the user's own act: tdesktop and iOS only tell the user "This will also add
// {bot} to {chat} with the following rights" and send nothing but this
// method (tdesktop boxes/peers/choose_peer_box.cpp:215-256, 540-556, lang
// lng_request_peer_confirm_rights; iOS TelegramUI/Sources/ChatController.swift:
// 5212-5245, 5305-5323), while Android promotes the bot itself before it
// sends (ui/DialogsActivity.java:11797-11800), which then finds it done.
// The rights the user must have are checked as the clients filter the chats
// (choose_peer_box.cpp:379-434); the biz services check again what the user
// may do to the bot.
func (c *BotsCore) MessagesSendBotRequestedPeer(in *mtproto.TLMessagesSendBotRequestedPeer) (*mtproto.Updates, error) {
	if c.MD.IsBot {
		return nil, mtproto.ErrBotMethodInvalid
	}
	peer := c.chatPeer(in.GetPeer())
	if !peer.IsUser() {
		return nil, mtproto.ErrPeerIdInvalid
	}
	bot, err := c.botUser(peer.PeerId)
	if errors.Is(err, mtproto.ErrBotInvalid) || errors.Is(err, mtproto.ErrUserIdInvalid) || errors.Is(err, mtproto.ErrInputUserDeactivated) {
		return nil, mtproto.ErrPeerIdInvalid
	}
	if err != nil {
		return nil, err
	}
	button, err := c.requestPeerButton(in, bot.Id())
	if err != nil {
		return nil, err
	}
	peers, err := requestedPeerList(in.GetRequestedPeers(), c.MD.UserId, button.GetMaxQuantity())
	if err != nil {
		return nil, err
	}

	var (
		shared []sharedPeer
		before []*mtproto.Updates
	)
	switch pt := button.GetPeerType(); pt.GetPredicateName() {
	case mtproto.Predicate_requestPeerTypeUser:
		shared, err = c.requestedUsers(peers, pt, button, bot.Id())
	case mtproto.Predicate_requestPeerTypeChat, mtproto.Predicate_requestPeerTypeBroadcast:
		shared, before, err = c.requestedChats(peers, pt, button, bot)
	case mtproto.Predicate_requestPeerTypeCreateBot:
		// Managed bots are not served (ADR 0043).
		err = mtproto.ErrPeerTypesInvalid
	default:
		err = mtproto.ErrButtonIdInvalid
	}
	if err != nil {
		return nil, err
	}

	mine := make([]*mtproto.Peer, 0, len(shared))
	sentMe := make([]*mtproto.RequestedPeer, 0, len(shared))
	for _, s := range shared {
		mine = append(mine, s.peer)
		sentMe = append(sentMe, s.sentMe)
	}
	randomId, err := dao.NewQueryId()
	if err != nil {
		return nil, mtproto.ErrInternalServerError
	}
	sent, err := c.svcCtx.MsgSendMessageV2(c.ctx, &msgpb.TLMsgSendMessageV2{
		UserId:    c.MD.UserId,
		AuthKeyId: c.MD.PermAuthKeyId,
		PeerType:  mtproto.PEER_USER,
		PeerId:    bot.Id(),
		Message: []*msgpb.OutboxMessage{msgpb.MakeTLOutboxMessage(&msgpb.OutboxMessage{
			NoWebpage: true,
			RandomId:  randomId,
			Message: mtproto.MakeTLMessageService(&mtproto.Message{
				Out:    true,
				FromId: mtproto.MakePeerUser(c.MD.UserId),
				PeerId: mtproto.MakePeerUser(bot.Id()),
				Date:   int32(time.Now().Unix()),
				Action: mtproto.MakeTLMessageActionRequestedPeer(&mtproto.MessageAction{
					ButtonId:         button.GetButtonId(),
					Peers_VECTORPEER: mine,
				}).To_MessageAction(),
			}).To_Message(),
			IncomingAction: mtproto.MakeTLMessageActionRequestedPeerSentMe(&mtproto.MessageAction{
				ButtonId:                  button.GetButtonId(),
				Peers_VECTORREQUESTEDPEER: sentMe,
			}).To_MessageAction(),
		}).To_OutboxMessage()},
	})
	if err != nil {
		c.Logger.Errorf("messages.sendBotRequestedPeer - bot %d: %v", bot.Id(), err)
		return nil, err
	}
	if len(before) == 0 {
		return sent, nil
	}
	return c.withUsers(mergeUpdates(append(before, sent)...))
}

// requestPeerButton is the button the request answers: the prepared one of
// webapp_req_id, or the one of button_id in the keyboard of the bot's
// message msg_id. A button that is not there is BUTTON_ID_INVALID, a message
// that is not the bot's MESSAGE_ID_INVALID (core.telegram.org/method/
// messages.sendBotRequestedPeer), a webapp_req_id that is not a live button
// of the bot for the user WEBAPP_REQ_ID_INVALID.
func (c *BotsCore) requestPeerButton(in *mtproto.TLMessagesSendBotRequestedPeer, botId int64) (*mtproto.KeyboardButton, error) {
	if id := in.GetWebappReqId(); id != nil {
		b, err := c.preparedButton("messages.sendBotRequestedPeer", botId, id.GetValue())
		if err != nil {
			return nil, err
		}
		if b.GetButtonId() != in.GetButtonId() {
			return nil, mtproto.ErrButtonIdInvalid
		}
		return b, nil
	}
	msgId := in.GetMsgId_FLAGINT32()
	if msgId == nil {
		return nil, mtproto.ErrMessageIdInvalid
	}
	p, err := c.pressedMessage(mtproto.MakePeerUtil(mtproto.PEER_USER, botId), msgId.GetValue())
	if err != nil {
		return nil, err
	}
	if p.inline || p.message.GetOut() || p.message.GetPredicateName() != mtproto.Predicate_message {
		return nil, mtproto.ErrMessageIdInvalid
	}
	if b := findRequestPeerButton(p.message.GetReplyMarkup(), in.GetButtonId()); b != nil {
		return b, nil
	}
	return nil, mtproto.ErrButtonIdInvalid
}

// findRequestPeerButton returns the request-peer button of the id in markup,
// nil for none.
func findRequestPeerButton(markup *mtproto.ReplyMarkup, buttonId int32) *mtproto.KeyboardButton {
	for _, row := range markup.GetRows() {
		for _, b := range row.GetButtons() {
			if b.GetPredicateName() == mtproto.Predicate_keyboardButtonRequestPeer && b.GetButtonId() == buttonId {
				return b
			}
		}
	}
	return nil
}

// requestedPeerList resolves the chosen peers: at least one
// (PEERS_LIST_EMPTY), at most max_quantity of the button, between 1 and
// maxRequestedPeers (LIMIT_INVALID), none twice (PEER_ID_INVALID).
func requestedPeerList(in []*mtproto.InputPeer, selfId int64, maxQuantity int32) ([]*mtproto.PeerUtil, error) {
	limit := min(max(int(maxQuantity), 1), maxRequestedPeers)
	switch {
	case len(in) == 0:
		return nil, mtproto.ErrPeersListEmpty
	case len(in) > limit:
		return nil, mtproto.ErrLimitInvalid
	}
	peers := make([]*mtproto.PeerUtil, 0, len(in))
	seen := make(map[[2]int64]bool, len(in))
	for _, p := range in {
		peer := mtproto.FromInputPeer2(selfId, p)
		if peer.PeerType == mtproto.PEER_SELF {
			peer.PeerType = mtproto.PEER_USER
		}
		key := [2]int64{int64(peer.PeerType), peer.PeerId}
		if peer.PeerId == 0 || seen[key] {
			return nil, mtproto.ErrPeerIdInvalid
		}
		seen[key] = true
		peers = append(peers, peer)
	}
	return peers, nil
}

// sharedPeer is a chosen peer as the user's copy names it and as the bot's
// copy describes it.
type sharedPeer struct {
	peer   *mtproto.Peer
	sentMe *mtproto.RequestedPeer
}

// boolMatches tells whether value meets a restriction of RequestPeerType:
// none, or the same.
func boolMatches(restriction *mtproto.Bool, value bool) bool {
	return restriction == nil || mtproto.FromBool(restriction) == value
}

// requestedUsers checks users for requestPeerTypeUser: users that exist,
// bots or not and Premium or not as the type asks (Premium as every user
// has it, ADR 0041). Anything else is PEER_ID_INVALID.
func (c *BotsCore) requestedUsers(peers []*mtproto.PeerUtil, pt *mtproto.RequestPeerType, button *mtproto.KeyboardButton, botId int64) ([]sharedPeer, error) {
	ids := make([]int64, 0, len(peers)+1)
	for _, p := range peers {
		if !p.IsUser() {
			return nil, mtproto.ErrPeerIdInvalid
		}
		ids = append(ids, p.PeerId)
	}
	users, err := c.svcCtx.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: append(ids, botId)})
	if err != nil {
		return nil, err
	}
	out := make([]sharedPeer, 0, len(ids))
	for _, id := range ids {
		u, ok := users.GetImmutableUser(id)
		if !ok || u.Deleted() || !boolMatches(pt.GetBot(), u.IsBot()) || !boolMatches(pt.GetPremium(), u.Premium()) {
			return nil, mtproto.ErrPeerIdInvalid
		}
		rp := &mtproto.RequestedPeer{UserId: id}
		// The user as the bot sees it: its privacy of the photo holds.
		seen, err := users.GetUnsafeUser(botId, id)
		if err != nil {
			return nil, err
		}
		if button.GetNameRequested() {
			rp.FirstName = mtproto.MakeFlagsString(seen.GetFirstName().GetValue())
			rp.LastName = mtproto.MakeFlagsString(seen.GetLastName().GetValue())
		}
		if name := seen.GetUsername().GetValue(); button.GetUsernameRequested() && name != "" {
			rp.Username = mtproto.MakeFlagsString(name)
		}
		if photo := u.Photo(); button.GetPhotoRequested() && photo.GetPredicateName() == mtproto.Predicate_photo &&
			seen.GetPhoto().GetPhotoId() == photo.GetId() {
			rp.Photo = photo
		}
		out = append(out, sharedPeer{
			peer:   mtproto.MakePeerUser(id),
			sentMe: mtproto.MakeTLRequestedPeerUser(rp).To_RequestedPeer(),
		})
	}
	return out, nil
}

// botStep is a change that brings the bot into a chosen chat; it returns
// the updates of the change for the user's session.
type botStep func() (*mtproto.Updates, error)

// requestedChats checks groups and channels for requestPeerTypeChat and
// requestPeerTypeBroadcast and brings the bot in as the type asks. Every
// chat is checked before the bot is brought into any, so that a chat that
// does not match leaves the others as they were. It returns the updates of
// those changes for the user's session.
func (c *BotsCore) requestedChats(peers []*mtproto.PeerUtil, pt *mtproto.RequestPeerType, button *mtproto.KeyboardButton, bot *mtproto.ImmutableUser) ([]sharedPeer, []*mtproto.Updates, error) {
	var (
		out   = make([]sharedPeer, 0, len(peers))
		steps []botStep
	)
	for _, p := range peers {
		var (
			s   sharedPeer
			st  []botStep
			err error
		)
		switch {
		case p.IsChat() && pt.GetPredicateName() == mtproto.Predicate_requestPeerTypeChat:
			s, st, err = c.requestedBasicGroup(p.PeerId, pt, button, bot)
		case p.IsChannel():
			s, st, err = c.requestedChannel(p.PeerId, pt, button, bot)
		default:
			err = mtproto.ErrPeerIdInvalid
		}
		if err != nil {
			return nil, nil, err
		}
		out = append(out, s)
		steps = append(steps, st...)
	}
	updates := make([]*mtproto.Updates, 0, len(steps))
	for _, step := range steps {
		u, err := step()
		if err != nil {
			return nil, nil, err
		}
		if u != nil {
			updates = append(updates, u)
		}
	}
	return out, updates, nil
}

// requestedBasicGroup checks a basic group: the user is in it, its creator
// when the type asks, with the user_admin_rights (an admin of a basic group
// has basicAdminRights); a basic group has no username and is no forum.
// The bot joins it for bot_participant or bot_admin_rights and is made an
// admin for bot_admin_rights, as Android does it (MessagesController.java:
// 8894-8917: addUserToChat, then messages.editChatAdmin).
func (c *BotsCore) requestedBasicGroup(chatId int64, pt *mtproto.RequestPeerType, button *mtproto.KeyboardButton, bot *mtproto.ImmutableUser) (sharedPeer, []botStep, error) {
	chat, err := c.svcCtx.Chat.ChatGetMutableChat(c.ctx, &chatpb.TLChatGetMutableChat{ChatId: chatId})
	if errors.Is(err, mtproto.ErrChatIdInvalid) || errors.Is(err, mtproto.ErrPeerIdInvalid) {
		return sharedPeer{}, nil, mtproto.ErrPeerIdInvalid
	}
	if err != nil {
		return sharedPeer{}, nil, err
	}
	me, in := chat.GetImmutableChatParticipant(c.MD.UserId)
	if chat.Deactivated() || chat.MigratedTo() != nil || !in || !me.IsChatMemberStateNormal() {
		return sharedPeer{}, nil, mtproto.ErrPeerIdInvalid
	}
	creator := chat.Creator() == c.MD.UserId
	var have int32
	switch {
	case creator:
		have = basicAdminRights | mtproto.ADMIN_ADD_ADMINS
	case me.IsChatMemberAdmin():
		have = basicAdminRights
	}
	if pt.GetCreator() && !creator || !boolMatches(pt.GetHasUsername(), false) || !boolMatches(pt.GetForum(), false) ||
		!holds(have, pt.GetUserAdminRights()) {
		return sharedPeer{}, nil, mtproto.ErrPeerIdInvalid
	}

	var steps []botStep
	botP, botIn := chat.GetImmutableChatParticipant(bot.Id())
	botIn = botIn && botP.IsChatMemberStateNormal()
	if !botIn && (pt.GetBotParticipant() || pt.GetBotAdminRights() != nil) {
		if bot.BotNochats() {
			return sharedPeer{}, nil, mtproto.ErrBotGroupsBlocked
		}
		steps = append(steps, func() (*mtproto.Updates, error) { return c.addBotToBasicGroup(chatId, bot) })
	}
	if pt.GetBotAdminRights() != nil && !(botIn && (botP.IsChatMemberAdmin() || botP.IsChatMemberCreator())) {
		steps = append(steps, func() (*mtproto.Updates, error) {
			if _, err := c.svcCtx.Chat.ChatEditChatAdmin(c.ctx, &chatpb.TLChatEditChatAdmin{
				ChatId:          chatId,
				OperatorId:      c.MD.UserId,
				EditChatAdminId: bot.Id(),
				IsAdmin:         mtproto.BoolTrue,
			}); err != nil {
				c.Logger.Errorf("messages.sendBotRequestedPeer - admin bot %d in chat %d: %v", bot.Id(), chatId, err)
				return nil, err
			}
			return nil, nil
		})
	}

	rp := &mtproto.RequestedPeer{ChatId: chatId}
	if button.GetNameRequested() {
		rp.Title = mtproto.MakeFlagsString(chat.Title())
	}
	if photo := chat.Photo(); button.GetPhotoRequested() && photo.GetPredicateName() == mtproto.Predicate_photo {
		rp.Photo = photo
	}
	return sharedPeer{peer: mtproto.MakePeerChat(chatId), sentMe: mtproto.MakeTLRequestedPeerChat(rp).To_RequestedPeer()}, steps, nil
}

// addBotToBasicGroup adds the bot to a basic group as the user, as
// messages.addChatUser does: the member and its service message.
func (c *BotsCore) addBotToBasicGroup(chatId int64, bot *mtproto.ImmutableUser) (*mtproto.Updates, error) {
	chat, err := c.svcCtx.Chat.ChatAddChatUser(c.ctx, &chatpb.TLChatAddChatUser{
		ChatId:    chatId,
		InviterId: c.MD.UserId,
		UserId:    bot.Id(),
		IsBot:     true,
	})
	if err != nil {
		c.Logger.Errorf("messages.sendBotRequestedPeer - add bot %d to chat %d: %v", bot.Id(), chatId, err)
		return nil, err
	}
	randomId, err := dao.NewQueryId()
	if err != nil {
		return nil, mtproto.ErrInternalServerError
	}
	return c.svcCtx.MsgSendMessageV2(c.ctx, &msgpb.TLMsgSendMessageV2{
		UserId:    c.MD.UserId,
		AuthKeyId: c.MD.PermAuthKeyId,
		PeerType:  mtproto.PEER_CHAT,
		PeerId:    chatId,
		Message: []*msgpb.OutboxMessage{msgpb.MakeTLOutboxMessage(&msgpb.OutboxMessage{
			NoWebpage: true,
			RandomId:  randomId,
			Message:   chat.MakeMessageService(c.MD.UserId, mtproto.MakeMessageActionChatAddUser(bot.Id())),
		}).To_OutboxMessage()},
	})
}

// requestedChannel checks a supergroup (requestPeerTypeChat) or a broadcast
// channel (requestPeerTypeBroadcast): the user is in it, its creator when
// the type asks, with the user_admin_rights; a username and a forum as the
// type asks. For bot_admin_rights the bot is made an admin with those
// rights on top of its own (biz channel.editAdmin, which also brings a bot
// in, rights.go EditAdmin); for bot_participant of a supergroup it is
// invited (biz channel.inviteToChannel). An empty bot_admin_rights asks for
// the bot in the chat, not for an admin without rights, which
// channel.editAdmin reads as a demotion.
func (c *BotsCore) requestedChannel(channelId int64, pt *mtproto.RequestPeerType, button *mtproto.KeyboardButton, bot *mtproto.ImmutableUser) (sharedPeer, []botStep, error) {
	mc, err := c.svcCtx.Channel.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: channelId, Id: []int64{c.MD.UserId, bot.Id()}})
	if errors.Is(err, mtproto.ErrChannelInvalid) || errors.Is(err, mtproto.ErrChannelPrivate) || errors.Is(err, mtproto.ErrPeerIdInvalid) {
		return sharedPeer{}, nil, mtproto.ErrPeerIdInvalid
	}
	if err != nil {
		return sharedPeer{}, nil, err
	}
	info := mc.GetChannel()
	me, member := mc.GetParticipant(c.MD.UserId)
	isGroup := pt.GetPredicateName() == mtproto.Predicate_requestPeerTypeChat
	if info.GetDeleted() || info.GetMonoforum() || !member || me.GetState() != channelperm.StateIn ||
		isGroup && !info.GetMegagroup() || !isGroup && !info.GetBroadcast() {
		return sharedPeer{}, nil, mtproto.ErrPeerIdInvalid
	}
	creator := me.GetRole() == channelperm.RoleCreator
	if pt.GetCreator() && !creator || !boolMatches(pt.GetHasUsername(), info.Public()) ||
		isGroup && !boolMatches(pt.GetForum(), info.GetForum()) || !holds(adminRightsOf(me), pt.GetUserAdminRights()) {
		return sharedPeer{}, nil, mtproto.ErrPeerIdInvalid
	}

	var steps []botStep
	botP, botMember := mc.GetParticipant(bot.Id())
	botIn := botMember && botP.GetState() == channelperm.StateIn
	want := int32(mtproto.MakeChatAdminRightsHelper(pt.GetBotAdminRights()))
	botHas := adminRightsOf(botP)
	if !botIn {
		botHas = 0
	}
	switch {
	case pt.GetBotAdminRights() != nil && want != 0 && (!botIn || botHas&want != want):
		rights := (botHas | want) & channelperm.AdminMask
		steps = append(steps, func() (*mtproto.Updates, error) {
			u, err := c.svcCtx.Channel.ChannelEditAdmin(c.ctx, &channelpb.TLChannelEditAdmin{
				SelfId:        c.MD.UserId,
				PermAuthKeyId: c.MD.PermAuthKeyId,
				ChannelId:     channelId,
				UserId:        bot.Id(),
				AdminRights:   rights,
				KeepRank:      mtproto.BoolTrue,
			})
			if err != nil {
				c.Logger.Errorf("messages.sendBotRequestedPeer - admin bot %d in channel %d: %v", bot.Id(), channelId, err)
				return nil, err
			}
			return u, nil
		})
	case !botIn && (isGroup && pt.GetBotParticipant() || pt.GetBotAdminRights() != nil):
		if isGroup && bot.BotNochats() {
			return sharedPeer{}, nil, mtproto.ErrBotGroupsBlocked
		}
		steps = append(steps, func() (*mtproto.Updates, error) {
			u, err := c.svcCtx.Channel.ChannelInviteToChannel(c.ctx, &channelpb.TLChannelInviteToChannel{
				SelfId:        c.MD.UserId,
				PermAuthKeyId: c.MD.PermAuthKeyId,
				ChannelId:     channelId,
				Id:            []int64{bot.Id()},
			})
			if err != nil {
				c.Logger.Errorf("messages.sendBotRequestedPeer - invite bot %d to channel %d: %v", bot.Id(), channelId, err)
				return nil, err
			}
			return u, nil
		})
	}

	rp := &mtproto.RequestedPeer{ChannelId: channelId}
	if button.GetNameRequested() {
		rp.Title = mtproto.MakeFlagsString(info.GetTitle())
	}
	if button.GetUsernameRequested() && info.Public() {
		rp.Username = mtproto.MakeFlagsString(info.GetUsername())
	}
	if photo := info.GetPhoto(); button.GetPhotoRequested() && photo.GetPredicateName() == mtproto.Predicate_photo {
		rp.Photo = photo
	}
	return sharedPeer{peer: mtproto.MakePeerChannel(channelId), sentMe: mtproto.MakeTLRequestedPeerChannel(rp).To_RequestedPeer()}, steps, nil
}

// adminRightsOf is the admin rights a participant of a channel holds: every
// one for the creator (channelperm.Grantable).
func adminRightsOf(p *channelpb.ImmutableChannelParticipant) int32 {
	switch p.GetRole() {
	case channelperm.RoleCreator:
		return channelperm.AdminMask
	case channelperm.RoleAdmin:
		return p.GetAdminRights()
	}
	return 0
}

// holds tells whether rights include every right wanted asks for; none
// asked is held by anybody.
func holds(rights int32, wanted *mtproto.ChatAdminRights) bool {
	w := int32(mtproto.MakeChatAdminRightsHelper(wanted))
	return rights&w == w
}

// mergeUpdates joins the updates of several changes into one answer, in
// their order.
func mergeUpdates(list ...*mtproto.Updates) *mtproto.Updates {
	var (
		updates []*mtproto.Update
		users   []*mtproto.User
		chats   []*mtproto.Chat
		date    int32
	)
	type chatKey struct {
		predicate string
		id        int64
	}
	seenChats := make(map[chatKey]bool)
	for _, u := range list {
		switch u.GetPredicateName() {
		case mtproto.Predicate_updates, mtproto.Predicate_updatesCombined:
			updates = append(updates, u.GetUpdates()...)
		case mtproto.Predicate_updateShort:
			updates = append(updates, u.GetUpdate())
		}
		users = append(users, u.GetUsers()...)
		for _, ch := range u.GetChats() {
			key := chatKey{ch.GetPredicateName(), ch.GetId()}
			if !seenChats[key] {
				seenChats[key] = true
				chats = append(chats, ch)
			}
		}
		date = max(date, u.GetDate())
	}
	return mtproto.MakeTLUpdates(&mtproto.Updates{
		Updates: updates,
		Users:   users,
		Chats:   chats,
		Date:    date,
		Seq:     0,
	}).To_Updates()
}

// withUsers fills the users of an answer as the caller sees them: biz
// channel leaves its answers' users to the caller (channel.tl.proto).
func (c *BotsCore) withUsers(r *mtproto.Updates) (*mtproto.Updates, error) {
	h := mtproto.NewIDListHelper(c.MD.UserId)
	h.AppendUsers(c.MD.UserId)
	h.PickByUpdates(r.GetUpdates()...)
	users, err := c.svcCtx.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{
		Id:      h.UserIdList,
		Privacy: true,
		HasTo:   true,
		To:      []int64{c.MD.UserId},
	})
	if err != nil {
		return nil, err
	}
	r.Users = users.GetUserListByIdList(c.MD.UserId, h.UserIdList...)
	return r, nil
}
