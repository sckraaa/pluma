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
	"github.com/teamgram/teamgram-server/pkg/groupcalls"
)

// PhoneGetGroupParticipants
// phone.getGroupParticipants#c558d8ab call:InputGroupCall ids:Vector<InputPeer> sources:Vector<int> offset:string limit:int = phone.GroupParticipants;
//
// The participants in the call: those of ids or of sources (tdesktop
// resolves a source it does not know by it, data_group_call.cpp:897-905,
// 1036-1043), or a page after offset; an empty next_offset ends the list
// (tdesktop :187-205).
func (c *GroupCallsCore) PhoneGetGroupParticipants(in *mtproto.TLPhoneGetGroupParticipants) (*mtproto.Phone_GroupParticipants, error) {
	call, err := c.load(in.GetCall())
	if err != nil {
		return nil, err
	}
	a, err := c.viewable(call)
	if err != nil {
		return nil, err
	}
	page := &groupcalls.Page{}
	switch {
	case call.Discarded:
	case len(in.GetIds()) > 0 || len(in.GetSources()) > 0:
		var peers []groupcalls.PeerKey
		for _, id := range in.GetIds() {
			p := mtproto.FromInputPeer2(c.MD.UserId, id)
			if p.IsUser() || p.IsChannel() {
				peers = append(peers, participantKey(p))
			}
		}
		sources := make([]uint32, 0, len(in.GetSources()))
		for _, s := range in.GetSources() {
			sources = append(sources, uint32(s))
		}
		if page.Participants, err = c.svcCtx.Dao.Store.Lookup(c.ctx, call.Id, peers, sources); err != nil {
			return nil, err
		}
	default:
		if page, err = c.svcCtx.Dao.Store.List(c.ctx, call.Id, in.GetOffset(), clampLimit(in.GetLimit())); err != nil {
			return nil, storeError(err)
		}
	}
	list, users, chats := c.rows(a, call, page.Participants)
	return mtproto.MakeTLPhoneGroupParticipants(&mtproto.Phone_GroupParticipants{
		Count:        call.ParticipantsCount,
		Participants: list,
		NextOffset:   page.Next,
		Chats:        chats,
		Users:        users,
		Version:      call.Version,
	}).To_Phone_GroupParticipants(), nil
}
