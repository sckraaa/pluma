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
	"time"
	"unicode/utf8"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/dao"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/botmarkup"
	"github.com/teamgram/teamgram-server/pkg/catalog/stickerlists"
	"github.com/teamgram/teamgram-server/pkg/replyheader"

	"github.com/zeromicro/go-zero/core/contextx"
	"github.com/zeromicro/go-zero/core/threading"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// Limits of core.telegram.org/bots/api#answerinlinequery and
// core.telegram.org/method/messages.setInlineBotResults.
const (
	maxInlineResults = 50
	maxNextOffset    = 64
	// maxInlineQuery: bots/api#inlinequery, query "up to 256 characters".
	// messages.getInlineBotResults lists no error for a longer one, so
	// MESSAGE_TOO_LONG is this server's choice.
	maxInlineQuery = 256
)

// inlinePeerType is the peer_type of updateBotInlineQuery and a key part of
// the cache: inlineQueryPeerType of the chat the query is made in. A query
// from no chat the server knows has none, which the update allows: clients
// send inputPeerEmpty from a secret chat (Android MentionsAdapter.java:925)
// and with no recipient (tdesktop location_picker.cpp:1230).
func (c *BotsCore) inlinePeerType(botId int64, peer *mtproto.PeerUtil) (*mtproto.InlineQueryPeerType, string, error) {
	switch {
	case peer.PeerType == mtproto.PEER_EMPTY:
		return nil, "none", nil
	case peer.IsUser() && peer.PeerId == botId:
		return mtproto.MakeTLInlineQueryPeerTypeSameBotPM(nil).To_InlineQueryPeerType(), "same_bot", nil
	case peer.IsUser():
		u, err := c.svcCtx.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{Id: peer.PeerId})
		if err != nil {
			return nil, "", err
		}
		if u.IsBot() {
			return mtproto.MakeTLInlineQueryPeerTypeBotPM(nil).To_InlineQueryPeerType(), "bot", nil
		}
		return mtproto.MakeTLInlineQueryPeerTypePM(nil).To_InlineQueryPeerType(), "pm", nil
	case peer.IsChat():
		return mtproto.MakeTLInlineQueryPeerTypeChat(nil).To_InlineQueryPeerType(), "chat", nil
	case peer.IsChannel():
		mc, err := c.svcCtx.Channel.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: peer.PeerId, Id: []int64{c.MD.UserId}})
		if err != nil {
			return nil, "", err
		}
		if mc.GetChannel().GetBroadcast() {
			return mtproto.MakeTLInlineQueryPeerTypeBroadcast(nil).To_InlineQueryPeerType(), "broadcast", nil
		}
		return mtproto.MakeTLInlineQueryPeerTypeMegagroup(nil).To_InlineQueryPeerType(), "megagroup", nil
	}
	return nil, "", mtproto.ErrPeerIdInvalid
}

