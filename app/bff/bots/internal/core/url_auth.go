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
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/botkeys"
	"github.com/teamgram/teamgram-server/pkg/botmarkup"
	"github.com/teamgram/teamgram-server/pkg/botstore"
	"github.com/teamgram/teamgram-server/pkg/urlauth"
	"github.com/teamgram/teamgram-server/pkg/webappdata"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// The login of a user on a bot's site from Pluma (core.telegram.org/api/url-authorization,
// task 106, ADR 0068 p. 6): a login button of a bot's message
// (keyboardButtonUrlAuth) or a link to a domain of url_auth_domains. The
// site gets the data of the login widget (core.telegram.org/widgets/login-legacy)
// in the query of its address, signed with the login key of the bot.

// markupEnv is what botmarkup.Normalize looks up for a markup of c's user, a
// bot.
func (c *BotsCore) markupEnv() botmarkup.Env {
	return botmarkup.Env{
		Self:        c.MD.UserId,
		ResolveUser: c.resolveUser,
		LoginDomain: c.loginDomainOf,
	}
}

// loginDomainOf is the login domain of a user that is a bot; isBot is false
// for a user that is not one or no longer exists.
func (c *BotsCore) loginDomainOf(userId int64) (string, bool, error) {
	u, err := c.svcCtx.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{Id: userId})
	if errors.Is(err, mtproto.ErrUserIdInvalid) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if !u.IsBot() || u.Deleted() {
		return "", false, nil
	}
	domain, err := c.svcCtx.Bots.LoginDomain(c.ctx, userId)
	if err != nil {
		c.Logger.Errorf("bot markup - login domain of %d: %v", userId, err)
		return "", true, mtproto.ErrInternalServerError
	}
	return domain, true, nil
}

// urlAuthTarget is the login a requestUrlAuth or acceptUrlAuth names.
type urlAuthTarget struct {
	bot    *mtproto.ImmutableUser
	domain string // the login domain of the bot
	link   string // the address the site gets the data at
	// askWrite: the button asks the user to let the bot write to them
	// (inputKeyboardButtonUrlAuth.request_write_access). A link asks for
	// nothing.
	askWrite bool
	loginKey []byte
}

// urlAuthTarget resolves the button or the link of a request. nil, without
// an error, is a login the server does not take part in, which clients open
// as a plain link (urlAuthResultDefault): a button whose bot is gone, has
// another domain by now, or has no keys (no BOT_SECRET_ENCRYPTION_KEY, or a
// bot that has not logged in since the keys were introduced).
func (c *BotsCore) urlAuthTarget(peer *mtproto.InputPeer, msgId, buttonId *int32, link *string, inAppOrigin *string) (*urlAuthTarget, error) {
	if inAppOrigin != nil {
		// in_app_origin names the web page of an app that asks for a login
		// of a site (core.telegram.org/method/messages.requestUrlAuth): it
		// belongs to the logins of sites through oauth.pluma.chat only.
		return nil, mtproto.ErrUrlInvalid
	}
	switch {
	case peer != nil && msgId != nil && buttonId != nil:
		return c.buttonTarget(peer, *msgId, *buttonId)
	case link != nil:
		return c.linkTarget(*link)
	}
	return nil, mtproto.ErrUrlInvalid
}

