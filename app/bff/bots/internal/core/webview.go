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
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/dao"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/botkeys"
	"github.com/teamgram/teamgram-server/pkg/botstore"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
	"github.com/teamgram/teamgram-server/pkg/webappdata"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// The mini apps of bots (task 106, ADR 0068 p. 4). messages.requestWebView,
// requestSimpleWebView, requestMainWebView and requestAppWebView answer
// webViewResultUrl: the address of the app with the launch parameters in
// its fragment, which telegram-web-app.js reads (tgWebAppData,
// tgWebAppVersion, tgWebAppPlatform, tgWebAppThemeParams, ...). The clients
// open the address as it is and build none of it (tweb only rewrites
// tgWebAppVersion=8.0, src/components/webApp.tsx:252-254), so the server
// builds the fragment. tgWebAppData is the initData, signed for the bot
// with its web app key (pkg/webappdata).

// Limits of the parameters of a web view.
const (
	// maxWebViewPlatform: platform is 0 to 64 letters, digits and _
	// (task 106, "Пределы").
	maxWebViewPlatform = 64
	// maxThemeParams bounds the JSON of theme_params; the clients send
	// some 15 colors (Android BotWebViewSheet.makeThemeParams, iOS
	// WebAppController.swift:127-145), well under 1 KiB.
	maxThemeParams = 4096
	// maxThemeKeys bounds the colors of theme_params.
	maxThemeKeys = 64
	// maxWebAppStartParam bounds start_param of a mini app (task 106,
	// "Пределы"); the characters are those of /start's (startParamRe).
	maxWebAppStartParam = 512
	// maxWebAppURL bounds the address of a mini app, as bot_menu_buttons
	// and bot_apps keep it.
	maxWebAppURL = 1024
)

var (
	platformRe = regexp.MustCompile(`^[A-Za-z0-9_]*$`)
	themeKeyRe = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)
	hexColorRe = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)
	rgbColorRe = regexp.MustCompile(`^rgba?\(\s*(\d{1,3})\s*,\s*(\d{1,3})\s*,\s*(\d{1,3})\s*(,\s*[0-9.]+\s*)?\)$`)
)

// webViewParams are the parameters every request of a web view has.
type webViewParams struct {
	platform   string // as the client names itself; "unknown" when it does not
	theme      string // tgWebAppThemeParams; "" for none
	startParam string // "" for none
	fullscreen bool
}

// webViewParamsOf checks the parameters of a request of a web view.
func webViewParamsOf(platform string, theme *mtproto.DataJSON, startParam *wrapperspb.StringValue, fullscreen bool) (webViewParams, error) {
	if len(platform) > maxWebViewPlatform || !platformRe.MatchString(platform) {
		return webViewParams{}, mtproto.ErrPlatformInvalid
	}
	if platform == "" {
		// The platform telegram-web-app.js assumes without one.
		platform = "unknown"
	}
	p := webViewParams{platform: platform, fullscreen: fullscreen}
	var err error
	if p.theme, err = themeParamsOf(theme); err != nil {
		return webViewParams{}, err
	}
	if startParam != nil && startParam.GetValue() != "" {
		s := startParam.GetValue()
		if len(s) > maxWebAppStartParam || !startParamRe.MatchString(s) {
			return webViewParams{}, mtproto.ErrStartParamInvalid
		}
		p.startParam = s
	}
	return p, nil
}

