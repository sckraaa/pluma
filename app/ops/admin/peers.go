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

package main

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/pkg/moderation"

	"github.com/zeromicro/go-zero/core/logx"
)

// The kinds of a peer in a path.
const (
	peerChannel = "channel"
	peerChat    = "chat"
)

// peerPath reads the type and id of a peer path. ok is false when the
// response is already written.
func peerPath(w http.ResponseWriter, r *http.Request) (string, int64, bool) {
	kind := r.PathValue("type")
	if kind != peerChannel && kind != peerChat {
		httpError(w, http.StatusBadRequest, "bad peer")
		return "", 0, false
	}
	id, ok := parseID(w, r.PathValue("id"))
	return kind, id, ok
}

func peerTarget(kind string, id int64) string { return fmt.Sprintf("%s:%d", kind, id) }

// peerTypeOf is the peer type of a report.
func peerTypeOf(kind string) int32 {
	if kind == peerChannel {
		return mtproto.PEER_CHANNEL
	}
	return mtproto.PEER_CHAT
}

func (s *server) handlePeersSearch(w http.ResponseWriter, r *http.Request, a *actor) {
	rows, err := s.store.searchPeers(r.Context(), r.URL.Query().Get("q"), listPage)
	if err != nil {
		s.fail(w, r, "peers", err)
		return
	}
	writeJSON(w, map[string]any{"peers": rows})
}

func (s *server) handlePeerCard(w http.ResponseWriter, r *http.Request, a *actor) {
	kind, id, ok := peerPath(w, r)
	if !ok {
		return
	}
	p, err := s.store.peer(r.Context(), kind, id)
	if err != nil {
		s.fail(w, r, "peer", err)
		return
	}
	if p == nil {
		httpError(w, http.StatusNotFound, "no such peer")
		return
	}
	if !s.journal(w, r, a, "view_peer", peerTarget(kind, id), "") {
		return
	}
	var owner *userRow
	if p.CreatorId != 0 {
		if owner, err = s.store.user(r.Context(), p.CreatorId); err != nil {
			s.fail(w, r, "owner", err)
			return
		}
	}
	reports, err := s.dao.ReportsOfPeer(r.Context(), peerTypeOf(kind), id, 30)
	if err != nil {
		s.fail(w, r, "reports", err)
		return
	}
	history, err := s.dao.AuditLogFiltered(r.Context(), moderation.AuditFilter{Target: peerTarget(kind, id), Limit: 20})
	if err != nil {
		s.fail(w, r, "peer history", err)
		return
	}
	writeJSON(w, map[string]any{"peer": p, "owner": owner, "reports": reports, "history": history})
}

// handlePeerBlock blocks a channel, supergroup or basic group: the service
// stops it for everyone, reversibly.
func (s *server) handlePeerBlock(w http.ResponseWriter, r *http.Request, a *actor) {
	s.setPeerBlocked(w, r, a, true)
}

func (s *server) handlePeerUnblock(w http.ResponseWriter, r *http.Request, a *actor) {
	s.setPeerBlocked(w, r, a, false)
}

func (s *server) setPeerBlocked(w http.ResponseWriter, r *http.Request, a *actor, blocked bool) {
	kind, id, ok := peerPath(w, r)
	if !ok {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if !s.readDangerous(w, r, a, &body) {
		return
	}
	p, err := s.store.peer(r.Context(), kind, id)
	if err != nil {
		s.fail(w, r, "peer", err)
		return
	}
	if p == nil {
		httpError(w, http.StatusNotFound, "no such peer")
		return
	}
	action := "unblock_peer"
	if blocked {
		action = "block_peer"
	}
	if !s.journal(w, r, a, action, peerTarget(kind, id), body.Reason) {
		return
	}
	if kind == peerChannel {
		_, err = s.channel.ChannelSetBlocked(r.Context(), &channel.TLChannelSetBlocked{ChannelId: id, Blocked: mtproto.ToBool(blocked)})
	} else {
		_, err = s.chat.ChatSetBlocked(r.Context(), &chat.TLChatSetBlocked{ChatId: id, Blocked: mtproto.ToBool(blocked)})
	}
	if err != nil {
		s.logWarn(r, action, err)
		httpError(w, http.StatusBadGateway, "the service refused the change")
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handlePeerDelete deletes a channel or a group for everyone, by the same
// RPC as its creator would: only one blocked first, with a separate
// confirmation.
func (s *server) handlePeerDelete(w http.ResponseWriter, r *http.Request, a *actor) {
	kind, id, ok := peerPath(w, r)
	if !ok {
		return
	}
	var body deleteBody
	if !s.readDangerous(w, r, a, &body) {
		return
	}
	if body.Confirm != strconv.FormatInt(id, 10) {
		httpError(w, http.StatusBadRequest, "the confirmation does not match")
		return
	}
	p, err := s.store.peer(r.Context(), kind, id)
	if err != nil {
		s.fail(w, r, "peer", err)
		return
	}
	if p == nil {
		httpError(w, http.StatusNotFound, "no such peer")
		return
	}
	if !p.Blocked {
		httpError(w, http.StatusConflict, "block it first")
		return
	}
	if !s.journal(w, r, a, "delete_peer", peerTarget(kind, id), "") {
		return
	}
	if kind == peerChannel {
		_, err = s.channel.ChannelDeleteChannel(r.Context(), &channel.TLChannelDeleteChannel{SelfId: p.CreatorId, ChannelId: id})
	} else {
		_, err = s.chat.ChatDeleteChat(r.Context(), &chat.TLChatDeleteChat{ChatId: id})
	}
	if err != nil {
		s.logWarn(r, "delete peer", err)
		httpError(w, http.StatusBadGateway, "the service refused the deletion")
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// logWarn records an error of a step, the response tells the client that the
// step failed.
func (s *server) logWarn(r *http.Request, what string, err error) {
	logx.WithContext(r.Context()).Errorf("admin: %s: %v", what, err)
}
