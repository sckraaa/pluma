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
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/env2"
)

// newInviteHashes are the hashes of a listener's and a speaker's link.
func newInviteHashes() (listener, speaker string, err error) {
	var b [16]byte
	if _, err = rand.Read(b[:8]); err != nil {
		return "", "", err
	}
	if _, err = rand.Read(b[8:]); err != nil {
		return "", "", err
	}
	return hex.EncodeToString(b[:8]), hex.EncodeToString(b[8:]), nil
}

// PhoneExportGroupCallInvite
// phone.exportGroupCallInvite#e6aa647f flags:# can_self_unmute:flags.0?true call:InputGroupCall = phone.ExportedGroupCallInvite;
//
// The link of a listener, or with can_self_unmute of a speaker (only one
// who manages the call gives it: tdesktop asks it only then,
// calls_group_settings.cpp:920-922). The link is the public username of
// the chat with voicechat=<hash>, as the clients read it (Android
// LaunchActivity.java:2277-2278, tdesktop local_url_handlers.cpp:853-858);
// Android offers it for public chats only (GroupCallActivity.java:
// 6632-6634), and a private chat has none: PUBLIC_CHANNEL_MISSING.
func (c *GroupCallsCore) PhoneExportGroupCallInvite(in *mtproto.TLPhoneExportGroupCallInvite) (*mtproto.Phone_ExportedGroupCallInvite, error) {
	call, err := c.loadLive(in.GetCall())
	if err != nil {
		return nil, err
	}
	a, err := c.loadPeer(call.PeerType, call.PeerId, c.MD.UserId)
	if err != nil {
		return nil, err
	}
	if !a.canView(c.MD.UserId) {
		return nil, mtproto.ErrGroupcallForbidden
	}
	if in.GetCanSelfUnmute() && !a.canManage(c.MD.UserId) {
		return nil, mtproto.ErrChatAdminRequired
	}
	username := a.username()
	if username == "" {
		return nil, mtproto.ErrPublicChannelMissing
	}
	if call.ListenerHash == "" {
		listener, speaker, err := newInviteHashes()
		if err != nil {
			return nil, err
		}
		if call, err = c.svcCtx.Dao.Store.InitInviteHashes(c.ctx, call.Id, listener, speaker); err != nil {
			return nil, storeError(err)
		}
	}
	hash := call.ListenerHash
	if in.GetCanSelfUnmute() {
		hash = call.SpeakerHash
	}
	return mtproto.MakeTLPhoneExportedGroupCallInvite(&mtproto.Phone_ExportedGroupCallInvite{
		Link: fmt.Sprintf("https://%s/%s?voicechat=%s", env2.TDotMe, username, hash),
	}).To_Phone_ExportedGroupCallInvite(), nil
}