// themeParamsOf is theme_params as the page gets it: a JSON object of the
// names of the colors and their "#rrggbb". The clients send the colors in
// three forms: tdesktop as "#rrggbb" strings (window/themes/window_theme.cpp
// WebViewParams), Android as ARGB integers (ui/bots/BotWebViewSheet.java:1340-1366
// without hexify), iOS as RGB integers (WebUI/Sources/WebAppController.swift:127-145),
// while telegram-web-app.js takes only "#rgb", "#rrggbb" and rgb()/rgba()
// strings (parseColorToHex, telegram.org/js/telegram-web-app.js, read
// 2026-11-16). A value of another form is left out, as the page would
// leave it out; a theme_params that is not a JSON object of at most 4 KiB
// is THEME_PARAMS_INVALID.
func themeParamsOf(in *mtproto.DataJSON) (string, error) {
	if in == nil || strings.TrimSpace(in.GetData()) == "" {
		return "", nil
	}
	data := in.GetData()
	if len(data) > maxThemeParams {
		return "", mtproto.ErrThemeParamsInvalid
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(data), &raw); err != nil || raw == nil || len(raw) > maxThemeKeys {
		return "", mtproto.ErrThemeParamsInvalid
	}
	colors := make(map[string]string, len(raw))
	for k, v := range raw {
		if !themeKeyRe.MatchString(k) {
			continue
		}
		if c, ok := themeColor(v); ok {
			colors[k] = c
		}
	}
	if len(colors) == 0 {
		return "", nil
	}
	out, err := json.Marshal(colors) // keys sorted
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// themeColor reads one color of theme_params as "#rrggbb".
func themeColor(v json.RawMessage) (string, bool) {
	var n json.Number
	if err := json.Unmarshal(v, &n); err == nil {
		i, err := n.Int64()
		if err != nil || i < -1<<31 || i > 1<<32-1 {
			return "", false
		}
		return fmt.Sprintf("#%06x", uint32(i)&0xffffff), true
	}
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return "", false
	}
	s = strings.ToLower(strings.TrimSpace(s))
	if hexColorRe.MatchString(s) {
		if len(s) == 4 {
			return "#" + string([]byte{s[1], s[1], s[2], s[2], s[3], s[3]}), true
		}
		return s, true
	}
	if m := rgbColorRe.FindStringSubmatch(s); m != nil {
		var rgb [3]int
		for i := range rgb {
			x, err := strconv.Atoi(m[i+1])
			if err != nil || x > 255 {
				return "", false
			}
			rgb[i] = x
		}
		return fmt.Sprintf("#%02x%02x%02x", rgb[0], rgb[1], rgb[2]), true
	}
	return "", false
}

// checkWebAppURL checks the address of a mini app: https, at most 1024
// characters (task 106, "Пределы"), with a host.
func checkWebAppURL(link string) error {
	if !strings.HasPrefix(link, "https://") || utf8.RuneCountInString(link) > maxWebAppURL {
		return mtproto.ErrUrlInvalid
	}
	u, err := url.Parse(link)
	if err != nil || u.Host == "" || u.User != nil {
		return mtproto.ErrUrlInvalid
	}
	return nil
}

// webAppFragment are the launch parameters of a page.
type webAppFragment struct {
	data         string // the signed initData
	version      string
	platform     string
	theme        string
	startParam   string
	fullscreen   bool
	botInline    bool // opened from the switch_webview of inline results
	showSettings bool // the bot has the settings item of its attachment menu entry
}

