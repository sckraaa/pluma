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
	"slices"
	"strings"

	"github.com/teamgram/proto/mtproto"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/botstore"
	"github.com/teamgram/teamgram-server/pkg/urlauth"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// The login of a user on a site through oauth.pluma.chat (task 106, ADR
// 0068 p. 6; core.telegram.org/api/url-authorization). The site's page made
// a request (pkg/urlauth, oauth_req) and shows its link as a QR code and a
// button; the app passes the link to messages.requestUrlAuth and gets
// urlAuthResultRequest with the bot, the site, the visitor's browser and
// the match codes. The user picks the code the page shows
// (checkUrlAuthMatchCode, as match_codes_first asks) and confirms
// (acceptUrlAuth with the code), or declines (declineUrlAuth); the page
// learns the outcome by polling the request.

// oauthTokenOf is the token of a request that names a login request by its
// link, not a button: tdesktop (boxes/url_auth_box.cpp:76-85), Android
// (ui/LinkManager.java:1340-1351, ui/web/BotWebViewContainer.java:1396-1412)
// and iOS (TelegramCore RequestMessageActionCallback.swift:291) pass only
// url, with in_app_origin from a web app.
func oauthTokenOf(peer *mtproto.InputPeer, msgId, buttonId *int32, link *string) (string, bool) {
	if (peer != nil && msgId != nil && buttonId != nil) || link == nil {
		return "", false
	}
	return urlauth.OAuthToken(*link)
}

// oauthError is the error of the API for an error of the request store.
func oauthError(err error) error {
	switch {
	case errors.Is(err, urlauth.ErrExpired):
		return mtproto.ErrUrlExpired
	case errors.Is(err, urlauth.ErrInvalid):
		return mtproto.ErrOauthRequestInvalid
	}
	return err
}

// logOAuthError logs an error of a login request that is not an answer of
// the API (URL_EXPIRED and the like are the user's, not the server's).
func (c *BotsCore) logOAuthError(method string, err error) {
	if _, ok := status.FromError(err); !ok {
		c.Logger.Errorf("%s - login request: %v", method, err)
	}
}

// oauthEntry is the request of token with its bot. A request whose bot is
// gone is OAUTH_REQUEST_INVALID (method/messages.requestUrlAuth).
func (c *BotsCore) oauthEntry(token string) (*urlauth.Entry, *mtproto.ImmutableUser, error) {
	e, err := c.svcCtx.OAuth.Get(c.ctx, token)
	if err != nil {
		return nil, nil, oauthError(err)
	}
	bot, err := c.svcCtx.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{Id: e.BotId})
	if errors.Is(err, mtproto.ErrUserIdInvalid) {
		return nil, nil, mtproto.ErrOauthRequestInvalid
	}
	if err != nil {
		return nil, nil, err
	}
	if !bot.IsBot() || bot.Deleted() {
		return nil, nil, mtproto.ErrOauthRequestInvalid
	}
	return e, bot, nil
}

// checkInAppOrigin admits a request from a web app only from an origin the
// bot's login client lists (bot_login_clients.origins), or, for the login
// widget format, from the origin the site made the request with: Android
// passes the origin of the page of the web view (BotWebViewContainer.java:
// 1396-1412, getOriginHost :1541-1560) and hands the outcome back to that
// page only (ui/OAuthSheet.java:82-85).
func (c *BotsCore) checkInAppOrigin(e *urlauth.Entry, origin string) error {
	origin = strings.ToLower(origin)
	if origin == "" {
		return mtproto.ErrUrlInvalid
	}
	if e.Kind == urlauth.KindLegacy && e.Origin != "" && strings.ToLower(e.Origin) == origin {
		return nil
	}
	client, err := c.svcCtx.Bots.LoginClient(c.ctx, e.BotId)
	if err != nil {
		return err
	}
	if client == nil || !slices.Contains(client.Origins, origin) {
		return mtproto.ErrUrlInvalid
	}
	return nil
}

// oauthWrite tells whether the bot of a request may write to c's user once
// the user accepts it: the site asked for it (telegram:bot_access) and the
// user lets it now or let it before.
func (c *BotsCore) oauthWrite(e *urlauth.Entry, botId int64, allowNow bool) (bool, error) {
	if !e.WantsWrite() {
		return false, nil
	}
	if allowNow {
		return true, nil
	}
	return c.svcCtx.Bots.WriteAllowed(c.ctx, c.MD.UserId, botId)
}

