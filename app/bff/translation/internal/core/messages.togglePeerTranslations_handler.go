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

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/translate"
)

// MessagesTogglePeerTranslations
// messages.togglePeerTranslations#e47cb579 flags:# disabled:flags.0?true peer:InputPeer = Bool;
//
// Hides (disabled) or shows again the translation bar of a chat for the
// caller (ADR 0064, p. 8). The clients send it both ways and read the flag
// back from translations_disabled of userFull, chatFull and channelFull
// (Android TranslateController.java:194-214, 349-361; tdesktop
// data/data_peer.cpp:858-866; Web K appTranslationsManager.ts:522-535).
// There is no update for it: the other sessions of the caller see the flag
// at their next getFull*. It is kept whether or not an engine is
// configured: it is the reader's choice about a chat, not a translation.
//
// The chat must be the caller's: a dialog with the user or the group, a
// channel it can read; PEER_ID_INVALID otherwise. A service or the database
// failing on the way is FLOOD_WAIT_X, as in translateText: Android and
// tdesktop repeat a 500 without end (jni/tgnet/ConnectionsManager.cpp:
// 1372-1420, mtproto/mtp_instance.cpp:1583-1625).
func (c *TranslationCore) MessagesTogglePeerTranslations(in *mtproto.TLMessagesTogglePeerTranslations) (*mtproto.Bool, error) {
	if c.MD.IsBot {
		// A bot has no translation bar.
		return nil, mtproto.ErrBotMethodInvalid
	}
	peer, err := c.chatPeer(in.GetPeer())
	if err == nil {
		switch peer.Type {
		case mtproto.PEER_CHANNEL:
			_, err = c.readableChannel(in.GetPeer())
		default:
			err = c.checkDialog(peer)
		}
	}
	switch {
	case errors.Is(err, errUnreachable):
		return nil, mtproto.ErrPeerIdInvalid
	case err != nil:
		c.Logger.Errorf("messages.togglePeerTranslations - peer %d/%d of user %d: %v", peer.Type, peer.Id, c.MD.UserId, err)
		return nil, translate.FloodWait(internalWait)
	}
	if err = c.svcCtx.Settings.SetTranslationsDisabled(c.ctx, c.MD.UserId, peer, in.GetDisabled()); err != nil {
		c.Logger.Errorf("messages.togglePeerTranslations - user %d: %v", c.MD.UserId, err)
		return nil, translate.FloodWait(internalWait)
	}
	return mtproto.BoolTrue, nil
}