// webAppURL is the address of the app with the launch parameters in its
// fragment, in the order Telegram gives them (tgWebAppData, tgWebAppVersion,
// tgWebAppPlatform, tgWebAppThemeParams, then the optional ones; the order
// is what tweb's rewrite of tgWebAppVersion=8.0 relies on only through the
// literal "tgWebAppVersion=8.0"). An app's own fragment is kept the way
// telegram-web-app.js reads it (urlParseHashParams): a path fragment ("#/x")
// takes the parameters after '?', one with parameters after '&'.
func webAppURL(appURL string, f webAppFragment) string {
	var b strings.Builder
	add := func(k, v string) {
		if b.Len() > 0 {
			b.WriteByte('&')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(url.QueryEscape(v))
	}
	add("tgWebAppData", f.data)
	add("tgWebAppVersion", f.version)
	add("tgWebAppPlatform", f.platform)
	if f.theme != "" {
		add("tgWebAppThemeParams", f.theme)
	}
	if f.startParam != "" {
		add("tgWebAppStartParam", f.startParam)
	}
	if f.fullscreen {
		add("tgWebAppFullscreen", "1")
	}
	if f.botInline {
		add("tgWebAppBotInline", "1")
	}
	if f.showSettings {
		add("tgWebAppShowSettings", "1")
	}
	params := b.String()
	base, frag, hasFrag := strings.Cut(appURL, "#")
	switch {
	case !hasFrag || frag == "":
		return base + "#" + params
	case !strings.ContainsAny(frag, "=?"):
		return base + "#" + frag + "?" + params
	}
	return base + "#" + frag + "&" + params
}

// resolveBot is the bot an InputUser of a web view names: BOT_INVALID for
// a user that is not a bot.
func (c *BotsCore) resolveBot(in *mtproto.InputUser) (*mtproto.ImmutableUser, error) {
	target := mtproto.FromInputUser(c.MD.UserId, in)
	if !target.IsUser() {
		return nil, mtproto.ErrBotInvalid
	}
	bot, err := c.botUser(target.PeerId)
	if errors.Is(err, mtproto.ErrUserIdInvalid) {
		return nil, mtproto.ErrBotInvalid
	}
	return bot, err
}

// webAppKey is the key the bot's initData is signed with. A server without
// WEBAPP_SIGNING_KEY or BOT_SECRET_ENCRYPTION_KEY, and a bot whose keys are
// not stored (it has not logged in with its token since the keys were
// introduced), open no mini app: BOT_WEBVIEW_DISABLED (ADR 0068 p. 3).
func (c *BotsCore) webAppKey(botId int64) ([]byte, error) {
	if c.svcCtx.WebAppSigner == nil || c.svcCtx.BotKeys == nil {
		return nil, mtproto.ErrBotWebviewDisabled
	}
	keys, ok, err := c.svcCtx.BotKeys.Get(c.ctx, botId)
	if errors.Is(err, botkeys.ErrSealedInvalid) {
		// Sealed under another encryption key: the bot's next login stores
		// them again (botkeys.Store.Get).
		c.Logger.Errorf("web view - keys of bot %d do not open: %v", botId, err)
		return nil, mtproto.ErrBotWebviewDisabled
	}
	if err != nil {
		c.Logger.Errorf("web view - keys of bot %d: %v", botId, err)
		return nil, mtproto.ErrInternalServerError
	}
	if !ok || len(keys.WebApp) == 0 {
		return nil, mtproto.ErrBotWebviewDisabled
	}
	return keys.WebApp, nil
}

// botMayWrite tells whether the bot may write to c's user: the user let it
// (bot_write_allowed, ADR 0068 p. 4) or wrote to it. It is allows_write_to_pm
// of the initData and the answer of bots.canSendMessage.
func (c *BotsCore) botMayWrite(botId int64) (bool, error) {
	allowed, err := c.svcCtx.Bots.WriteAllowed(c.ctx, c.MD.UserId, botId)
	if err != nil || allowed {
		return allowed, err
	}
	return c.svcCtx.WroteToBot(c.ctx, c.MD.UserId, botId)
}

// photoURL is the public photo of a user or chat with a username
// (/_l/avatar/<username>, ADR 0036 p. 4); bots/webapps: "photo_url ...
// .jpeg or .svg", which the photos of links are.
func (c *BotsCore) photoURL(username string) string {
	if username == "" || c.svcCtx.Config.Avatars == "" {
		return ""
	}
	return c.svcCtx.Config.Avatars + url.PathEscape(username)
}

// webAppUser is a WebAppUser of u (core.telegram.org/bots/webapps#webappuser)
// with what any user sees of it; is_premium by user.premium, which every
// user has (ADR 0041).
func (c *BotsCore) webAppUser(u *mtproto.ImmutableUser) *webappdata.User {
	return &webappdata.User{
		Id:        u.Id(),
		IsBot:     u.IsBot(),
		FirstName: u.FirstName(),
		LastName:  u.LastName(),
		Username:  u.Username(),
		IsPremium: u.Premium(),
		PhotoUrl:  c.photoURL(u.Username()),
	}
}

// initUser is the user of an initData: c's user with the language of its
// session and what the bot may do with it.
func (c *BotsCore) initUser(botId int64) (*webappdata.User, error) {
	me, err := c.svcCtx.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{Id: c.MD.UserId})
	if err != nil {
		return nil, err
	}
	u := c.webAppUser(me)
	u.LanguageCode = c.MD.LangCode
	if u.AllowsWriteToPm, err = c.botMayWrite(botId); err != nil {
		c.Logger.Errorf("web view - may bot %d write to %d: %v", botId, c.MD.UserId, err)
		return nil, mtproto.ErrInternalServerError
	}
	if u.AddedToAttachmentMenu, err = c.svcCtx.Bots.AttachMenuInstalled(c.ctx, c.MD.UserId, botId); err != nil {
		c.Logger.Errorf("web view - attachment menu of bot %d for %d: %v", botId, c.MD.UserId, err)
		return nil, mtproto.ErrInternalServerError
	}
	return u, nil
}

