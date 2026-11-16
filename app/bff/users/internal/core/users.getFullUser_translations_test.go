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
	"github.com/teamgram/teamgram-server/app/bff/users/internal/svc"
	"github.com/teamgram/teamgram-server/pkg/translate"
)

// translationSettings is peer_translation_settings of one reader.
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

func translationsCore(me int64, settings *translationSettings) *UsersCore {
	core := New(context.Background(), &svc.ServiceContext{Translations: settings})
	core.MD = &metadata.RpcMetadata{UserId: me}
	return core
}

// userFull.translations_disabled is what the reader chose with
// messages.togglePeerTranslations for the chat with that user: Saved
// Messages under its own id, a group or a channel of the same id is
// another chat.
func TestGetFullUserTranslationsDisabled(t *testing.T) {
	const me, other = int64(7), int64(8)
	user := func(id int64) translate.TranslationPeer {
		return translate.TranslationPeer{Type: mtproto.PEER_USER, Id: id}
	}
	for _, tc := range []struct {
		name   string
		peer   int64
		hidden map[translate.TranslationPeer]bool
		want   bool
	}{
		{"hidden", other, map[translate.TranslationPeer]bool{user(other): true}, true},
		{"shown", other, nil, false},
		{"saved messages hidden", me, map[translate.TranslationPeer]bool{user(me): true}, true},
		{"another chat hidden", other, map[translate.TranslationPeer]bool{
			user(me):                                true,
			{Type: mtproto.PEER_CHAT, Id: other}:    true,
			{Type: mtproto.PEER_CHANNEL, Id: other}: true,
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings := &translationSettings{hidden: tc.hidden}
			got, err := translationsCore(me, settings).translationsDisabled(tc.peer)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("translations_disabled %v, want %v", got, tc.want)
			}
			if settings.user != me || len(settings.asked) != 1 || settings.asked[0] != user(tc.peer) {
				t.Errorf("asked the settings of %d for %v, want %d for %v", settings.user, settings.asked, me, user(tc.peer))
			}
		})
	}
}

func TestGetFullUserTranslationsFailure(t *testing.T) {
	failure := errors.New("mysql is down")
	if _, err := translationsCore(7, &translationSettings{err: failure}).translationsDisabled(8); !errors.Is(err, failure) {
		t.Errorf("error %v, want %v", err, failure)
	}
}
