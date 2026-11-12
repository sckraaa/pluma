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

package groupcalls

import (
	"context"
	"errors"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"

	"github.com/zeromicro/go-zero/core/logx"
)

// The call of a chat as its objects carry it (ADR 0059 §6): chat and
// channel have call_active (a call that is not over, scheduled ones
// included) and call_not_empty (someone is in it); chatFull and
// channelFull have call. They are read from the table with the chat every
// time, not cached, so that a call that starts or ends shows at once.

// ChatState is the call of one chat or channel.
type ChatState struct {
	Active   bool
	NotEmpty bool
	// Call is the inputGroupCall of the call, nil without one.
	Call *mtproto.InputGroupCall
}

// StateOf is the state of call, nil for none.
func StateOf(call *Call) ChatState {
	if call == nil || call.Discarded {
		return ChatState{}
	}
	return ChatState{Active: true, NotEmpty: call.ParticipantsCount > 0, Call: call.Input()}
}

// ChatStates returns the state of the calls of the peers of a type among
// ids: the peers without a call are absent.
func (s *Store) ChatStates(ctx context.Context, peerType int32, ids []int64) (map[int64]ChatState, error) {
	calls, err := s.ActiveOf(ctx, peerType, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]ChatState, len(calls))
	for id, c := range calls {
		out[id] = StateOf(c)
	}
	return out, nil
}

// ChatPlugin answers the chat service (app/service/biz/chat/plugin) about
// the calls of basic groups.
type ChatPlugin struct {
	store *Store
}

// NewChatPlugin returns the plugin over db.
func NewChatPlugin(db *sqlx.DB) *ChatPlugin {
	return &ChatPlugin{store: NewStore(db)}
}

func (p *ChatPlugin) state(ctx context.Context, chatId int64) ChatState {
	call, err := p.store.Active(ctx, mtproto.PEER_CHAT, chatId)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			// The chat goes out without its call; the clients learn it from
			// the next update of the call.
			logx.WithContext(ctx).Errorf("groupcalls: the call of chat %d: %v", chatId, err)
		}
		return ChatState{}
	}
	return StateOf(call)
}

// GetChatCallActiveAndNotEmpty is call_active and call_not_empty of a chat.
func (p *ChatPlugin) GetChatCallActiveAndNotEmpty(ctx context.Context, _ int64, chatId int64) (bool, bool) {
	st := p.state(ctx, chatId)
	return st.Active, st.NotEmpty
}

// GetChatGroupCall is chatFull.call of a chat, nil without a call.
func (p *ChatPlugin) GetChatGroupCall(ctx context.Context, _ int64, chatId int64) *mtproto.InputGroupCall {
	return p.state(ctx, chatId).Call
}