// webViewChat is the chat a web view is opened in.
type webViewChat struct {
	peer *mtproto.PeerUtil
	// user is the other user of a private chat (c's user itself in Saved
	// Messages).
	user *mtproto.ImmutableUser
	chat *mtproto.MutableChat
	// channel is the supergroup or channel, with c's user's row when it is
	// in it.
	channel *channelpb.MutableChannel
}

// webViewChatOf loads the chat a request of a web view names, nil for
// inputPeerEmpty. A basic group the user is not in is PEER_ID_INVALID, a
// channel it may not read CHANNEL_PRIVATE.
func (c *BotsCore) webViewChatOf(in *mtproto.InputPeer) (*webViewChat, error) {
	if in == nil || in.GetPredicateName() == mtproto.Predicate_inputPeerEmpty {
		return nil, nil
	}
	peer := c.chatPeer(in)
	ch := &webViewChat{peer: peer}
	var err error
	switch {
	case peer.IsUser():
		ch.user, err = c.svcCtx.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{Id: peer.PeerId})
		if errors.Is(err, mtproto.ErrUserIdInvalid) {
			return nil, mtproto.ErrPeerIdInvalid
		}
	case peer.IsChat():
		ch.chat, err = c.svcCtx.Chat.ChatGetMutableChat(c.ctx, &chatpb.TLChatGetMutableChat{ChatId: peer.PeerId})
		if err == nil {
			if _, in := ch.chat.GetImmutableChatParticipant(c.MD.UserId); !in {
				return nil, mtproto.ErrPeerIdInvalid
			}
		}
	case peer.IsChannel():
		ch.channel, err = c.svcCtx.Channel.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: peer.PeerId, Id: []int64{c.MD.UserId}})
		if err == nil {
			p, member := ch.channel.GetParticipant(c.MD.UserId)
			info := ch.channel.GetChannel()
			if info.GetDeleted() || info.GetBlocked() || (!info.Public() && (!member || p.GetState() != channelperm.StateIn)) {
				return nil, mtproto.ErrChannelPrivate
			}
		}
	default:
		return nil, mtproto.ErrPeerIdInvalid
	}
	if err != nil {
		return nil, err
	}
	return ch, nil
}

// chatType is chat_type of the initData (core.telegram.org/bots/webapps#webappinitdata):
// "sender" for the private chat with the bot ("a private chat with the user
// opening the link", as the bot sees it), "private" for another private
// chat, "group", "supergroup" or "channel".
func (ch *webViewChat) chatType(botId int64) string {
	switch {
	case ch.user != nil && ch.peer.PeerId == botId:
		return "sender"
	case ch.user != nil:
		return "private"
	case ch.chat != nil:
		return "group"
	case ch.channel.GetChannel().GetBroadcast():
		return "channel"
	}
	return "supergroup"
}

// botAPIChatId is the id of a group or channel as bots see it in the Bot
// API, which initData follows: -id for a basic group, -100<id> for a
// supergroup or channel.
func botAPIChatId(peer *mtproto.PeerUtil) int64 {
	if peer.IsChannel() {
		return -1000000000000 - peer.PeerId
	}
	return -peer.PeerId
}

