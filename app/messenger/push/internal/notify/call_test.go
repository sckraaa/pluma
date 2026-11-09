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

package notify

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/teamgram/proto/mtproto"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

func requestedCall() *mtproto.Updates {
	call := mtproto.MakeTLPhoneCallRequested(&mtproto.PhoneCall{
		Id: 555, AccessHash: 777, Date: 1000, AdminId: anna, ParticipantId: me,
		GAHash: make([]byte, 32),
		Protocol: mtproto.MakeTLPhoneCallProtocol(&mtproto.PhoneCallProtocol{
			UdpP2P: true, UdpReflector: true, MinLayer: 65, MaxLayer: 92, LibraryVersions: []string{"9.0.0"},
		}).To_PhoneCallProtocol(),
	}).To_PhoneCall()
	return mtproto.MakeTLUpdates(&mtproto.Updates{
		Updates: []*mtproto.Update{mtproto.MakeTLUpdatePhoneCall(&mtproto.Update{PhoneCall: call}).To_Update()},
		Users:   []*mtproto.User{mtproto.MakeTLUser(&mtproto.User{Id: anna, FirstName: wrapperspb.String("Анна")}).To_User()},
		Chats:   []*mtproto.Chat{},
	}).To_Updates()
}

// The push of a call carries the update to ring on, for iOS as
// core.telegram.org/api/push-updates and NotificationService.swift:1053
// read it, for Android as PHONE_CALL_REQUEST.
func TestCallPush(t *testing.T) {
	ns := FromUpdates(me, requestedCall())
	if len(ns) != 1 || ns[0].Kind != KindCall {
		t.Fatalf("notifications: %+v", ns)
	}
	n := ns[0]
	if n.CallId != 555 || n.CallAccessHash != 777 || n.FromName != "Анна" {
		t.Fatalf("call: %+v", n)
	}
	raw, err := base64.RawURLEncoding.DecodeString(n.CallUpdates)
	if err != nil || len(raw) == 0 {
		t.Fatalf("updates: %q %v", n.CallUpdates, err)
	}

	androidBytes, err := Android(n, false)
	if err != nil {
		t.Fatal(err)
	}
	var a struct {
		LocKey  string         `json:"loc_key"`
		LocArgs []string       `json:"loc_args"`
		Custom  map[string]any `json:"custom"`
	}
	if err := json.Unmarshal(androidBytes, &a); err != nil {
		t.Fatal(err)
	}
	if a.LocKey != "PHONE_CALL_REQUEST" || a.LocArgs[0] != "Анна" || a.Custom["call_ah"] != float64(777) || a.Custom["updates"] != n.CallUpdates {
		t.Fatalf("android: %s", androidBytes)
	}

	p, err := IOS(n, "ru", HideAll) // a call rings, locked or not
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(p.Payload, &m); err != nil {
		t.Fatal(err)
	}
	if m["call_id"] != "555" || m["call_ah"] != "777" || m["from_id"] != "7" || m["updates"] != n.CallUpdates || !p.Alert {
		t.Fatalf("ios: %s", p.Payload)
	}
}

func TestOtherCallUpdatesAreNotPushes(t *testing.T) {
	waiting := mtproto.MakeTLPhoneCallWaiting(&mtproto.PhoneCall{Id: 1}).To_PhoneCall()
	ups := mtproto.MakeTLUpdates(&mtproto.Updates{
		Updates: []*mtproto.Update{mtproto.MakeTLUpdatePhoneCall(&mtproto.Update{PhoneCall: waiting}).To_Update()},
	}).To_Updates()
	if ns := FromUpdates(me, ups); len(ns) != 0 {
		t.Fatalf("notifications: %+v", ns)
	}
}