// MessagesGetInlineBotResults
// messages.getInlineBotResults#514e999d flags:# bot:InputUser peer:InputPeer geo_point:flags.0?InputGeoPoint query:string offset:string = messages.BotResults;
func (c *BotsCore) MessagesGetInlineBotResults(in *mtproto.TLMessagesGetInlineBotResults) (*mtproto.Messages_BotResults, error) {
	target := mtproto.FromInputUser(c.MD.UserId, in.Bot)
	if !target.IsUser() {
		return nil, mtproto.ErrBotInvalid
	}
	bot, err := c.botUser(target.PeerId)
	if err != nil {
		return nil, err
	}
	if bot.BotInlinePlaceholder() == nil {
		return nil, mtproto.ErrBotInlineDisabled
	}
	// Each query is written to the bot's journal: it cannot be of any size.
	if utf8.RuneCountInString(in.Query) > maxInlineQuery {
		return nil, mtproto.ErrMessageTooLong
	}
	if len(in.Offset) > maxNextOffset {
		return nil, mtproto.ErrNextOffsetInvalid
	}
	peerType, kind, err := c.inlinePeerType(bot.Id(), mtproto.FromInputPeer2(c.MD.UserId, in.Peer))
	if err != nil {
		return nil, err
	}

	geo := in.GeoPoint
	if !bot.BotInlineGeo() {
		geo = nil
	}
	// A cached answer: the user's own first (private), then the shared one.
	for _, userId := range []int64{c.MD.UserId, 0} {
		q, err := c.svcCtx.CachedInlineQuery(c.ctx, dao.InlineCacheKey(bot.Id(), userId, kind, geo, in.Query, in.Offset))
		if err != nil {
			c.Logger.Errorf("messages.getInlineBotResults - error: %v", err)
			return nil, mtproto.ErrInternalServerError
		}
		if q == 0 {
			continue
		}
		a, err := c.svcCtx.InlineAnswer(c.ctx, q)
		if err != nil {
			c.Logger.Errorf("messages.getInlineBotResults - error: %v", err)
			return nil, mtproto.ErrInternalServerError
		}
		if a != nil {
			return c.inlineResults(q, a, bot.Id())
		}
	}

	if !c.svcCtx.EnterWait(c.MD.UserId) {
		return nil, mtproto.ErrBotResponseTimeout
	}
	defer c.svcCtx.LeaveWait(c.MD.UserId)
	queryId, err := dao.NewQueryId()
	if err != nil {
		return nil, mtproto.ErrInternalServerError
	}
	if err = c.svcCtx.BeginInline(c.ctx, queryId, bot.Id(), c.MD.UserId); err != nil {
		c.Logger.Errorf("messages.getInlineBotResults - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	u := mtproto.MakeTLUpdateBotInlineQuery(&mtproto.Update{
		QueryId_INT64: queryId,
		UserId:        c.MD.UserId,
		Query:         in.Query,
		PeerType:      peerType,
		Offset:        in.Offset,
	}).To_Update()
	if geo != nil {
		u.Geo = mtproto.MakeGeoPointByInput(geo)
	}
	users, err := c.usersFor(bot.Id(), c.MD.UserId)
	if err != nil {
		return nil, err
	}
	if err = c.svcCtx.PushToBot(c.ctx, bot.Id(), u, users, nil); err != nil {
		c.Logger.Errorf("messages.getInlineBotResults - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}

	answer, err := c.svcCtx.WaitInlineAnswer(c.ctx, queryId)
	if err != nil {
		c.Logger.Errorf("messages.getInlineBotResults - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	if answer == nil {
		return nil, mtproto.ErrBotResponseTimeout
	}
	cacheFor := int64(0)
	if answer.Private {
		cacheFor = c.MD.UserId
	}
	if err = c.svcCtx.CacheInline(c.ctx, dao.InlineCacheKey(bot.Id(), cacheFor, kind, geo, in.Query, in.Offset), queryId, answer.CacheTime); err != nil {
		// The cache spares the bot a repeat of the query; the answer stands.
		c.Logger.Errorf("messages.getInlineBotResults - cache: %v", err)
	}
	return c.inlineResults(queryId, answer, bot.Id())
}

func (c *BotsCore) inlineResults(queryId int64, a *dao.InlineAnswer, botId int64) (*mtproto.Messages_BotResults, error) {
	users, err := c.usersFor(c.MD.UserId, botId)
	if err != nil {
		return nil, err
	}
	r := &mtproto.Messages_BotResults{
		Gallery:       a.Gallery,
		QueryId:       queryId,
		SwitchPm:      a.SwitchPm,
		SwitchWebview: a.SwitchWebview,
		Results:       a.Results,
		CacheTime:     a.CacheTime,
		Users:         users,
	}
	if a.NextOffset != "" {
		r.NextOffset = mtproto.MakeFlagsString(a.NextOffset)
	}
	if r.Results == nil {
		r.Results = []*mtproto.BotInlineResult{}
	}
	return mtproto.MakeTLMessagesBotResults(r).To_Messages_BotResults(), nil
}

// MessagesSetInlineBotResults
// messages.setInlineBotResults#bb12a419 flags:# gallery:flags.0?true private:flags.1?true query_id:long results:Vector<InputBotInlineResult> cache_time:int next_offset:flags.2?string switch_pm:flags.3?InlineBotSwitchPM switch_webview:flags.4?InlineBotWebView = Bool;
func (c *BotsCore) MessagesSetInlineBotResults(in *mtproto.TLMessagesSetInlineBotResults) (*mtproto.Bool, error) {
	if _, err := c.meBot(); err != nil {
		return nil, err
	}
	if len(in.Results) > maxInlineResults {
		return nil, mtproto.ErrResultsTooMuch
	}
	if in.NextOffset != nil && len(in.NextOffset.GetValue()) > maxNextOffset {
		return nil, mtproto.ErrNextOffsetInvalid
	}
	if in.SwitchPm != nil && in.SwitchPm.GetText() == "" {
		return nil, mtproto.ErrSwitchPmTextEmpty
	}
	ids := make(map[string]struct{}, len(in.Results))
	results := make([]*mtproto.BotInlineResult, 0, len(in.Results))
	for _, r := range in.Results {
		if _, dup := ids[r.GetId()]; dup {
			return nil, mtproto.ErrResultIdDuplicate
		}
		ids[r.GetId()] = struct{}{}
		res, err := c.inlineResult(r)
		if err != nil {
			return nil, err
		}
		results = append(results, res)
	}
	a := &dao.InlineAnswer{
		Results:       results,
		Gallery:       in.Gallery,
		SwitchPm:      in.SwitchPm,
		SwitchWebview: in.SwitchWebview,
		CacheTime:     in.CacheTime,
		Private:       in.Private,
		Bot:           c.MD.UserId,
	}
	if in.NextOffset != nil {
		a.NextOffset = in.NextOffset.GetValue()
	}
	ok, err := c.svcCtx.SetInlineAnswer(c.ctx, in.QueryId, c.MD.UserId, a)
	if err != nil {
		c.Logger.Errorf("messages.setInlineBotResults - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	if !ok {
		return nil, mtproto.ErrQueryIdInvalid
	}
	return mtproto.BoolTrue, nil
}

// inlineResult turns a result of the bot into the one clients show. An
// article with a thumbnail and a text is what most bots send; a result with
// content at a URL and no media already on the server cannot be sent as
// media, because the server does not fetch URLs, and is refused.
func (c *BotsCore) inlineResult(r *mtproto.InputBotInlineResult) (*mtproto.BotInlineResult, error) {
	send, err := c.inlineMessage(r.GetSendMessage())
	if err != nil {
		return nil, err
	}
	switch r.GetPredicateName() {
	case mtproto.Predicate_inputBotInlineResult:
		if r.GetContent() != nil && send.GetPredicateName() == mtproto.Predicate_botInlineMessageMediaAuto {
			return nil, mtproto.ErrSendMessageMediaInvalid
		}
		return mtproto.MakeTLBotInlineResult(&mtproto.BotInlineResult{
			Id: r.GetId(), Type: r.GetType(), Title: r.GetTitle(), Description: r.GetDescription(), Url: r.GetUrl(),
			Thumb: webDocument(r.GetThumb()), Content: webDocument(r.GetContent()), SendMessage: send,
		}).To_BotInlineResult(), nil
	case mtproto.Predicate_inputBotInlineResultPhoto:
		// The access hash says the bot was given the file; only with it
		// does the answer hand the file out to others.
		photo, err := c.svcCtx.MediaGetPhoto(c.ctx, &media.TLMediaGetPhoto{PhotoId: r.GetPhoto().GetId()})
		if err != nil || photo.GetPredicateName() != mtproto.Predicate_photo || photo.GetAccessHash() != r.GetPhoto().GetAccessHash() {
			return nil, mtproto.ErrPhotoInvalid
		}
		return mtproto.MakeTLBotInlineMediaResult(&mtproto.BotInlineResult{Id: r.GetId(), Type: r.GetType(), Photo: photo, SendMessage: send}).To_BotInlineResult(), nil
	case mtproto.Predicate_inputBotInlineResultDocument:
		document, err := c.svcCtx.MediaGetDocument(c.ctx, &media.TLMediaGetDocument{Id: r.GetDocument().GetId()})
		if err != nil || document.GetPredicateName() != mtproto.Predicate_document || document.GetAccessHash() != r.GetDocument().GetAccessHash() {
			return nil, mtproto.ErrDocumentInvalid
		}
		return mtproto.MakeTLBotInlineMediaResult(&mtproto.BotInlineResult{Id: r.GetId(), Type: r.GetType(), Document: document, Title: r.GetTitle(), Description: r.GetDescription(), SendMessage: send}).To_BotInlineResult(), nil
	}
	// Games are not served.
	return nil, mtproto.ErrResultTypeInvalid
}

// resolveUser is the user an input button of a bot names, 0 for nobody.
func (c *BotsCore) resolveUser(in *mtproto.InputUser) int64 {
	u := mtproto.FromInputUser(c.MD.UserId, in)
	if !u.IsUser() {
		return 0
	}
	if _, err := c.svcCtx.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{Id: u.PeerId}); err != nil {
		return 0
	}
	return u.PeerId
}

func webDocument(d *mtproto.InputWebDocument) *mtproto.WebDocument {
	if d == nil {
		return nil
	}
	return mtproto.MakeTLWebDocumentNoProxy(&mtproto.WebDocument{
		Url: d.GetUrl(), Size2: d.GetSize2(), MimeType: d.GetMimeType(), Attributes: d.GetAttributes(),
	}).To_WebDocument()
}

// inlineMessage turns what a result sends into its stored form. Web page,
// invoice, game and rich message results are refused: nothing here makes
// them.
func (c *BotsCore) inlineMessage(m *mtproto.InputBotInlineMessage) (*mtproto.BotInlineMessage, error) {
	markup, err := botmarkup.Normalize(m.GetReplyMarkup(), c.resolveUser)
	if err != nil {
		return nil, err
	}
	switch m.GetPredicateName() {
	case mtproto.Predicate_inputBotInlineMessageMediaAuto:
		return mtproto.MakeTLBotInlineMessageMediaAuto(&mtproto.BotInlineMessage{InvertMedia: m.GetInvertMedia(), Message: m.GetMessage(), Entities: m.GetEntities(), ReplyMarkup: markup}).To_BotInlineMessage(), nil
	case mtproto.Predicate_inputBotInlineMessageText:
		if m.GetMessage() == "" {
			return nil, mtproto.ErrMessageEmpty
		}
		return mtproto.MakeTLBotInlineMessageText(&mtproto.BotInlineMessage{NoWebpage: m.GetNoWebpage(), InvertMedia: m.GetInvertMedia(), Message: m.GetMessage(), Entities: m.GetEntities(), ReplyMarkup: markup}).To_BotInlineMessage(), nil
	case mtproto.Predicate_inputBotInlineMessageMediaGeo:
		return mtproto.MakeTLBotInlineMessageMediaGeo(&mtproto.BotInlineMessage{Geo: mtproto.MakeGeoPointByInput(m.GetGeoPoint()), Heading: m.GetHeading(), Period: m.GetPeriod(), ProximityNotificationRadius: m.GetProximityNotificationRadius(), ReplyMarkup: markup}).To_BotInlineMessage(), nil
	case mtproto.Predicate_inputBotInlineMessageMediaVenue:
		return mtproto.MakeTLBotInlineMessageMediaVenue(&mtproto.BotInlineMessage{Geo: mtproto.MakeGeoPointByInput(m.GetGeoPoint()), Title: m.GetTitle(), Address: m.GetAddress(), Provider: m.GetProvider(), VenueId: m.GetVenueId(), VenueType: m.GetVenueType(), ReplyMarkup: markup}).To_BotInlineMessage(), nil
	case mtproto.Predicate_inputBotInlineMessageMediaContact:
		return mtproto.MakeTLBotInlineMessageMediaContact(&mtproto.BotInlineMessage{PhoneNumber: m.GetPhoneNumber(), FirstName: m.GetFirstName(), LastName: m.GetLastName(), Vcard: m.GetVcard(), ReplyMarkup: markup}).To_BotInlineMessage(), nil
	}
	return nil, mtproto.ErrSendMessageTypeInvalid
}

// outOfResult builds the message a chosen result sends: text and markup, and
// the media of the result (its photo or document) or of the message itself.
func outOfResult(r *mtproto.BotInlineResult) (*mtproto.Message, error) {
	send := r.GetSendMessage()
	out := mtproto.MakeTLMessage(&mtproto.Message{
		Out:         true,
		InvertMedia: send.GetInvertMedia(),
		Message:     send.GetMessage(),
		Entities:    send.GetEntities(),
		ReplyMarkup: send.GetReplyMarkup(),
	}).To_Message()
	switch send.GetPredicateName() {
	case mtproto.Predicate_botInlineMessageMediaAuto:
		switch {
		case r.GetPhoto() != nil:
			out.Media = mtproto.MakeTLMessageMediaPhoto(&mtproto.MessageMedia{Photo_FLAGPHOTO: r.GetPhoto()}).To_MessageMedia()
		case r.GetDocument() != nil:
			out.Media = mtproto.MakeTLMessageMediaDocument(&mtproto.MessageMedia{Document: r.GetDocument()}).To_MessageMedia()
		default:
			return nil, mtproto.ErrSendMessageMediaInvalid
		}
	case mtproto.Predicate_botInlineMessageText:
	case mtproto.Predicate_botInlineMessageMediaGeo:
		if send.GetPeriod() != nil {
			out.Media = mtproto.MakeTLMessageMediaGeoLive(&mtproto.MessageMedia{Geo: send.GetGeo(), Heading: send.GetHeading(), Period: send.GetPeriod().GetValue(), ProximityNotificationRadius: send.GetProximityNotificationRadius()}).To_MessageMedia()
		} else {
			out.Media = mtproto.MakeTLMessageMediaGeo(&mtproto.MessageMedia{Geo: send.GetGeo()}).To_MessageMedia()
		}
	case mtproto.Predicate_botInlineMessageMediaVenue:
		out.Media = mtproto.MakeTLMessageMediaVenue(&mtproto.MessageMedia{Geo: send.GetGeo(), Title: send.GetTitle(), Address: send.GetAddress(), Provider: send.GetProvider(), VenueId: send.GetVenueId(), VenueType: send.GetVenueType()}).To_MessageMedia()
	case mtproto.Predicate_botInlineMessageMediaContact:
		out.Media = mtproto.MakeTLMessageMediaContact(&mtproto.MessageMedia{PhoneNumber: send.GetPhoneNumber(), FirstName: send.GetFirstName(), LastName: send.GetLastName(), Vcard: send.GetVcard()}).To_MessageMedia()
	default:
		return nil, mtproto.ErrSendMessageTypeInvalid
	}
	return out, nil
}

// MessagesSendInlineBotResult
// messages.sendInlineBotResult#c0cf7646 flags:# silent:flags.5?true background:flags.6?true clear_draft:flags.7?true hide_via:flags.11?true peer:InputPeer reply_to:flags.0?InputReplyTo random_id:long query_id:long id:string schedule_date:flags.10?int send_as:flags.13?InputPeer quick_reply_shortcut:flags.17?InputQuickReplyShortcut allow_paid_stars:flags.21?long = Updates;
func (c *BotsCore) MessagesSendInlineBotResult(in *mtproto.TLMessagesSendInlineBotResult) (*mtproto.Updates, error) {
	peer := c.chatPeer(in.Peer)
	if !peer.IsUser() && !peer.IsChat() && !peer.IsChannel() {
		return nil, mtproto.ErrPeerIdInvalid
	}
	answer, err := c.svcCtx.InlineAnswer(c.ctx, in.QueryId)
	if err != nil {
		c.Logger.Errorf("messages.sendInlineBotResult - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	if answer == nil {
		return nil, mtproto.ErrQueryIdInvalid
	}
	var result *mtproto.BotInlineResult
	for _, r := range answer.Results {
		if r.GetId() == in.Id {
			result = r
			break
		}
	}
	if result == nil {
		return nil, mtproto.ErrResultIdInvalid
	}

	// The peer the message comes from: send_as decides it as for
	// messages.sendMessage (pkg/sendas), and msg checks it again with the
	// channel locked (task 081).
	from, err := c.svcCtx.SendAs.From(c.ctx, c.MD.UserId, peer, in.GetSendAs())
	if err != nil {
		c.Logger.Errorf("messages.sendInlineBotResult - error: %v", err)
		return nil, err
	}

	out, err := outOfResult(result)
	if err != nil {
		return nil, err
	}
	out.Date = int32(time.Now().Unix())
	out.FromId = from
	out.PeerId = peer.ToPeer()
	if peer.IsSelfUser(c.MD.UserId) {
		// The sub-chat of Saved Messages, as messages.sendMessage writes it.
		out.SavedPeerId = peer.ToPeer()
	}
	out.Silent = in.Silent
	// core.telegram.org/api/bots/inline: the message has via_bot_id, and
	// clients show the username of the bot in its header. hide_via is not
	// honored: no bot here has the right to hide itself.
	out.ViaBotId = wrapperspb.Int64(answer.Bot)
	// The same reply as messages.sendMessage makes: the clients fill reply_to
	// of both alike (tdesktop data_histories.cpp ReplyToForMTP, Android
	// SendMessagesHelper.createReplyInput).
	if out.ReplyTo, err = replyheader.Make(replySource{c: c}, c.MD.UserId, peer, nil, in.GetReplyTo()); err != nil {
		c.Logger.Errorf("messages.sendInlineBotResult - error: %v", err)
		return nil, err
	}
	out.ReplyTo = replyheader.WithTopic(out.ReplyTo, peer, in.GetReplyTo())

	updates, err := c.svcCtx.MsgSendMessageV2(c.ctx, &msgpb.TLMsgSendMessageV2{
		UserId:    c.MD.UserId,
		AuthKeyId: c.MD.PermAuthKeyId,
		PeerType:  peer.PeerType,
		PeerId:    peer.PeerId,
		Message: []*msgpb.OutboxMessage{msgpb.MakeTLOutboxMessage(&msgpb.OutboxMessage{
			NoWebpage:    result.GetSendMessage().GetNoWebpage(),
			Background:   in.Background,
			RandomId:     in.RandomId,
			Message:      out,
			ScheduleDate: in.ScheduleDate,
		}).To_OutboxMessage()},
	})
	if err != nil {
		c.Logger.Errorf("messages.sendInlineBotResult - error: %v", err)
		return nil, err
	}
	c.addRecentSticker(out.GetMedia())
	if in.ClearDraft {
		ctx := contextx.ValueOnlyFrom(c.ctx)
		threading.GoSafe(func() { c.clearDraft(ctx, peer, in.GetReplyTo().GetTopMsgId().GetValue()) })
	}
	return updates, nil
}

// sentSticker is the sticker a message of an inline result sent, 0 for
// none.
func sentSticker(media *mtproto.MessageMedia) int64 {
	if media.GetPredicateName() != mtproto.Predicate_messageMediaDocument || stickerlists.StickerAttribute(media.GetDocument()) == nil {
		return 0
	}
	return media.GetDocument().GetId()
}

// addRecentSticker puts the sticker an inline result sent at the top of the
// account's recent stickers, as messages.sendMedia does for one sent by
// itself (task 081): Android and iOS put every sticker message the account
// sends, whatever method sent it, at the top of their own list
// (MessagesController.updateInterfaceWithMessages, MessagesController.java:
// 22010; SendMessagesHelper.java:9071; iOS StateReplay+Messages.swift:
// 201-218), and take the server's list when its hash differs, so the list
// keeps the sticker only if the server adds it too. The message is sent
// by then: a failure is logged, the sticker is only missing from the list.
func (c *BotsCore) addRecentSticker(media *mtproto.MessageMedia) {
	if c.MD.IsBot {
		return
	}
	if id := sentSticker(media); id != 0 {
		if err := c.svcCtx.StickerLists.Add(c.ctx, c.MD.UserId, stickerlists.Recent, id); err != nil {
			c.Logger.Errorf("recent stickers - sticker %d: %v", id, err)
		}
	}
}

// clearDraft removes the draft of the chat (clear_draft), as sendMessage does.
func (c *BotsCore) clearDraft(ctx context.Context, peer *mtproto.PeerUtil, topMsgId int32) {
	var (
		rV  *mtproto.Bool
		err error
	)
	if topMsgId != 0 {
		rV, err = c.svcCtx.DialogSaveThreadDraft(ctx, &dialog.TLDialogSaveThreadDraft{UserId: c.MD.UserId, PeerType: peer.PeerType, PeerId: peer.PeerId, TopMsgId: topMsgId, Message: mtproto.MakeTLDraftMessageEmpty(nil).To_DraftMessage()})
	} else {
		rV, err = c.svcCtx.DialogClearDraftMessage(ctx, &dialog.TLDialogClearDraftMessage{UserId: c.MD.UserId, PeerType: peer.PeerType, PeerId: peer.PeerId})
	}
	if err != nil {
		c.Logger.Errorf("clear draft - error: %v", err)
		return
	}
	if !mtproto.FromBool(rV) {
		return
	}
	draft := mtproto.MakeTLUpdateDraftMessage(&mtproto.Update{
		Peer_PEER:          peer.ToPeer(),
		TopMsgId_FLAGINT32: mtproto.MakeFlagsInt32(topMsgId),
		Draft:              mtproto.MakeTLDraftMessageEmpty(&mtproto.DraftMessage{Date_FLAGINT32: mtproto.MakeFlagsInt32(int32(time.Now().Unix()))}).To_DraftMessage(),
	}).To_Update()
	if _, err = c.svcCtx.SyncUpdatesNotMe(ctx, &sync.TLSyncUpdatesNotMe{UserId: c.MD.UserId, PermAuthKeyId: c.MD.PermAuthKeyId, Updates: mtproto.MakeUpdatesByUpdates(draft)}); err != nil {
		c.Logger.Errorf("clear draft - error: %v", err)
	}
}
