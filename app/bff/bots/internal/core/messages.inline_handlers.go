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
	"github.com/teamgram/teamgram-server/pkg/botgames"
	"github.com/teamgram/teamgram-server/pkg/botmarkup"
	"github.com/teamgram/teamgram-server/pkg/catalog/stickerlists"
	"github.com/teamgram/teamgram-server/pkg/inlinebots"
	"github.com/teamgram/teamgram-server/pkg/replyheader"
	"github.com/teamgram/teamgram-server/pkg/webfile"

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
	// The built-in bots @gif and @pic have no process: the server answers
	// for them (task 099, ADR 0062).
	if b, ok := inlinebots.ByID(bot.Id()); ok {
		return c.builtinResults(b, in)
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
	if a.SwitchWebview != nil {
		// The button's mini app opens only from results the user got
		// (messages.requestSimpleWebView, from_switch_webview).
		if err = c.svcCtx.RememberSwitchWebview(c.ctx, botId, c.MD.UserId, a.SwitchWebview.GetUrl(), a.CacheTime); err != nil {
			c.Logger.Errorf("messages.getInlineBotResults - switch_webview: %v", err)
			return nil, mtproto.ErrInternalServerError
		}
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
	var games map[string]dao.GameRef
	for _, r := range in.Results {
		if _, dup := ids[r.GetId()]; dup {
			return nil, mtproto.ErrResultIdDuplicate
		}
		ids[r.GetId()] = struct{}{}
		var (
			res *mtproto.BotInlineResult
			err error
		)
		if r.GetPredicateName() == mtproto.Predicate_inputBotInlineResultGame {
			var ref dao.GameRef
			if res, ref, err = c.inlineGameResult(r); err == nil {
				if games == nil {
					games = make(map[string]dao.GameRef)
				}
				games[r.GetId()] = ref
			}
		} else {
			res, err = c.inlineResult(r)
		}
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
		Games:         games,
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
	// Type "game" is a game's (inlineGameResult): tdesktop shows a result of
	// that type as a game (inline_bot_result.cpp:75), and sending one that
	// is not would fail (outOfResult).
	if r.GetType() == "game" {
		return nil, mtproto.ErrResultTypeInvalid
	}
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
			Thumb: webDocument(c.svcCtx.WebFileKey, r.GetThumb()), Content: webDocument(c.svcCtx.WebFileKey, r.GetContent()), SendMessage: send,
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
	// A game is a result of an inline query only (inlineGameResult).
	return nil, mtproto.ErrResultTypeInvalid
}

// inlineGameResult turns a game of the bot (inputBotInlineResultGame) into
// the result clients show: botInlineMediaResult of type "game" with the
// game's picture, animation, title and description, which tdesktop shows
// and sends as a game (inline_bot_result.cpp:75, 155-165, 188-192). Its
// message is inputBotInlineMessageGame, SEND_MESSAGE_TYPE_INVALID
// otherwise, and its keyboard starts with the game's button or has none
// (botgames.Check): the button goes in when a user sends the result, in
// the user's language. The game is a game of the bot, by its short name,
// MEDIA_INVALID for none. ref names the game for the send.
func (c *BotsCore) inlineGameResult(r *mtproto.InputBotInlineResult) (*mtproto.BotInlineResult, dao.GameRef, error) {
	m := r.GetSendMessage()
	if m.GetPredicateName() != mtproto.Predicate_inputBotInlineMessageGame {
		return nil, dao.GameRef{}, mtproto.ErrSendMessageTypeInvalid
	}
	env := c.markupEnv()
	env.Game = true
	markup, err := botmarkup.Normalize(m.GetReplyMarkup(), env)
	if err != nil {
		return nil, dao.GameRef{}, err
	}
	if err = botgames.Check(markup); err != nil {
		return nil, dao.GameRef{}, err
	}
	game, err := c.svcCtx.Bots.GameByShortName(c.ctx, c.MD.UserId, r.GetShortName())
	if err != nil {
		c.Logger.Errorf("messages.setInlineBotResults - game: %v", err)
		return nil, dao.GameRef{}, mtproto.ErrInternalServerError
	}
	if game == nil {
		return nil, dao.GameRef{}, mtproto.ErrMediaInvalid
	}
	tl, err := botgames.TL(c.ctx, c.svcCtx.MediaClient, game)
	if err != nil {
		c.Logger.Errorf("messages.setInlineBotResults - game: %v", err)
		return nil, dao.GameRef{}, mtproto.ErrInternalServerError
	}
	res := &mtproto.BotInlineResult{
		Id:       r.GetId(),
		Type:     "game",
		Photo:    tl.GetPhoto(),
		Document: tl.GetDocument(),
		Title:    wrapperspb.String(tl.GetTitle()),
		SendMessage: mtproto.MakeTLBotInlineMessageMediaAuto(&mtproto.BotInlineMessage{
			ReplyMarkup: markup,
		}).To_BotInlineMessage(),
	}
	if tl.GetDescription() != "" {
		res.Description = wrapperspb.String(tl.GetDescription())
	}
	return mtproto.MakeTLBotInlineMediaResult(res).To_BotInlineResult(), dao.GameRef{Id: game.Id, AccessHash: game.AccessHash}, nil
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

// webDocument is a thumbnail or content of a bot's result as clients get
// it: a webDocument whose access_hash is the server's signature of the
// address (webfile.Sign), which clients fetch through upload.getWebFile
// with inputWebFileLocation{url, access_hash} (core.telegram.org/api/files,
// "proxying the request through telegram"), so that the device never
// reaches the bot's host. Without a key it is webDocumentNoProxy, which
// clients fetch from the address themselves.
func webDocument(key []byte, d *mtproto.InputWebDocument) *mtproto.WebDocument {
	if d == nil {
		return nil
	}
	doc := &mtproto.WebDocument{
		Url: d.GetUrl(), Size2: d.GetSize2(), MimeType: d.GetMimeType(), Attributes: d.GetAttributes(),
	}
	if len(key) == 0 {
		return mtproto.MakeTLWebDocumentNoProxy(doc).To_WebDocument()
	}
	doc.AccessHash = webfile.Sign(key, d.GetUrl())
	return mtproto.MakeTLWebDocument(doc).To_WebDocument()
}

// inlineMessage turns what a result sends into its stored form. Web page,
// invoice and rich message results are refused: nothing here makes them;
// the message of a game is inlineGameResult's.
func (c *BotsCore) inlineMessage(m *mtproto.InputBotInlineMessage) (*mtproto.BotInlineMessage, error) {
	markup, err := botmarkup.Normalize(m.GetReplyMarkup(), c.markupEnv())
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
// game is the game of a game result, which is its media; a game result
// without it is not sent.
func outOfResult(r *mtproto.BotInlineResult, game *mtproto.Game) (*mtproto.Message, error) {
	send := r.GetSendMessage()
	out := mtproto.MakeTLMessage(&mtproto.Message{
		Out:         true,
		InvertMedia: send.GetInvertMedia(),
		Message:     send.GetMessage(),
		Entities:    send.GetEntities(),
		ReplyMarkup: send.GetReplyMarkup(),
	}).To_Message()
	if (game != nil) != (r.GetPredicateName() == mtproto.Predicate_botInlineMediaResult && r.GetType() == "game") {
		return nil, mtproto.ErrResultTypeInvalid
	}
	switch send.GetPredicateName() {
	case mtproto.Predicate_botInlineMessageMediaAuto:
		switch {
		case game != nil:
			out.Media = botgames.MessageMedia(game)
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

	// A game result sends the game the answer named (dao.GameRef): one
	// removed since is no longer sent.
	var game *mtproto.Game
	if ref, ok := answer.Games[in.Id]; ok {
		if game, err = c.inlineGame(ref); err != nil {
			c.Logger.Errorf("messages.sendInlineBotResult - error: %v", err)
			return nil, err
		}
	}

	out, updates, err := c.sendInlineResult(&inlineSend{
		game:         game,
		userId:       c.MD.UserId,
		authKeyId:    c.MD.PermAuthKeyId,
		peer:         peer,
		botId:        answer.Bot,
		result:       result,
		hideVia:      in.HideVia,
		silent:       in.Silent,
		background:   in.Background,
		randomId:     in.RandomId,
		scheduleDate: in.ScheduleDate,
		replyTo:      in.GetReplyTo(),
		sendAs:       in.GetSendAs(),
		shortcut:     in.GetQuickReplyShortcut(),
	})
	if err != nil {
		c.Logger.Errorf("messages.sendInlineBotResult - error: %v", err)
		return nil, err
	}
	c.addRecentSticker(out.GetMedia())
	c.countGifSend(answer.Bot, out.GetMedia())
	// A result kept in a quick reply shortcut leaves the draft of Saved
	// Messages alone, as messages.sendMessage does (task 105).
	if in.ClearDraft && in.GetQuickReplyShortcut() == nil {
		ctx := contextx.ValueOnlyFrom(c.ctx)
		threading.GoSafe(func() { c.clearDraft(ctx, peer, in.GetReplyTo().GetTopMsgId().GetValue()) })
	}
	return updates, nil
}

// inlineSend is an inline result a user sends to a chat: a result the user
// chose (messages.sendInlineBotResult) or the answer of a bot to the user's
// web view (messages.sendWebViewResultMessage).
type inlineSend struct {
	// userId sends the message; authKeyId is the session the reply goes
	// to, which the push leaves out (0: every session of the user).
	userId    int64
	authKeyId int64
	peer      *mtproto.PeerUtil
	// botId is the bot of the result, via_bot_id of the message.
	botId  int64
	result *mtproto.BotInlineResult
	// game is the game of a game result (task 106).
	game *mtproto.Game
	// hideVia drops via_bot_id for a built-in bot.
	hideVia      bool
	silent       bool
	background   bool
	randomId     int64
	scheduleDate *wrapperspb.Int32Value
	replyTo      *mtproto.InputReplyTo
	sendAs       *mtproto.InputPeer
	// shortcut keeps the message in a quick reply shortcut of the user
	// instead of sending it (task 105).
	shortcut *mtproto.InputQuickReplyShortcut
}

// sendInlineResult sends the message of an inline result from s.userId to
// s.peer through msg, and returns the message and the updates of the send.
// With s.shortcut the message goes to that quick reply shortcut of the
// user instead (task 105, core.telegram.org/api/business: messages are
// added "to inputPeerSelf, specifying the shortcut"), as iOS sends a result
// in the shortcut's editor (PendingMessageManager.swift:2093).
func (c *BotsCore) sendInlineResult(s *inlineSend) (*mtproto.Message, *mtproto.Updates, error) {
	if s.shortcut != nil {
		switch {
		case c.MD.IsBot:
			return nil, nil, mtproto.ErrBotMethodInvalid
		case !s.peer.IsSelfUser(s.userId):
			return nil, nil, mtproto.ErrPeerIdInvalid
		case s.scheduleDate != nil:
			// A shortcut has no time to go out at (not verified which error
			// Telegram answers).
			return nil, nil, mtproto.ErrScheduleDateInvalid
		}
	}
	// The peer the message comes from: send_as decides it as for
	// messages.sendMessage (pkg/sendas), and msg checks it again with the
	// channel locked (task 081).
	from, err := c.svcCtx.SendAs.From(c.ctx, s.userId, s.peer, s.sendAs)
	if err != nil {
		return nil, nil, err
	}

	out, err := outOfResult(s.result, s.game)
	if err != nil {
		return nil, nil, err
	}
	if s.game != nil {
		// The button that starts the game, in the sender's language when
		// the bot gave none (botgames.Markup).
		if out.ReplyMarkup, err = botgames.Markup(out.ReplyMarkup, s.game.GetTitle(), c.MD.LangCode); err != nil {
			return nil, nil, err
		}
	}
	out.Date = int32(time.Now().Unix())
	out.FromId = from
	out.PeerId = s.peer.ToPeer()
	if s.peer.IsSelfUser(s.userId) && s.shortcut == nil {
		// The sub-chat of Saved Messages, as messages.sendMessage writes it.
		out.SavedPeerId = s.peer.ToPeer()
	}
	out.Silent = s.silent
	// core.telegram.org/api/bots/inline: the message has via_bot_id, and
	// clients show the username of the bot in its header. hide_via may
	// drop it "only for bot usernames encountered in the config"
	// (core.telegram.org/method/messages.sendInlineBotResult): the
	// built-in bots of help.getConfig, whose GIFs the emoji panel sends
	// with it (Android ChatActivityEnterView.java:12491-12509, iOS
	// ChatController.swift:2820, tdesktop gifs_list_widget.cpp:545).
	// Another bot keeps it.
	if !s.hideVia || !inlinebots.IsBuiltin(s.botId) {
		out.ViaBotId = wrapperspb.Int64(s.botId)
	}
	// The same reply as messages.sendMessage makes: the clients fill reply_to
	// of both alike (tdesktop data_histories.cpp ReplyToForMTP, Android
	// SendMessagesHelper.createReplyInput).
	// A message of a shortcut answers nothing: it goes to another chat later.
	if s.shortcut == nil {
		if out.ReplyTo, err = replyheader.Make(replySource{c: c, userId: s.userId}, s.userId, s.peer, nil, s.replyTo); err != nil {
			return nil, nil, err
		}
		out.ReplyTo = replyheader.WithTopic(out.ReplyTo, s.peer, s.replyTo)
	}

	outbox := msgpb.MakeTLOutboxMessage(&msgpb.OutboxMessage{
		NoWebpage:    s.result.GetSendMessage().GetNoWebpage(),
		Background:   s.background,
		RandomId:     s.randomId,
		Message:      out,
		ScheduleDate: s.scheduleDate,
	}).To_OutboxMessage()
	var updates *mtproto.Updates
	if s.shortcut != nil {
		updates, err = c.svcCtx.MsgAddQuickReplyMessages(c.ctx, &msgpb.TLMsgAddQuickReplyMessages{
			UserId:    s.userId,
			AuthKeyId: s.authKeyId,
			Shortcut:  s.shortcut,
			Message:   []*msgpb.OutboxMessage{outbox},
		})
	} else {
		updates, err = c.svcCtx.MsgSendMessageV2(c.ctx, &msgpb.TLMsgSendMessageV2{
			UserId:    s.userId,
			AuthKeyId: s.authKeyId,
			PeerType:  s.peer.PeerType,
			PeerId:    s.peer.PeerId,
			Message:   []*msgpb.OutboxMessage{outbox},
		})
	}
	if err != nil {
		return nil, nil, err
	}
	return out, updates, nil
}

// countGifSend counts a GIF of @gif's results sent: the popular GIFs of
// its empty query are the ones sent most. The message is sent by then: a
// failure is logged, the send is only missing from the count.
func (c *BotsCore) countGifSend(botId int64, media *mtproto.MessageMedia) {
	if botId != inlinebots.Gif.Id || media.GetPredicateName() != mtproto.Predicate_messageMediaDocument || media.GetDocument().GetId() == 0 {
		return
	}
	if err := c.svcCtx.GifSends.CountGifSend(c.ctx, media.GetDocument().GetId()); err != nil {
		c.Logger.Errorf("messages.sendInlineBotResult - count of a @gif send: %v", err)
	}
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
