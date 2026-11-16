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
	"context"
	"errors"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/chats/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/chats/internal/svc"
	msg_client "github.com/teamgram/teamgram-server/app/messenger/msg/msg/client"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	chat_client "github.com/teamgram/teamgram-server/app/service/biz/chat/client"
	dialog_client "github.com/teamgram/teamgram-server/app/service/biz/dialog/client"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/translate"
)

const (
	fullChatMe = int64(7)
	fullChatId = int64(42)
)

// fullChatGroup is the group fullChatId with fullChatMe as a plain member.
type fullChatGroup struct{ chat_client.ChatClient }

func (fullChatGroup) ChatGetMutableChat(_ context.Context, in *chatpb.TLChatGetMutableChat) (*mtproto.MutableChat, error) {
	if in.ChatId != fullChatId {
		return nil, mtproto.ErrChatIdInvalid
	}
	return mtproto.MakeTLMutableChat(&mtproto.MutableChat{
		Chat: mtproto.MakeTLImmutableChat(&mtproto.ImmutableChat{Id: fullChatId, ParticipantsCount: 1}).To_ImmutableChat(),
		ChatParticipants: []*mtproto.ImmutableChatParticipant{
			mtproto.MakeTLImmutableChatParticipant(&mtproto.ImmutableChatParticipant{
				ChatId: fullChatId, UserId: fullChatMe, State: mtproto.ChatMemberStateNormal, ParticipantType: mtproto.ChatMemberNormal,
			}).To_ImmutableChatParticipant(),
		},
	}).To_MutableChat(), nil
}

type fullChatDialogs struct{ dialog_client.DialogClient }

func (fullChatDialogs) DialogGetDialogsByIdList(_ context.Context, in *dialog.TLDialogGetDialogsByIdList) (*dialog.Vector_DialogExt, error) {
	return &dialog.Vector_DialogExt{Datas: []*dialog.DialogExt{{
		Dialog: mtproto.MakeTLDialog(&mtproto.Dialog{Peer: mtproto.MakePeerChat(fullChatId)}).To_Dialog(),
	}}}, nil
}

type fullChatMsgs struct{ msg_client.MsgClient }

func (fullChatMsgs) MsgHasScheduled(context.Context, *msgpb.TLMsgHasScheduled) (*mtproto.Bool, error) {
	return mtproto.BoolFalse, nil
}

type fullChatUsers struct{ user_client.UserClient }

func (fullChatUsers) UserGetNotifySettings(context.Context, *userpb.TLUserGetNotifySettings) (*mtproto.PeerNotifySettings, error) {
	return mtproto.MakeTLPeerNotifySettings(nil).To_PeerNotifySettings(), nil
}

func (fullChatUsers) UserGetMutableUsers(context.Context, *userpb.TLUserGetMutableUsers) (*userpb.Vector_ImmutableUser, error) {
	return &userpb.Vector_ImmutableUser{}, nil
}

// translationSettings is peer_translation_settings of fullChatMe.
type translationSettings struct {
	hidden map[translate.TranslationPeer]bool
	err    error
	asked  []translate.TranslationPeer
	user   int64
}

func (s *translationSettings) TranslationsDisabled(_ context.Context, userId int64, peers ...translate.TranslationPeer) (map[translate.TranslationPeer]bool, error) {
	s.user = userId
	s.asked = append(s.asked, peers...)
	if s.err != nil {
		return nil, s.err
	}
	disabled := make(map[translate.TranslationPeer]bool)
	for _, p := range peers {
		if s.hidden[p] {
			disabled[p] = true
		}
	}
	return disabled, nil
}

func fullChatCore(settings *translationSettings) *ChatsCore {
	core := New(context.Background(), &svc.ServiceContext{
		Dao: &dao.Dao{
			ChatClient:   chat_client.NewChatClientHelperOf(fullChatGroup{}),
			DialogClient: fullChatDialogs{},
			MsgClient:    fullChatMsgs{},
			UserClient:   fullChatUsers{},
		},
		Translations: settings,
	})
	core.MD = &metadata.RpcMetadata{UserId: fullChatMe}
	return core
}

// chatFull.translations_disabled is what the member chose with
// messages.togglePeerTranslations for this group, and only for it.
func TestGetFullChatTranslationsDisabled(t *testing.T) {
	group := translate.TranslationPeer{Type: mtproto.PEER_CHAT, Id: fullChatId}
	for _, tc := range []struct {
		name   string
		hidden map[translate.TranslationPeer]bool
		want   bool
	}{
		{"hidden", map[translate.TranslationPeer]bool{group: true}, true},
		{"shown", nil, false},
		{"another chat hidden", map[translate.TranslationPeer]bool{
			{Type: mtproto.PEER_CHANNEL, Id: fullChatId}: true,
			{Type: mtproto.PEER_USER, Id: fullChatId}:    true,
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings := &translationSettings{hidden: tc.hidden}
			full, err := fullChatCore(settings).MessagesGetFullChat(&mtproto.TLMessagesGetFullChat{ChatId: fullChatId})
			if err != nil {
				t.Fatal(err)
			}
			if got := full.GetFullChat().GetTranslationsDisabled(); got != tc.want {
				t.Errorf("translations_disabled %v, want %v", got, tc.want)
			}
			if settings.user != fullChatMe || len(settings.asked) != 1 || settings.asked[0] != group {
				t.Errorf("asked the settings of %d for %v, want %d for %v", settings.user, settings.asked, fullChatMe, group)
			}
		})
	}
}

// The settings failing to read fail the full chat rather than show a bar
// the member hid.
func TestGetFullChatTranslationsFailure(t *testing.T) {
	failure := errors.New("mysql is down")
	_, err := fullChatCore(&translationSettings{err: failure}).MessagesGetFullChat(&mtproto.TLMessagesGetFullChat{ChatId: fullChatId})
	if !errors.Is(err, failure) {
		t.Errorf("error %v, want %v", err, failure)
	}
}
