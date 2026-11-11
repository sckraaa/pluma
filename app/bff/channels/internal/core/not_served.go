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
)

// The methods of channels and supergroups not served yet (ADR 0034, tasks
// 038-049): each answers ERR_ENTERPRISE_IS_BLOCKED (400), which the clients
// do not retry, never an internal error (500), which Android's and Web K's
// transports retry without end. A method moves to its own handler file when
// its task serves it.

// MessagesGetPersonalChannelHistory is not served yet: messages.getPersonalChannelHistory#55fb0996.
func (c *ChannelsCore) MessagesGetPersonalChannelHistory(in *mtproto.TLMessagesGetPersonalChannelHistory) (*mtproto.Messages_Messages, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// MessagesTranslateText is not served yet: messages.translateText#a5eec345.
// The server translates nothing until task 027 chooses how (ADR 0050); the
// clients offer no translation while translations_manual_enabled is
// disabled.
func (c *ChannelsCore) MessagesTranslateText(in *mtproto.TLMessagesTranslateText) (*mtproto.Messages_TranslatedText, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// MessagesTogglePeerTranslations is not served yet:
// messages.togglePeerTranslations#e47cb579, the translation bar of a chat,
// which the clients show only with translation on (task 027).
func (c *ChannelsCore) MessagesTogglePeerTranslations(in *mtproto.TLMessagesTogglePeerTranslations) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}
