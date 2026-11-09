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

package phonecalls

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

func TestNegotiate(t *testing.T) {
	a := Protocol{UdpP2P: true, UdpReflector: true, MinLayer: 65, MaxLayer: 92, LibraryVersions: []string{"10.0.0", "9.0.0"}}
	b := Protocol{UdpP2P: true, UdpReflector: true, MinLayer: 70, MaxLayer: 90, LibraryVersions: []string{"9.0.0", "10.0.0", "8.0.0"}}
	got, ok := Negotiate(a, b)
	// The greatest common version as a string, whatever the order of the lists.
	want := Protocol{UdpP2P: true, UdpReflector: true, MinLayer: 70, MaxLayer: 90, LibraryVersions: []string{"9.0.0"}}
	if !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("Negotiate = %+v %v, want %+v", got, ok, want)
	}
	// Android as the caller, its list in the order of the registry, and an
	// iOS or desktop client: never a version Android takes for an old one.
	android := []string{"10.0.0", "11.0.0", "12.0.0", "13.0.0", "2.4.4", "2.7.7", "5.0.0", "7.0.0", "8.0.0", "9.0.0"}
	desktop := []string{"9.0.0", "8.0.0", "7.0.0", "5.0.0", "2.7.7", "13.0.0", "12.0.0", "11.0.0", "10.0.0"}
	got, ok = Negotiate(Protocol{MinLayer: 65, MaxLayer: 92, LibraryVersions: android}, Protocol{MinLayer: 65, MaxLayer: 92, LibraryVersions: desktop})
	if !ok || got.LibraryVersions[0] != "9.0.0" {
		t.Fatalf("Android and desktop: %+v %v", got, ok)
	}
	// Web K speaks one version.
	got, ok = Negotiate(Protocol{MinLayer: 65, MaxLayer: 92, LibraryVersions: android}, Protocol{MinLayer: 92, MaxLayer: 92, LibraryVersions: []string{"13.0.0"}})
	if !ok || got.LibraryVersions[0] != "13.0.0" {
		t.Fatalf("Android and Web K: %+v %v", got, ok)
	}
	if _, ok := Negotiate(a, Protocol{MinLayer: 93, MaxLayer: 95, LibraryVersions: []string{"9.0.0"}}); ok {
		t.Fatal("no common layer: negotiated")
	}
	if _, ok := Negotiate(a, Protocol{MinLayer: 65, MaxLayer: 92, LibraryVersions: []string{"7.0.0"}}); ok {
		t.Fatal("no common library: negotiated")
	}
}

func TestViews(t *testing.T) {
	c := &Call{
		Id: 7, AccessHash: 8, AdminId: 1, ParticipantId: 2, AdminAuthKeyId: 100, Date: 1000,
		GAHash: []byte("hash"), GB: []byte("b"), GA: []byte("a"), KeyFingerprint: 9,
		AdminProtocol:   Protocol{LibraryVersions: []string{"x"}},
		AcceptProtocol:  Protocol{LibraryVersions: []string{"y"}},
		ConfirmProtocol: Protocol{LibraryVersions: []string{"z"}},
		State:           StateRequested,
	}
	if p := c.TL(Participant, nil); p.GetPredicateName() != mtproto.Predicate_phoneCallRequested || string(p.GetGAHash()) != "hash" {
		t.Fatalf("participant while requested: %s", p.GetPredicateName())
	}
	p := c.TL(Admin, nil)
	if p.GetPredicateName() != mtproto.Predicate_phoneCallWaiting || p.GetReceiveDate() != nil {
		t.Fatalf("admin while requested: %v", p)
	}
	c.ReceiveDate = 1001
	if got := c.TL(Admin, nil).GetReceiveDate(); got == nil || got.GetValue() != 1001 {
		t.Fatalf("receive_date: %v", got)
	}
	c.State = StateAccepted
	if p := c.TL(Admin, nil); p.GetPredicateName() != mtproto.Predicate_phoneCallAccepted || string(p.GetGB()) != "b" {
		t.Fatalf("admin while accepted: %v", p)
	}
	if p := c.TL(Participant, nil); p.GetPredicateName() != mtproto.Predicate_phoneCallWaiting {
		t.Fatalf("participant while accepted: %s", p.GetPredicateName())
	}
	c.State, c.StartDate = StateActive, 1010
	conns := []*mtproto.PhoneConnection{mtproto.MakeTLPhoneConnectionWebrtc(&mtproto.PhoneConnection{Id: 1}).To_PhoneConnection()}
	if p := c.TL(Admin, conns); p.GetPredicateName() != mtproto.Predicate_phoneCall || string(p.GetGAOrB()) != "b" || len(p.GetConnections()) != 1 {
		t.Fatalf("admin while active: %v", p)
	}
	if p := c.TL(Participant, conns); string(p.GetGAOrB()) != "a" || p.GetKeyFingerprint() != 9 || p.GetStartDate() != 1010 {
		t.Fatalf("participant while active: %v", p)
	}
	c.State, c.Reason, c.Duration = StateDiscarded, ReasonHangup, 42
	p = c.TL(Admin, nil)
	if p.GetPredicateName() != mtproto.Predicate_phoneCallDiscarded || p.GetDuration().GetValue() != 42 ||
		p.GetReason().GetPredicateName() != mtproto.Predicate_phoneCallDiscardReasonHangup {
		t.Fatalf("discarded: %v", p)
	}
}

