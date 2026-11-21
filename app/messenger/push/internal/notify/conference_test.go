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
	"encoding/json"
	"testing"

	"github.com/teamgram/proto/mtproto"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

const bob = 9

func conferenceMessage(a *mtproto.MessageAction, out bool) *mtproto.Message {
	return mtproto.MakeTLMessageService(&mtproto.Message{
		Id: 41, Out: out, FromId: mtproto.MakePeerUser(anna), PeerId: mtproto.MakePeerUser(anna), Date: 1000,
		Action: mtproto.MakeTLMessageActionConferenceCall(a).To_MessageAction(),
	}).To_Message()
}

func conferenceUpdates(predicate string, m *mtproto.Message) *mtproto.Updates {
	u := &mtproto.Update{Message_MESSAGE: m}
	var up *mtproto.Update
	if predicate == mtproto.Predicate_updateEditMessage {
		up = mtproto.MakeTLUpdateEditMessage(u).To_Update()
	} else {
		up = mtproto.MakeTLUpdateNewMessage(u).To_Update()
	}
	return mtproto.MakeTLUpdates(&mtproto.Updates{
		Updates: []*mtproto.Update{up},
		Users: []*mtproto.User{
			mtproto.MakeTLUser(&mtproto.User{Id: anna, FirstName: wrapperspb.String("Анна")}).To_User(),
			mtproto.MakeTLUser(&mtproto.User{Id: bob, FirstName: wrapperspb.String("Боб")}).To_User(),
		},
		Chats: []*mtproto.Chat{},
	}).To_Updates()
}

// An invitation to a conference rings: Android reads loc_args[2], call_id
// and msg_id (PushListenerController.java:367-384), the notification
// extension of iOS CONF_CALL_REQUEST with call_conference_from
// (NotificationService.swift:1095-1113), PushKit from_id, group_call_id,
// msg_id and from_title (AppDelegate.swift:2345).
func TestConferenceInvitePush(t *testing.T) {
	m := conferenceMessage(&mtproto.MessageAction{CallId: 555, Video: true, OtherParticipants: []*mtproto.Peer{mtproto.MakePeerUser(bob)}}, false)
	ns := FromUpdates(me, conferenceUpdates(mtproto.Predicate_updateNewMessage, m))
	if len(ns) != 1 || ns[0].Kind != KindConferenceInvite || !ns[0].IsCall() {
		t.Fatalf("notifications: %+v", ns)
	}
	n := ns[0]
	if n.CallId != 555 || n.MsgId != 41 || n.FromName != "Анна" || n.CallOthersCount != 1 || len(n.CallOthers) != 1 {
		t.Fatalf("invitation: %+v", n)
	}

	raw, err := Android(n, true)
	if err != nil {
		t.Fatal(err)
	}
	var a struct {
		LocKey  string         `json:"loc_key"`
		LocArgs []string       `json:"loc_args"`
		Custom  map[string]any `json:"custom"`
	}
	if err = json.Unmarshal(raw, &a); err != nil {
		t.Fatal(err)
	}
	if a.LocKey != "CONF_VIDEOCALL_REQUEST" || len(a.LocArgs) != 3 || a.LocArgs[2] != "Анна, Боб" ||
		a.Custom["call_id"] != float64(555) || a.Custom["msg_id"] != float64(41) || a.Custom["from_id"] != float64(anna) {
		t.Fatalf("android: %s", raw)
	}

	p, err := IOS(n, "ru", HideAll)
	if err != nil || p == nil || !p.Alert {
		t.Fatalf("ios: %+v %v", p, err)
	}
	var e map[string]any
	if err = json.Unmarshal(p.Payload, &e); err != nil {
		t.Fatal(err)
	}
	if e["loc-key"] != "CONF_VIDEOCALL_REQUEST" || e["call_id"] != "555" || e["msg_id"] != "41" || e["from_id"] != "7" ||
		e["call_conference_from"] != "Анна" || e["call_participants_cnt"] != "1" {
		t.Fatalf("ios extension: %s", p.Payload)
	}

	voip, err := IOSConferenceVoIP(n)
	if err != nil {
		t.Fatal(err)
	}
	var k map[string]string
	if err = json.Unmarshal(voip, &k); err != nil {
		t.Fatal(err)
	}
	if k["from_id"] != "7" || k["group_call_id"] != "555" || k["msg_id"] != "41" || k["from_title"] != "Анна" || k["video"] != "1" || k["member_count"] != "1" {
		t.Fatalf("pushkit: %s", voip)
	}
	if _, ok := k["phoneNumber"]; ok {
		t.Fatal("the number in a push")
	}
}

// Only an incoming invitation that still rings is a push; an answered one,
// an outgoing one or another edit is none. A missed one is Android's
// CONF_CALL_MISSED (PushListenerController.java:1407-1415).
func TestConferenceInviteStates(t *testing.T) {
	for name, a := range map[string]*mtproto.MessageAction{
		"active":   {CallId: 1, Active: true},
		"missed":   {CallId: 1, Missed: true},
		"duration": {CallId: 1, Duration: wrapperspb.Int32(60)},
	} {
		if ns := FromUpdates(me, conferenceUpdates(mtproto.Predicate_updateNewMessage, conferenceMessage(a, false))); len(ns) != 0 {
			t.Fatalf("%s: %+v", name, ns)
		}
	}
	if ns := FromUpdates(me, conferenceUpdates(mtproto.Predicate_updateNewMessage, conferenceMessage(&mtproto.MessageAction{CallId: 1}, true))); len(ns) != 0 {
		t.Fatalf("outgoing: %+v", ns)
	}
	if ns := FromUpdates(me, conferenceUpdates(mtproto.Predicate_updateEditMessage, conferenceMessage(&mtproto.MessageAction{CallId: 1, Active: true}, false))); len(ns) != 0 {
		t.Fatalf("an edit to active: %+v", ns)
	}
	ns := FromUpdates(me, conferenceUpdates(mtproto.Predicate_updateEditMessage, conferenceMessage(&mtproto.MessageAction{CallId: 3, Missed: true, OtherParticipants: []*mtproto.Peer{mtproto.MakePeerUser(bob)}}, false)))
	if len(ns) != 1 || ns[0].Kind != KindConferenceMissed {
		t.Fatalf("missed: %+v", ns)
	}
	key, args := LocKey(ns[0], false)
	if key != "CONF_CALL_MISSED" || len(args) != 2 || args[0] != "Анна" || args[1] != "1" {
		t.Fatalf("missed: %s %v", key, args)
	}
	if p, err := IOS(ns[0], "ru", HideNone); err != nil || p != nil {
		t.Fatalf("ios of a missed invitation: %+v %v", p, err)
	}
}