// buttonTarget is the login of a keyboardButtonUrlAuth. The client names the
// message as with a callback button (tdesktop boxes/url_auth_box.cpp:279-295:
// the peer of the chat, the message and the button's button_id), and the
// login bot and request_write_access are those the button was stored with
// (pkg/botmarkup).
func (c *BotsCore) buttonTarget(inPeer *mtproto.InputPeer, msgId, buttonId int32) (*urlAuthTarget, error) {
	p, err := c.pressedMessage(c.chatPeer(inPeer), msgId)
	if err != nil {
		return nil, err
	}
	button := loginButtonOf(p.message, buttonId)
	if button == nil {
		return nil, mtproto.ErrButtonIdInvalid
	}
	botId := button.GetUserId_INT64()
	if botId == 0 {
		return nil, nil
	}
	bot, err := c.svcCtx.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{Id: botId})
	if errors.Is(err, mtproto.ErrUserIdInvalid) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !bot.IsBot() || bot.Deleted() {
		return nil, nil
	}
	domain, err := c.svcCtx.Bots.LoginDomain(c.ctx, botId)
	if err != nil {
		return nil, err
	}
	if host, ok := botmarkup.LoginHost(button.GetUrl()); !ok || domain == "" || host != domain {
		return nil, nil
	}
	return c.withLoginKey(&urlAuthTarget{bot: bot, domain: domain, link: button.GetUrl(), askWrite: button.GetRequestWriteAccess()})
}

// loginButtonOf returns the login button of the message with the button_id,
// nil for none.
func loginButtonOf(m *mtproto.Message, buttonId int32) *mtproto.KeyboardButton {
	if m.GetReplyMarkup().GetPredicateName() != mtproto.Predicate_replyInlineMarkup {
		return nil
	}
	for _, row := range m.GetReplyMarkup().GetRows() {
		for _, b := range row.GetButtons() {
			if b.GetPredicateName() == mtproto.Predicate_keyboardButtonUrlAuth && b.GetButtonId() == buttonId {
				return b
			}
		}
	}
	return nil
}

// linkTarget is the login of a link whose domain is one of url_auth_domains
// of the app config: the domain of a bot. A link to any other address is
// URL_INVALID (method/messages.requestUrlAuth), which clients open as it is
// (tdesktop boxes/url_auth_box.cpp:346-352).
func (c *BotsCore) linkTarget(link string) (*urlAuthTarget, error) {
	host, ok := botmarkup.LoginHost(link)
	if !ok {
		return nil, mtproto.ErrUrlInvalid
	}
	botId, err := c.svcCtx.Bots.BotOfLoginDomain(c.ctx, host)
	if err != nil {
		return nil, err
	}
	if botId == 0 {
		return nil, mtproto.ErrUrlInvalid
	}
	bot, err := c.botUser(botId)
	if err != nil {
		return nil, err
	}
	return c.withLoginKey(&urlAuthTarget{bot: bot, domain: host, link: link})
}

// withLoginKey adds the login key of the bot to t; nil when the bot has none.
func (c *BotsCore) withLoginKey(t *urlAuthTarget) (*urlAuthTarget, error) {
	if c.svcCtx.BotKeys == nil {
		return nil, nil
	}
	keys, ok, err := c.svcCtx.BotKeys.Get(c.ctx, t.bot.Id())
	if errors.Is(err, botkeys.ErrSealedInvalid) {
		// Keys sealed under another encryption key: the bot's next login
		// stores them again (botkeys.Store.Get).
		c.Logger.Errorf("url auth - keys of bot %d do not open: %v", t.bot.Id(), err)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	t.loginKey = keys.Login
	return t, nil
}

// signedLink is the address of t with the data of c's user, signed with the
// login key of the bot (core.telegram.org/widgets/login-legacy). photo_url
// is the public photo of a user with a username (ADR 0036 p. 4).
func (c *BotsCore) signedLink(t *urlAuthTarget) (string, error) {
	me, err := c.svcCtx.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{Id: c.MD.UserId})
	if err != nil {
		return "", err
	}
	login := &webappdata.Login{
		Id:        me.Id(),
		FirstName: me.FirstName(),
		LastName:  me.LastName(),
		Username:  me.Username(),
		AuthDate:  time.Now().Unix(),
	}
	if login.Username != "" && c.svcCtx.Config.Avatars != "" {
		login.PhotoUrl = c.svcCtx.Config.Avatars + url.PathEscape(login.Username)
	}
	fields, err := webappdata.SignLogin(login, t.loginKey)
	if err != nil {
		return "", err
	}
	return urlauth.LoginLink(t.link, fields)
}

