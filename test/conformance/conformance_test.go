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

package conformance

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestTree(t *testing.T) {
	sent := mtproto.MakeTLAuthSentCode(&mtproto.Auth_SentCode{
		Type:          mtproto.MakeTLAuthSentCodeTypeSms(&mtproto.Auth_SentCodeType{Length: 5}).To_Auth_SentCodeType(),
		PhoneCodeHash: "abc",
		Timeout:       wrapperspb.Int32(60),
	})
	got := Tree(sent)
	want := map[string]any{
		"_":               mtproto.Predicate_auth_sentCode,
		"type":            map[string]any{"_": mtproto.Predicate_auth_sentCodeTypeSms, "length": int64(5)},
		"phone_code_hash": "abc",
		"timeout":         int64(60),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Tree = %#v\nwant %#v", got, want)
	}

	// Type suffixes of the generated fields are dropped; lists stay lists.
	u := mtproto.MakeTLUpdateReadHistoryInbox(&mtproto.Update{Peer_PEER: mtproto.MakePeerUser(7), MaxId: 9, Pts_INT32: 10, PtsCount: 1}).To_Update()
	tree := Tree(u).(map[string]any)
	if tree["peer"] == nil || tree["max_id"] != int64(9) {
		t.Fatalf("Tree of an update: %#v", tree)
	}
}

func TestLabeler(t *testing.T) {
	l := NewLabeler()
	a := l.Label(map[string]any{"_": "message", "id": int64(500), "date": int64(1700000000), "pts": int64(40),
		"peer": map[string]any{"_": "peerUser", "user_id": int64(136907713)}, "message": "hi"})
	b := l.Label(map[string]any{"_": "updateReadHistoryInbox", "max_id": int64(500), "pts": int64(42),
		"peer": map[string]any{"_": "peerUser", "user_id": int64(136907713)}})
	am, bm := a.(map[string]any), b.(map[string]any)
	// Keys in order: id, then peer.user_id.
	if am["id"] != "$id1" || bm["max_id"] != "$id1" || am["peer"].(map[string]any)["user_id"] != "$id2" ||
		bm["peer"].(map[string]any)["user_id"] != "$id2" {
		t.Fatalf("ids: %v %v", a, b)
	}
	if am["date"] != "$time" || am["pts"] != "$pts+0" || bm["pts"] != "$pts+2" || am["message"] != "hi" {
		t.Fatalf("labels: %v %v", a, b)
	}
	// The ids of deleted messages are the ids of the messages.
	d := l.Label(map[string]any{"_": "updateDeleteMessages", "messages": []any{int64(500), int64(501)}}).(map[string]any)
	if ids := d["messages"].([]any); ids[0] != "$id1" || ids[1] != "$id3" {
		t.Fatalf("deleted ids: %v", d)
	}

	// Who the account is stays out; an empty last name stays empty.
	u := l.Label(map[string]any{"_": "user", "first_name": "Ann", "last_name": "", "phone": "79990001122",
		"usernames": []any{map[string]any{"_": "username", "username": "ann"}, map[string]any{"_": "username", "username": "ann2"}}}).(map[string]any)
	again := l.Label(map[string]any{"_": "user", "first_name": "Ann"}).(map[string]any)
	names := u["usernames"].([]any)
	if u["first_name"] != "$first_name1" || again["first_name"] != "$first_name1" || u["last_name"] != "" || u["phone"] != "$phone1" ||
		names[0].(map[string]any)["username"] != "$username1" || names[1].(map[string]any)["username"] != "$username2" {
		t.Fatalf("personal labels: %v %v", u, again)
	}
}

func fixture(t *testing.T, steps ...Step) *Fixture {
	t.Helper()
	b, _ := json.Marshal(&Fixture{Scenario: "x", Steps: steps})
	var f Fixture
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	return &f
}

