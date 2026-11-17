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
	"github.com/teamgram/proto/mtproto"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	mediapb "github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/botstore"
)

// MessagesGetAttachMenuBots
// messages.getAttachMenuBots#16fcc2cb hash:long = AttachMenuBots;
//
// The attachment menu bot mini apps the account installed
// (core.telegram.org/method/messages.getAttachMenuBots): the bots it added
// with messages.toggleBotInAttachMenu, in the order it added them (ADR
// 0068, p. 5). iOS asks on start with hash 0
// (TelegramCore/Sources/TelegramEngine/Messages/AttachMenuBots.swift:286-301),
// Web on start and on updateAttachMenuBots
// (lib/appManagers/appAttachMenuBotsManager.ts:51-71), Android keeps the
// hash of the answer for the next request
// (messenger/MediaDataController.java:568-582). attachMenuBotsNotModified
// only for the client's copy of a list: a hash that is ours and not 0.
func (c *BotsCore) MessagesGetAttachMenuBots(in *mtproto.TLMessagesGetAttachMenuBots) (*mtproto.AttachMenuBots, error) {
	if c.MD.IsBot {
		err := mtproto.ErrBotMethodInvalid
		c.Logger.Errorf("messages.getAttachMenuBots - error: %v", err)
		return nil, err
	}
	menus, err := c.svcCtx.Bots.InstalledAttachMenus(c.ctx, c.MD.UserId)
	if err != nil {
		c.Logger.Errorf("messages.getAttachMenuBots - error: %v", err)
		return nil, err
	}
	hash := botstore.AttachMenusHash(menus)
	if hash != 0 && in.GetHash() == hash {
		return mtproto.MakeTLAttachMenuBotsNotModified(nil).To_AttachMenuBots(), nil
	}
	docs, err := c.attachMenuDocuments(menus...)
	if err != nil {
		c.Logger.Errorf("messages.getAttachMenuBots - icons: %v", err)
		return nil, err
	}
	bots := make([]*mtproto.AttachMenuBot, 0, len(menus))
	ids := make([]int64, 0, len(menus))
	for i := range menus {
		bots = append(bots, c.attachMenuBot(&menus[i], docs, false))
		ids = append(ids, menus[i].BotId)
	}
	users, err := c.usersAsSeen(ids...)
	if err != nil {
		c.Logger.Errorf("messages.getAttachMenuBots - users: %v", err)
		return nil, err
	}
	return mtproto.MakeTLAttachMenuBots(&mtproto.AttachMenuBots{
		Hash:  hash,
		Bots:  bots,
		Users: users,
	}).To_AttachMenuBots(), nil
}

// attachMenuDocuments fetches the documents of the icons of the entries,
// by id, in one request to the media service.
func (c *BotsCore) attachMenuDocuments(menus ...botstore.AttachMenu) (map[int64]*mtproto.Document, error) {
	var ids []int64
	for _, m := range menus {
		for _, icon := range m.Icons {
			ids = append(ids, icon.DocumentId)
		}
	}
	docs := make(map[int64]*mtproto.Document, len(ids))
	if len(ids) == 0 {
		return docs, nil
	}
	list, err := c.svcCtx.MediaGetDocumentList(c.ctx, &mediapb.TLMediaGetDocumentList{IdList: ids})
	if err != nil {
		return nil, err
	}
	for _, d := range list.GetDatas() {
		docs[d.GetId()] = d
	}
	return docs, nil
}

// attachMenuBot is the TL attachMenuBot of an entry, inactive when the
// account has not added it. An icon whose document the media service does
// not have is left out and logged: the clients take each icon on its own
// (tdesktop inline_bots/bot_attach_web_view.cpp:169-185, iOS
// AttachMenuBots.swift:309-318), so the others still serve.
func (c *BotsCore) attachMenuBot(m *botstore.AttachMenu, docs map[int64]*mtproto.Document, inactive bool) *mtproto.AttachMenuBot {
	bot, missing := attachMenuBotOf(m, docs, inactive)
	for _, name := range missing {
		c.Logger.Errorf("attachment menu of bot %d: no document of icon %s", m.BotId, name)
	}
	return bot
}

