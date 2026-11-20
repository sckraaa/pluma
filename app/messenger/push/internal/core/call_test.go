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
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/pushdevices"
	"github.com/teamgram/teamgram-server/pkg/pushprovider"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestOneChannelPerDevice(t *testing.T) {
	devices := []*pushdevices.Device{
		{Id: 1, AuthKeyId: 10, TokenType: pushdevices.TokenAPNs},
		{Id: 2, AuthKeyId: 10, TokenType: pushdevices.TokenVoIP},
		{Id: 3, AuthKeyId: 20, TokenType: pushdevices.TokenAPNs},
		{Id: 4, AuthKeyId: 30, TokenType: pushdevices.TokenFCM},
	}
	out := oneChannelPerDevice(devices)
	got := map[int64]bool{}
	for _, d := range out {
		got[d.Id] = true
	}
	if got[1] || !got[2] || !got[3] || !got[4] {
		t.Fatalf("devices: %+v", got)
	}
}

func TestPushable(t *testing.T) {
	if pushable(pushdevices.TokenVoIP, false) {
		t.Fatal("VoIP token used for a message")
	}
	if !pushable(pushdevices.TokenVoIP, true) || !pushable(pushdevices.TokenAPNs, true) || !pushable(pushdevices.TokenFCM, false) {
		t.Fatal("expected pushable")
	}
	if pushable(pushdevices.TokenWebPush, true) {
		t.Fatal("web push is not sent")
	}
}

// An invitation to a conference rings as a call: on an iPhone with a VoIP
// token by PushKit alone, whatever the chat is muted, and it expires as a
// call does.
func TestConferenceInviteRouting(t *testing.T) {
	e := newEnv(t)
	e.register(t, keyIOS, pushdevices.TokenVoIP, "voip-token", "ru", true)
	e.settings[settingsKey{peerType: mtproto.PEER_USER, peerId: anna}] = &mtproto.PeerNotifySettings{MuteUntil: wrapperspb.Int32(int32(time.Now().Add(time.Hour).Unix()))}
	m := mtproto.MakeTLMessageService(&mtproto.Message{
		Id: 41, FromId: mtproto.MakePeerUser(anna), PeerId: mtproto.MakePeerUser(anna),
		Action: mtproto.MakeTLMessageActionConferenceCall(&mtproto.MessageAction{CallId: 555}).To_MessageAction(),
	}).To_Message()
	e.core.Handle(context.Background(), &Event{UserId: me, Updates: mtproto.MakeTLUpdates(&mtproto.Updates{
		Updates: []*mtproto.Update{mtproto.MakeTLUpdateNewMessage(&mtproto.Update{Message_MESSAGE: m}).To_Update()},
		Users:   []*mtproto.User{mtproto.MakeTLUser(&mtproto.User{Id: anna, FirstName: wrapperspb.String("Анна")}).To_User()},
	}).To_Updates()})
	got := e.take(t)
	if len(got) != 2 || got["apns-token"] != nil {
		t.Fatalf("pushes to %v, want fcm-token and voip-token", keys(got))
	}
	if got["voip-token"]["group_call_id"] != "555" || got["fcm-token"]["loc_key"] != "CONF_CALL_REQUEST" {
		t.Fatalf("pushes: %v", got)
	}
	if p := e.lastAPNs(); p.Type != pushprovider.APNsVoIP || p.Expiration.After(time.Now().Add(callExpiration+time.Second)) {
		t.Fatalf("APNs request: %+v", p)
	}
}