func TestCompare(t *testing.T) {
	config := map[string]any{"_": "config", "this_dc": int64(2), "dc_options": []any{map[string]any{"_": "dcOption", "id": "$id1"}},
		"test_mode": map[string]any{"_": "boolTrue"}}
	want := fixture(t,
		Step{Call: "help.getConfig", Result: config},
		Step{Call: "auth.signIn", Error: NewError(400, "PHONE_CODE_INVALID")},
	)
	if d := Compare(want, want, Rules{}); len(d) != 0 {
		t.Fatalf("a fixture differs from itself: %v", d)
	}

	ours := map[string]any{"_": "config", "this_dc": int64(1), "dc_options": []any{map[string]any{"_": "dcOption", "id": "$id1"}, map[string]any{"_": "dcOption", "id": "$id2"}},
		"test_mode": map[string]any{"_": "boolFalse"}, "extra": "x"}
	got := fixture(t,
		Step{Call: "help.getConfig", Result: ours},
		Step{Call: "auth.signIn", Error: NewError(400, "PHONE_CODE_EXPIRED")},
	)
	d := strings.Join(Compare(want, got, Rules{}), "\n")
	for _, s := range []string{"this_dc: 1, want 2", "dc_options: 2 items, want 1", "test_mode: boolFalse, want boolTrue", "extra is extra", "error 400 PHONE_CODE_EXPIRED, want 400 PHONE_CODE_INVALID"} {
		if !strings.Contains(d, s) {
			t.Errorf("no %q in\n%s", s, d)
		}
	}

	// Loose paths compare kinds only; deviations are not compared.
	rules := Rules{
		Loose:      []string{"this_dc", "dc_options", "test_mode"},
		Deviations: []Deviation{{Step: 0, Path: "extra", Reason: "ours"}, {Step: 1, Reason: "ours"}},
	}
	if d := Compare(want, got, rules); len(d) != 0 {
		t.Fatalf("with rules: %v", d)
	}
	if d := Compare(want, fixture(t, Step{Call: "help.getConfig", Result: map[string]any{"_": "config", "this_dc": "two"}}), Rules{Loose: []string{"this_dc"}}); len(d) < 2 {
		t.Fatalf("a loose path of another kind and missing fields: %v", d)
	}
	if NewError(420, "FLOOD_WAIT_37").Message != "FLOOD_WAIT_X" {
		t.Fatal("the number of FLOOD_WAIT is kept")
	}
}

func TestForeign(t *testing.T) {
	self := map[string]any{"_": "user", "self": true, "id": "$id1"}
	own := fixture(t, Step{Call: "updates.getDifference", Result: map[string]any{
		"_":            "updates_difference",
		"new_messages": []any{map[string]any{"_": "message", "message": "conformance: hi"}},
		"users":        []any{self},
		"chats":        []any{},
	}})
	if f := Foreign(own, "conformance: "); len(f) != 0 {
		t.Fatalf("own data is foreign: %v", f)
	}
	other := fixture(t, Step{Call: "updates.getDifference", Result: map[string]any{
		"_":            "updates_difference",
		"new_messages": []any{map[string]any{"_": "message", "message": "Login code: 12345"}},
		"users":        []any{self, map[string]any{"_": "user", "id": "$id2"}},
		"chats":        []any{map[string]any{"_": "chat", "title": "family"}},
	}})
	got := strings.Join(Foreign(other, "conformance: "), "\n")
	for _, s := range []string{"step 0.chats[0]: a chat", "step 0.new_messages[0].message: a text not of the scenario", "step 0.users[1]: a user other than self"} {
		if !strings.Contains(got, s) {
			t.Errorf("no %q in\n%s", s, got)
		}
	}
	if strings.Contains(got, "12345") || strings.Contains(got, "family") {
		t.Errorf("values are in the result: %s", got)
	}
}

func TestSessionPath(t *testing.T) {
	for _, name := range []string{"main", "spare-2"} {
		if _, err := sessionPath(name); err != nil {
			t.Errorf("sessionPath(%q): %v", name, err)
		}
	}
	for _, name := range []string{"", "../main", "a/b", "Main", "-x", strings.Repeat("a", 33)} {
		if _, err := sessionPath(name); err == nil {
			t.Errorf("sessionPath(%q) is accepted", name)
		}
	}
}

func TestLocaleTag(t *testing.T) {
	for in, want := range map[string]string{
		"ru_RU.UTF-8":     "ru-RU",
		"en_US@rg=dezzzz": "en-US",
		"de_DE":           "de-DE",
		"C":               "",
		"POSIX":           "",
		"":                "",
	} {
		if got := localeTag(in); got != want {
			t.Errorf("localeTag(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHostDevice(t *testing.T) {
	d := hostDevice()
	if d.model == "" || d.system == "" || d.langCode == "" || !strings.HasPrefix(d.systemLangCode, d.langCode) {
		t.Fatalf("hostDevice() = %+v", d)
	}
}