// attachPeerTypes are the constructors of the bits of
// botstore.AttachMenu.PeerTypes, in the order of the TL schema.
var attachPeerTypes = []struct {
	bit  int32
	ctor func() *mtproto.AttachMenuPeerType
}{
	{botstore.AttachPeerSameBotPM, func() *mtproto.AttachMenuPeerType {
		return mtproto.MakeTLAttachMenuPeerTypeSameBotPM(nil).To_AttachMenuPeerType()
	}},
	{botstore.AttachPeerBotPM, func() *mtproto.AttachMenuPeerType {
		return mtproto.MakeTLAttachMenuPeerTypeBotPM(nil).To_AttachMenuPeerType()
	}},
	{botstore.AttachPeerPM, func() *mtproto.AttachMenuPeerType {
		return mtproto.MakeTLAttachMenuPeerTypePM(nil).To_AttachMenuPeerType()
	}},
	{botstore.AttachPeerChat, func() *mtproto.AttachMenuPeerType {
		return mtproto.MakeTLAttachMenuPeerTypeChat(nil).To_AttachMenuPeerType()
	}},
	{botstore.AttachPeerBroadcast, func() *mtproto.AttachMenuPeerType {
		return mtproto.MakeTLAttachMenuPeerTypeBroadcast(nil).To_AttachMenuPeerType()
	}},
}

// attachMenuBotOf builds the TL attachMenuBot of an entry and names the
// icons left out for want of their documents.
//
// peer_types shares flags.3 with show_in_attach_menu
// (attachMenuBot#d90d8dfe), so they go together: the types only for an
// entry shown in the attachment menu, which always has some
// (botstore.AttachMenu.validate). side_menu_disclaimer_needed stays false:
// it asks the user to accept Telegram's terms for mini apps, which Pluma
// has none of (ADR 0068, p. 5).
func attachMenuBotOf(m *botstore.AttachMenu, docs map[int64]*mtproto.Document, inactive bool) (*mtproto.AttachMenuBot, []string) {
	var peerTypes []*mtproto.AttachMenuPeerType
	if m.ShowInAttachMenu {
		peerTypes = make([]*mtproto.AttachMenuPeerType, 0, len(attachPeerTypes))
		for _, t := range attachPeerTypes {
			if m.PeerTypes&t.bit != 0 {
				peerTypes = append(peerTypes, t.ctor())
			}
		}
	}
	icons := make([]*mtproto.AttachMenuBotIcon, 0, len(m.Icons))
	var missing []string
	for _, icon := range m.Icons {
		doc := docs[icon.DocumentId]
		if doc == nil {
			missing = append(missing, icon.Name)
			continue
		}
		var colors []*mtproto.AttachMenuBotIconColor
		for _, color := range icon.Colors {
			colors = append(colors, mtproto.MakeTLAttachMenuBotIconColor(&mtproto.AttachMenuBotIconColor{
				Name:  color.Name,
				Color: color.Color,
			}).To_AttachMenuBotIconColor())
		}
		icons = append(icons, mtproto.MakeTLAttachMenuBotIcon(&mtproto.AttachMenuBotIcon{
			Name:   icon.Name,
			Icon:   doc,
			Colors: colors,
		}).To_AttachMenuBotIcon())
	}
	return mtproto.MakeTLAttachMenuBot(&mtproto.AttachMenuBot{
		Inactive:           inactive,
		HasSettings:        m.HasSettings,
		RequestWriteAccess: m.RequestWriteAccess,
		ShowInAttachMenu:   m.ShowInAttachMenu,
		ShowInSideMenu:     m.ShowInSideMenu,
		BotId:              m.BotId,
		ShortName:          m.ShortName,
		PeerTypes:          peerTypes,
		Icons:              icons,
	}).To_AttachMenuBot(), missing
}

// usersAsSeen is the users with the ids as the account sees them.
func (c *BotsCore) usersAsSeen(ids ...int64) ([]*mtproto.User, error) {
	if len(ids) == 0 {
		return []*mtproto.User{}, nil
	}
	users, err := c.svcCtx.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: append(append([]int64{}, ids...), c.MD.UserId)})
	if err != nil {
		return nil, err
	}
	return users.GetUserListByIdList(c.MD.UserId, ids...), nil
}