// accepted is urlAuthResultAccepted with the address to open.
func accepted(link string) *mtproto.UrlAuthResult {
	return mtproto.MakeTLUrlAuthResultAccepted(&mtproto.UrlAuthResult{
		Url_STRING:     link,
		Url_FLAGSTRING: mtproto.MakeFlagsString(link),
	}).To_UrlAuthResult()
}

// urlAuthDefault is urlAuthResultDefault: the client opens the link as it is.
func urlAuthDefault() *mtproto.UrlAuthResult {
	return mtproto.MakeTLUrlAuthResultDefault(nil).To_UrlAuthResult()
}

// clientIP is the address of the request without a port, "" when it is
// not an address.
func clientIP(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		addr = host
	}
	if ip := net.ParseIP(addr); ip != nil {
		return ip.String()
	}
	return ""
}

// webSession describes where c's user logs in from, as the list of the
// sites logged in on shows it (webAuthorization): the app and the device of
// the session, its address and the region of the address by the GeoIP
// database of authsession. What Telegram writes there for a login from its
// apps is not known; a missing part is left empty, and logged, rather than
// failing the login it only describes.
func (c *BotsCore) webSession() (browser, platform, ip, region string) {
	ip = clientIP(c.MD.ClientAddr)
	a, err := c.svcCtx.AuthsessionGetAuthorization(c.ctx, &authsession.TLAuthsessionGetAuthorization{AuthKeyId: c.MD.PermAuthKeyId})
	if err != nil {
		c.Logger.Errorf("url auth - session of user %d: %v", c.MD.UserId, err)
	} else {
		browser, platform = sessionNames(a)
	}
	if ip != "" {
		r, err := c.svcCtx.AuthsessionGetIpRegion(c.ctx, &authsession.TLAuthsessionGetIpRegion{Ip: ip})
		if err != nil {
			c.Logger.Errorf("url auth - region of the address of user %d: %v", c.MD.UserId, err)
		} else {
			region = r.GetV()
		}
	}
	return browser, platform, ip, region
}

// sessionNames are the browser and the platform of a web session made from
// an app session: the app with its version (app_name and app_version of
// account.getAuthorizations), and the device with its system.
func sessionNames(a *mtproto.Authorization) (browser, platform string) {
	browser = joinNonEmpty(" ", a.GetAppName(), a.GetAppVersion())
	system := a.GetPlatform()
	if system == "" {
		system = a.GetSystemVersion()
	}
	return browser, joinNonEmpty(", ", a.GetDeviceModel(), system)
}

func joinNonEmpty(sep string, parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}

// writeAllowed tells whether the bot of t may write to c's user already.
func (c *BotsCore) writeAllowed(t *urlAuthTarget) (bool, error) {
	if !t.askWrite {
		return false, nil
	}
	return c.svcCtx.Bots.WriteAllowed(c.ctx, c.MD.UserId, t.bot.Id())
}

// recordWebLogin records the login of c's user on the domain of t
// (web_authorizations, account.getWebAuthorizations).
func (c *BotsCore) recordWebLogin(t *urlAuthTarget, writeAllowed bool) error {
	browser, platform, ip, region := c.webSession()
	_, err := c.svcCtx.Bots.PutWebAuthorization(c.ctx, botstore.WebAuthorization{
		UserId:       c.MD.UserId,
		BotId:        t.bot.Id(),
		Domain:       t.domain,
		Browser:      browser,
		Platform:     platform,
		IP:           ip,
		Region:       region,
		WriteAllowed: writeAllowed,
	}, nowUnix())
	return err
}

// int32Of is the value of an optional int field, nil when it is absent.
func int32Of(v *wrapperspb.Int32Value) *int32 {
	if v == nil {
		return nil
	}
	x := v.GetValue()
	return &x
}

// stringOf is the value of an optional string field, nil when it is absent.
func stringOf(v *wrapperspb.StringValue) *string {
	if v == nil {
		return nil
	}
	x := v.GetValue()
	return &x
}

func nowUnix() int32 {
	return int32(time.Now().Unix())
}