// webAppChat is chat of the initData of a group or channel
// (core.telegram.org/bots/webapps#webappchat); nil for a private chat.
func (c *BotsCore) webAppChat(ch *webViewChat, botId int64) *webappdata.Chat {
	switch {
	case ch.chat != nil:
		return &webappdata.Chat{Id: botAPIChatId(ch.peer), Type: ch.chatType(botId), Title: ch.chat.Title()}
	case ch.channel != nil:
		info := ch.channel.GetChannel()
		return &webappdata.Chat{
			Id:       botAPIChatId(ch.peer),
			Type:     ch.chatType(botId),
			Title:    info.GetTitle(),
			Username: info.GetUsername(),
			PhotoUrl: c.photoURL(info.GetUsername()),
		}
	}
	return nil
}

// answerFrom checks that c's user may send the bot's answer of a web view
// (messages.sendWebViewResultMessage: an inline result from the user, via
// the bot) to the chat, as messages.sendInlineBotResult would, and returns
// the peer it goes from (send_as, pkg/sendas). msg checks the rights again
// when the answer is sent.
func (c *BotsCore) answerFrom(ch *webViewChat, sendAs *mtproto.InputPeer) (*mtproto.Peer, error) {
	switch {
	case ch.user != nil:
		if ch.peer.PeerId != c.MD.UserId {
			blocked, err := c.svcCtx.UserBlockedByUser(c.ctx, &userpb.TLUserBlockedByUser{UserId: c.MD.UserId, PeerUserId: ch.peer.PeerId})
			if err != nil {
				return nil, err
			}
			if mtproto.FromBool(blocked) {
				return nil, mtproto.ErrYouBlockedUser
			}
		}
	case ch.chat != nil:
		p, in := ch.chat.GetImmutableChatParticipant(c.MD.UserId)
		if !in || !p.IsChatMemberStateNormal() || ch.chat.MigratedTo() != nil || ch.chat.Deactivated() {
			return nil, mtproto.ErrChatWriteForbidden
		}
	case ch.channel != nil:
		p, member := ch.channel.GetParticipant(c.MD.UserId)
		if !member {
			return nil, mtproto.ErrChatWriteForbidden
		}
		info := ch.channel.GetChannel()
		if err := channelperm.Can(
			channelperm.Channel{Broadcast: info.GetBroadcast(), Gigagroup: info.GetGigagroup(), Default: info.GetDefaultBannedRights(), Blocked: info.GetBlocked(), Monoforum: info.GetMonoforum()},
			channelperm.Participant{Role: p.GetRole(), State: p.GetState(), AdminRights: p.GetAdminRights(), BannedRights: p.GetBannedRights(), UntilDate: p.GetUntilDate()},
			channelperm.SendInline, nowUnix()); err != nil {
			return nil, err
		}
	}
	return c.svcCtx.SendAs.From(c.ctx, c.MD.UserId, ch.peer, sendAs)
}

