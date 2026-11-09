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
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
)

// MessagesSetChatTheme
// messages.setChatTheme#81202c9 peer:InputPeer theme:InputChatTheme = Updates;
//
// The chat theme of a private chat, for both sides, or of a channel or a
// supergroup (core.telegram.org/api/themes). The clients take the change from
// the service message (tdesktop history.cpp:1463, Android ChatActivity:25878).
// A basic group has no way to get a theme in any client read (tdesktop
// window_peer_menu.cpp:1431) and is PEER_ID_INVALID.
func (c *CatalogCore) MessagesSetChatTheme(in *mtproto.TLMessagesSetChatTheme) (*mtproto.Updates, error) {
	peer := mtproto.FromInputPeer2(c.MD.UserId, in.GetPeer())
	emoticon, err := c.chatThemeEmoticon(in.GetTheme())
	if err != nil {
		return nil, err
	}
	switch {
	case peer.PeerType == mtproto.PEER_CHANNEL:
		r, err := c.svcCtx.Channel.ChannelSetChatTheme(c.ctx, &channelpb.TLChannelSetChatTheme{
			SelfId:        c.MD.UserId,
			PermAuthKeyId: c.MD.PermAuthKeyId,
			ChannelId:     peer.PeerId,
			ThemeEmoticon: emoticon,
		})
		if err != nil {
			c.Logger.Errorf("messages.setChatTheme - error: %v", err)
			return nil, err
		}
		return r, nil
	case peer.PeerType == mtproto.PEER_USER && peer.PeerId != c.MD.UserId:
		return c.setPrivateChatTheme(peer, emoticon)
	default:
		return nil, mtproto.ErrPeerIdInvalid
	}
}

// setPrivateChatTheme sets the theme of both sides. The service message goes
// first: it is what creates the dialogs of a first exchange, and the rows of
// the theme need them. A retry of the rows alone is safe (setBothDialogs).
func (c *CatalogCore) setPrivateChatTheme(peer *mtproto.PeerUtil, emoticon string) (*mtproto.Updates, error) {
	var have string
	current, err := c.svcCtx.Dialog.DialogGetDialogById(c.ctx, &dialog.TLDialogGetDialogById{
		UserId:   c.MD.UserId,
		PeerType: mtproto.PEER_USER,
		PeerId:   peer.PeerId,
	})
	if err == nil && current != nil {
		have = current.GetThemeEmoticon()
	}
	if have == emoticon {
		return nil, mtproto.ErrEmojiNotModified
	}
	action := mtproto.MakeTLMessageActionSetChatTheme(&mtproto.MessageAction{
		Theme: mtproto.MakeTLChatTheme(&mtproto.ChatTheme{Emoticon: emoticon}).To_ChatTheme(),
	}).To_MessageAction()
	r, err := c.sendPrivateService(peer, action)
	if err != nil {
		c.Logger.Errorf("messages.setChatTheme - error: %v", err)
		return nil, err
	}
	if _, err = c.svcCtx.Dialog.DialogSetChatTheme(c.ctx, &dialog.TLDialogSetChatTheme{
		UserId:        c.MD.UserId,
		PeerType:      mtproto.PEER_USER,
		PeerId:        peer.PeerId,
		ThemeEmoticon: emoticon,
	}); err != nil {
		c.Logger.Errorf("messages.setChatTheme - error: %v", err)
		return nil, err
	}
	return r, nil
}

// chatThemeEmoticon is the emoticon the request names, "" to take the theme
// away. Only the catalog's chat themes exist; a theme of a collectible gift
// (inputChatThemeUniqueGift) needs gifts, which are not served, and is
// EMOJI_INVALID (core.telegram.org/method/messages.setChatTheme).
func (c *CatalogCore) chatThemeEmoticon(theme *mtproto.InputChatTheme) (string, error) {
	switch theme.GetPredicateName() {
	case mtproto.Predicate_inputChatThemeEmpty:
		return "", nil
	case mtproto.Predicate_inputChatTheme:
		emoticon := theme.GetEmoticon()
		if emoticon == "" {
			return "", mtproto.ErrEmojiInvalid
		}
		list := &mtproto.Account_Themes{}
		_, found, err := c.stored("account.getChatThemes", "", list)
		if err != nil {
			return "", err
		}
		if found {
			for _, t := range list.GetThemes() {
				if t.GetEmoticon().GetValue() == emoticon {
					return emoticon, nil
				}
			}
		}
		return "", mtproto.ErrEmojiInvalid
	default:
		return "", mtproto.ErrEmojiInvalid
	}
}