func TestReasonOf(t *testing.T) {
	missed := mtproto.MakeTLPhoneCallDiscardReasonMissed(nil).To_PhoneCallDiscardReason()
	if ReasonOf(missed) != ReasonMissed {
		t.Fatal("missed")
	}
	if ReasonOf(mtproto.MakeTLPhoneCallDiscardReasonMigrateConferenceCall(nil).To_PhoneCallDiscardReason()) != ReasonHangup {
		t.Fatal("a reason of a group call is a hang-up")
	}
	if TLReason(ReasonNone) != nil {
		t.Fatal("no reason: nil")
	}
}

// The timeouts of the sweep are those the clients read from help.getConfig:
// a server shorter than the clients ends a call they still ring.
func TestTimeoutsMatchClientConfig(t *testing.T) {
	data, err := os.ReadFile("../../teamgramd/bin/config.json")
	if err != nil {
		t.Fatal(err)
	}
	var wrap struct {
		Data struct {
			Receive int `json:"call_receive_timeout_ms"`
			Ring    int `json:"call_ring_timeout_ms"`
			Connect int `json:"call_connect_timeout_ms"`
		} `json:"data2"`
	}
	if err = json.Unmarshal(data, &wrap); err != nil {
		t.Fatal(err)
	}
	cfg := wrap.Data
	for name, v := range map[string][2]time.Duration{
		"receive": {ReceiveTimeout, time.Duration(cfg.Receive) * time.Millisecond},
		"ring":    {RingTimeout, time.Duration(cfg.Ring) * time.Millisecond},
		"connect": {ConnectTimeout, time.Duration(cfg.Connect) * time.Millisecond},
	} {
		if v[0] != v[1] {
			t.Errorf("%s: sweep %v, clients %v", name, v[0], v[1])
		}
	}
}

func TestViewOf(t *testing.T) {
	c := &Call{Id: 1, AdminId: 10, ParticipantId: 20, AdminAuthKeyId: 100, ParticipantAuthKeyId: 200, State: StateAccepted, Reason: ReasonNone}
	if role, v := c.ViewOf(100); role != Admin || v.State != StateAccepted {
		t.Fatalf("admin: %v %+v", role, v)
	}
	if role, v := c.ViewOf(200); role != Participant || v.State != StateAccepted {
		t.Fatalf("the device that accepted: %v %+v", role, v)
	}
	if role, v := c.ViewOf(300); role != Participant || v.State != StateDiscarded || v.Reason != ReasonNone {
		t.Fatalf("another device: %v %+v", role, v)
	}
	if c.State != StateAccepted {
		t.Fatal("the call itself changed")
	}
	ended := &Call{AdminAuthKeyId: 100, State: StateDiscarded, Reason: ReasonMissed}
	if _, v := ended.ViewOf(300); v.Reason != ReasonMissed {
		t.Fatalf("a call nobody accepted keeps its reason: %+v", v)
	}
}