// refusedAnswer tells an error of answerFrom that only says the user may
// not write to the chat.
func refusedAnswer(err error) bool {
	for _, e := range []error{
		mtproto.ErrYouBlockedUser, mtproto.ErrChatWriteForbidden, mtproto.ErrChannelPrivate,
		mtproto.ErrSendAsPeerInvalid, mtproto.ErrChatSendInlineForbidden, mtproto.ErrChatAdminRequired,
	} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

// webViewAnswer is where the bot's answer of a web view goes.
type webViewAnswer struct {
	chat    *webViewChat
	replyTo *mtproto.InputReplyTo
	silent  bool
	sendAs  *mtproto.InputPeer
}

// webView is a web view to open.
type webView struct {
	bot    *mtproto.ImmutableUser
	appURL string
	params webViewParams
	// answer: the bot may answer into a chat; the web view gets a query_id.
	answer *webViewAnswer
	// receiver and chat of the initData (attachment menu).
	receiver *mtproto.ImmutableUser
	chat     *webappdata.Chat
	// chatType and chatInstance of the initData (direct and main apps).
	chatType     string
	chatInstance int64
	botInline    bool
	showSettings bool
	fullsize     bool
}

// open signs the initData of w and returns webViewResultUrl. A web view the
// bot may answer gets a query_id and is stored (dao.WebView) after the
// address is built and before it is returned.
func (c *BotsCore) open(w *webView) (*mtproto.WebViewResult, error) {
	key, err := c.webAppKey(w.bot.Id())
	if err != nil {
		return nil, err
	}
	user, err := c.initUser(w.bot.Id())
	if err != nil {
		return nil, err
	}
	d := &webappdata.InitData{
		User:         user,
		Chat:         w.chat,
		ChatType:     w.chatType,
		ChatInstance: w.chatInstance,
		StartParam:   w.params.startParam,
		AuthDate:     time.Now().Unix(),
	}
	if w.receiver != nil {
		d.Receiver = c.webAppUser(w.receiver)
	}
	var queryId int64
	if w.answer != nil {
		if queryId, err = dao.NewQueryId(); err != nil {
			return nil, mtproto.ErrInternalServerError
		}
		d.QueryId = strconv.FormatInt(queryId, 10)
	}
	data, err := webappdata.Sign(d, w.bot.Id(), key, c.svcCtx.WebAppSigner)
	if err != nil {
		c.Logger.Errorf("web view - sign for bot %d: %v", w.bot.Id(), err)
		return nil, mtproto.ErrInternalServerError
	}
	link := webAppURL(w.appURL, webAppFragment{
		data:         data,
		version:      c.svcCtx.Config.WebApp.Versions.For(w.params.platform),
		platform:     w.params.platform,
		theme:        w.params.theme,
		startParam:   w.params.startParam,
		fullscreen:   w.params.fullscreen,
		botInline:    w.botInline,
		showSettings: w.showSettings,
	})
	r := &mtproto.WebViewResult{
		Fullsize:   w.fullsize,
		Fullscreen: w.params.fullscreen,
		Url:        link,
	}
	if w.answer != nil {
		a := w.answer
		if err = c.svcCtx.PutWebView(c.ctx, queryId, &dao.WebView{
			User:     c.MD.UserId,
			Bot:      w.bot.Id(),
			PeerType: a.chat.peer.PeerType,
			PeerId:   a.chat.peer.PeerId,
			ReplyTo:  a.replyTo,
			Silent:   a.silent,
			SendAs:   a.sendAs,
		}); err != nil {
			c.Logger.Errorf("web view - store the query of bot %d: %v", w.bot.Id(), err)
			return nil, mtproto.ErrInternalServerError
		}
		r.QueryId_FLAGINT64 = wrapperspb.Int64(queryId)
		r.QueryId_INT64 = queryId
	}
	return mtproto.MakeTLWebViewResultUrl(r).To_WebViewResult(), nil
}

// attachMenuOf is the bot's attachment menu entry for c's user: BOT_INVALID
// when the bot has none, or the user has not added it (tdesktop reads the
// menu again on BOT_INVALID, inline_bots/bot_attach_web_view.cpp:1282-1286).
func (c *BotsCore) attachMenuOf(botId int64) (*botstore.AttachMenu, error) {
	m, err := c.svcCtx.Bots.AttachMenu(c.ctx, botId)
	if err != nil {
		c.Logger.Errorf("web view - attachment menu of bot %d: %v", botId, err)
		return nil, mtproto.ErrInternalServerError
	}
	if m == nil {
		return nil, mtproto.ErrBotInvalid
	}
	installed, err := c.svcCtx.Bots.AttachMenuInstalled(c.ctx, c.MD.UserId, botId)
	if err != nil {
		c.Logger.Errorf("web view - attachment menu of bot %d: %v", botId, err)
		return nil, mtproto.ErrInternalServerError
	}
	if !installed {
		return nil, mtproto.ErrBotInvalid
	}
	return m, nil
}

// attachPeerBit is the bit of botstore's peer types the chat is of
// (attachMenuPeerType*, core.telegram.org/api/bots/attach).
func (ch *webViewChat) attachPeerBit(botId int64) int32 {
	switch {
	case ch.user != nil && ch.peer.PeerId == botId:
		return botstore.AttachPeerSameBotPM
	case ch.user != nil && ch.user.IsBot():
		return botstore.AttachPeerBotPM
	case ch.user != nil:
		return botstore.AttachPeerPM
	case ch.channel.GetChannel().GetBroadcast():
		return botstore.AttachPeerBroadcast
	}
	return botstore.AttachPeerChat
}