// requestOAuth answers messages.requestUrlAuth for a login request.
//
// The answer always carries the visitor (browser, platform, ip and region
// share flags.2 of urlAuthResultRequest, so all four go out; iOS shows them
// only when all four are there, RequestMessageActionCallback.swift:312) and
// the match codes with match_codes_first: the user matches the code of the
// page before seeing what the site asks, and the apps check it with
// checkUrlAuthMatchCode first (Android ui/OAuthSheet.java:550-570, tdesktop
// boxes/url_auth_box.cpp:897-930, iOS AuthConfirmationScreen.swift:99-104).
// request_write_access is asked only of a user whose permission the bot
// does not have yet, as for a login button.
func (c *BotsCore) requestOAuth(token string, inAppOrigin *string) (*mtproto.UrlAuthResult, error) {
	e, bot, err := c.oauthEntry(token)
	if err != nil {
		return nil, err
	}
	if e.Status != urlauth.StatusPending {
		return nil, mtproto.ErrUrlExpired
	}
	if inAppOrigin != nil {
		if err = c.checkInAppOrigin(e, *inAppOrigin); err != nil {
			return nil, err
		}
	}
	askWrite := false
	if e.WantsWrite() {
		allowed, err := c.svcCtx.Bots.WriteAllowed(c.ctx, c.MD.UserId, bot.Id())
		if err != nil {
			return nil, err
		}
		askWrite = !allowed
	}
	users, err := c.svcCtx.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: []int64{c.MD.UserId, bot.Id()}})
	if err != nil {
		return nil, err
	}
	botUser := users.GetUserListByIdList(c.MD.UserId, bot.Id())
	if len(botUser) != 1 {
		return nil, mtproto.ErrOauthRequestInvalid
	}
	return mtproto.MakeTLUrlAuthResultRequest(&mtproto.UrlAuthResult{
		RequestWriteAccess: askWrite,
		RequestPhoneNumber: e.WantsPhone(),
		MatchCodesFirst:    true,
		Bot:                botUser[0],
		Domain:             e.Domain,
		Browser:            wrapperspb.String(e.Browser),
		Platform:           wrapperspb.String(e.Platform),
		Ip:                 wrapperspb.String(e.IP),
		Region:             wrapperspb.String(e.Region),
		MatchCodes:         e.MatchCodes,
	}).To_UrlAuthResult(), nil
}

// acceptOAuth answers messages.acceptUrlAuth for a login request: the match
// code must be the right one (a wrong one is URL_INVALID and counts towards
// the limit of the request), the request turns accepted for c's user with
// the consent to telegram:bot_access (write_allowed) and to phone
// (share_phone_number) where the site asked for them, and the answer is
// urlAuthResultAccepted without an address: the site gets the outcome
// through its page, and the apps say the login is done (tdesktop
// boxes/url_auth_box.cpp:493-505, Android ui/OAuthSheet.java:86-101).
//
// The acceptance in Redis is what logs the site in, so it goes first; the
// site among the sites logged in on and the bot's permission follow. If one
// of them fails the error goes back to the app, and the same acceptance
// sent again (CodeRepeated) records them again; both are idempotent.
func (c *BotsCore) acceptOAuth(token string, in *mtproto.TLMessagesAcceptUrlAuth) (*mtproto.UrlAuthResult, error) {
	code := in.GetMatchCode().GetValue()
	if code == "" {
		return nil, mtproto.ErrUrlInvalid
	}
	e, bot, err := c.oauthEntry(token)
	if err != nil {
		return nil, err
	}
	write, err := c.oauthWrite(e, bot.Id(), in.GetWriteAllowed())
	if err != nil {
		return nil, err
	}
	res, err := c.svcCtx.OAuth.Accept(c.ctx, token, urlauth.Acceptance{
		UserId:       c.MD.UserId,
		Code:         code,
		WriteAllowed: write,
		SharePhone:   in.GetSharePhoneNumber() && e.WantsPhone(),
	})
	if err != nil {
		return nil, oauthError(err)
	}
	if res != urlauth.CodeRight && res != urlauth.CodeRepeated {
		return nil, mtproto.ErrUrlInvalid
	}
	if _, err = c.svcCtx.Bots.PutWebAuthorization(c.ctx, botstore.WebAuthorization{
		UserId:       c.MD.UserId,
		BotId:        bot.Id(),
		Domain:       e.Domain,
		Browser:      e.Browser,
		Platform:     e.Platform,
		IP:           e.IP,
		Region:       e.Region,
		WriteAllowed: write,
	}, nowUnix()); err != nil {
		return nil, err
	}
	if write {
		if _, err = c.allowWrite(bot.Id(), botstore.WriteAllowedDomain, mtproto.MakeMessageActionBotAllowed(e.Domain)); err != nil {
			return nil, err
		}
	}
	return accepted(""), nil
}
