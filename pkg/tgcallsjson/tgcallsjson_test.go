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

package tgcallsjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden answer")

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return bytes.TrimSpace(data)
}

func TestParseJoinRequestFixtures(t *testing.T) {
	tests := []struct {
		file   string
		ssrc   uint32
		layers []VideoLayer
	}{
		{"request_tgcalls_audio.json", 1592683471, nil},
		{"request_tgcalls_camera.json", 1219378144, []VideoLayer{
			{1219378145, 1219378146}, {1219378147, 1219378148}, {1219378149, 1219378150},
		}},
		// Video sources of a screen go past 2^31 and arrive negative.
		{"request_tgcalls_screen.json", 2147483646, []VideoLayer{
			{2147483647, 2147483648}, {2147483649, 2147483650},
		}},
		{"request_web.json", 3812047219, nil},
		{"request_web_video.json", 3812047219, []VideoLayer{
			{2911730551, 401812233}, {1027716204, 4106281957}, {3388310452, 980127745},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			req, err := ParseJoinRequest(readFixture(t, tt.file))
			if err != nil {
				t.Fatal(err)
			}
			if req.SSRC != tt.ssrc {
				t.Errorf("ssrc %d, want %d", req.SSRC, tt.ssrc)
			}
			layers, err := req.VideoLayers()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(layers, tt.layers) {
				t.Errorf("layers %v, want %v", layers, tt.layers)
			}
			if req.HasVideo() != (tt.layers != nil) {
				t.Errorf("HasVideo %v", req.HasVideo())
			}
			if fp := req.SHA256Fingerprint(); fp == nil || fp.Setup != SetupPassive {
				t.Errorf("fingerprint %+v", fp)
			}
			if got := len(req.Sources()); got != 1+2*len(tt.layers) {
				t.Errorf("%d sources", got)
			}
		})
	}
}

// TestJoinRequestRoundTrip writes a request as tgcalls does and reads it
// back: the test client relies on it.
func TestJoinRequestRoundTrip(t *testing.T) {
	req, err := ParseJoinRequest(readFixture(t, "request_tgcalls_screen.json"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"ssrc":2147483646`) || !strings.Contains(string(data), `-2147483648`) {
		t.Errorf("sources not written signed: %s", data)
	}
	back, err := ParseJoinRequest(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, req) {
		t.Errorf("round trip:\n%+v\n%+v", back, req)
	}
}

func TestParseJoinRequestRejects(t *testing.T) {
	good := string(readFixture(t, "request_tgcalls_camera.json"))
	tests := []struct {
		name string
		data string
		want error
	}{
		{"empty", ``, ErrMalformed},
		{"not json", `{"ssrc":`, ErrMalformed},
		{"array", `[]`, ErrMalformed},
		{"no ssrc", strings.Replace(good, `"ssrc":1219378144,`, ``, 1), ErrMalformed},
		{"zero ssrc", strings.Replace(good, `"ssrc":1219378144`, `"ssrc":0`, 1), ErrMalformed},
		{"ssrc too large", strings.Replace(good, `"ssrc":1219378144`, `"ssrc":4294967296`, 1), ErrMalformed},
		{"ssrc fraction", strings.Replace(good, `"ssrc":1219378144`, `"ssrc":1.5`, 1), ErrMalformed},
		{"ssrc string", strings.Replace(good, `"ssrc":1219378144`, `"ssrc":"1"`, 1), ErrMalformed},
		{"short ufrag", strings.Replace(good, `"ufrag":"Gk7Q"`, `"ufrag":"Gk7"`, 1), ErrMalformed},
		{"ufrag with colon", strings.Replace(good, `"ufrag":"Gk7Q"`, `"ufrag":"Gk:7Q"`, 1), ErrMalformed},
		{"short pwd", strings.Replace(good, `"pwd":"q3Xz9KfP0aLmB7nR2tYw5vUe"`, `"pwd":"short"`, 1), ErrMalformed},
		{"no fingerprints", strings.Replace(good, `"fingerprints":[{"fingerprint":"3A:91:0C:7E:55:D2:18:AF:60:B4:9E:21:C3:47:FA:0D:88:12:6B:E9:5F:30:A1:7C:D4:2E:B8:63:09:F1:4A:C6","hash":"sha-256","setup":"passive"}]`, `"fingerprints":[]`, 1), ErrMalformed},
		{"sha-1 only", strings.Replace(good, `"fingerprint":"3A:91:0C:7E:55:D2:18:AF:60:B4:9E:21:C3:47:FA:0D:88:12:6B:E9:5F:30:A1:7C:D4:2E:B8:63:09:F1:4A:C6","hash":"sha-256"`, `"fingerprint":"3A:91:0C:7E:55:D2:18:AF:60:B4:9E:21:C3:47:FA:0D:88:12:6B:E9","hash":"sha-1"`, 1), ErrUnsupported},
		{"short fingerprint", strings.Replace(good, `4A:C6"`, `4A"`, 1), ErrMalformed},
		{"bad fingerprint digit", strings.Replace(good, `3A:91`, `3G:91`, 1), ErrMalformed},
		{"setup active", strings.Replace(good, `"setup":"passive"`, `"setup":"active"`, 1), ErrUnsupported},
		{"setup unknown", strings.Replace(good, `"setup":"passive"`, `"setup":"holdconn"`, 1), ErrMalformed},
		{"unknown semantics", strings.Replace(good, `"semantics":"SIM"`, `"semantics":"FEC-FR"`, 1), ErrUnsupported},
		{"four layers", strings.Replace(good, `[1219378145,1219378147,1219378149]`, `[1219378145,1219378147,1219378149,1219378151]`, 1), ErrMalformed},
		{"FID of three", strings.Replace(good, `[1219378145,1219378146]`, `[1219378145,1219378146,1219378151]`, 1), ErrMalformed},
		{"layer without FID", strings.Replace(good, `,{"semantics":"FID","sources":[1219378149,1219378150]}`, ``, 1), ErrMalformed},
		{"FID outside SIM", strings.Replace(good, `[1219378149,1219378150]`, `[1219378151,1219378150]`, 1), ErrMalformed},
		{"source repeats audio", strings.Replace(good, `[1219378145,1219378147,1219378149]`, `[1219378144,1219378147,1219378149]`, 1), ErrMalformed},
		{"too large", `{"ufrag":"` + strings.Repeat("a", MaxRequestSize) + `"}`, ErrMalformed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.data == good {
				t.Fatal("the case does not change the fixture")
			}
			_, err := ParseJoinRequest([]byte(tt.data))
			if !errors.Is(err, tt.want) {
				t.Fatalf("err %v, want %v", err, tt.want)
			}
		})
	}
}

func TestSingleLayerRequest(t *testing.T) {
	// A conference sends one layer: no SIM group, one FID group
	// (GroupInstanceCustomImpl.cpp:3706-3740).
	req := &JoinRequest{SSRC: 10, SourceGroups: []SourceGroup{{Semantics: SemanticsFID, Sources: []uint32{11, 12}}}}
	layers, err := req.VideoLayers()
	if err != nil || !reflect.DeepEqual(layers, []VideoLayer{{11, 12}}) {
		t.Fatalf("%v %v", layers, err)
	}
	req = &JoinRequest{SSRC: 10, SourceGroups: []SourceGroup{
		{Semantics: SemanticsFID, Sources: []uint32{11, 12}},
		{Semantics: SemanticsFID, Sources: []uint32{13, 14}},
	}}
	if _, err := req.VideoLayers(); !errors.Is(err, ErrMalformed) {
		t.Fatalf("two FID groups without SIM: %v", err)
	}
}

func testResponse(t *testing.T) *JoinResponse {
	t.Helper()
	digest := make([]byte, 32)
	for i := range digest {
		digest[i] = byte(i * 7)
	}
	resp, err := BuildResponse(ResponseParams{
		Ufrag:             "sfu4",
		Pwd:               "abcdefghijklmnopqrstuv0123",
		FingerprintSHA256: digest,
		Endpoint:          "e8f3a1c2",
		Addresses: []Address{
			{IP: netip.MustParseAddr("203.0.113.7"), Port: 20600},
			{IP: netip.MustParseAddr("2001:db8::7"), Port: 20600},
			{IP: netip.MustParseAddr("203.0.113.7"), Port: 20601, TCP: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// TestResponseGolden pins the answer byte for byte; regenerate with
// go test ./pkg/tgcallsjson -run Golden -update.
func TestResponseGolden(t *testing.T) {
	data, err := testResponse(t).Marshal()
	if err != nil {
		t.Fatal(err)
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, data, "", "  "); err != nil {
		t.Fatal(err)
	}
	pretty.WriteByte('\n')
	golden := filepath.Join("testdata", "response.json")
	if *update {
		if err := os.WriteFile(golden, pretty.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pretty.Bytes(), want) {
		t.Errorf("answer changed:\n%s", pretty.Bytes())
	}
}

// TestResponseAsTgcallsReadsIt checks the rules of
// GroupJoinResponsePayload::parse and Web's sdpSafety on the answer.
func TestResponseAsTgcallsReadsIt(t *testing.T) {
	data, err := testResponse(t).Marshal()
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	transport := raw["transport"].(map[string]any)
	for _, k := range []string{"ufrag", "pwd"} {
		if _, ok := transport[k].(string); !ok {
			t.Errorf("transport.%s is not a string", k)
		}
	}
	if transport["rtcp-mux"] != true {
		t.Error("rtcp-mux is not true: Chrome requires it")
	}
	for _, f := range transport["fingerprints"].([]any) {
		fp := f.(map[string]any)
		for _, k := range []string{"hash", "fingerprint", "setup"} {
			if _, ok := fp[k].(string); !ok {
				t.Errorf("fingerprint.%s is not a string", k)
			}
		}
		if fp["setup"] != SetupActive {
			t.Errorf("setup %v", fp["setup"])
		}
	}
	cands := transport["candidates"].([]any)
	if len(cands) != 3 {
		t.Fatalf("%d candidates", len(cands))
	}
	for i, c := range cands {
		cand := c.(map[string]any)
		for _, k := range []string{"port", "protocol", "network", "generation", "id", "component", "foundation", "priority", "ip", "type"} {
			s, ok := cand[k].(string)
			if !ok || s == "" || strings.ContainsAny(s, " \r\n") {
				t.Errorf("candidate %d: %s = %#v", i, k, cand[k])
			}
		}
	}
	for _, media := range []string{"audio", "video"} {
		m, ok := raw[media].(map[string]any)
		if !ok {
			t.Fatalf("no %s", media)
		}
		for _, pt := range m["payload-types"].([]any) {
			p := pt.(map[string]any)
			if params, ok := p["parameters"].(map[string]any); ok {
				for k, v := range params {
					// tgcalls keeps string parameters only
					// (GroupJoinPayloadInternal.cpp:201-207).
					if _, ok := v.(string); !ok {
						t.Errorf("%s parameter %s is not a string", media, k)
					}
				}
			}
		}
	}
	if raw["video"].(map[string]any)["endpoint"] != "e8f3a1c2" {
		t.Error("video endpoint")
	}

	parsed, err := ParseJoinResponse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Transport.Candidates) != 3 || parsed.Transport.Candidates[2].TCPType != "passive" {
		t.Errorf("parsed %+v", parsed.Transport.Candidates)
	}
	if parsed.Transport.Candidates[0].Priority <= parsed.Transport.Candidates[2].Priority {
		t.Errorf("UDP must rank above TCP: %s vs %s", parsed.Transport.Candidates[0].Priority, parsed.Transport.Candidates[2].Priority)
	}
}

func TestParseJoinResponseRejectsWhatTgcallsRejects(t *testing.T) {
	data, err := testResponse(t).Marshal()
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []struct{ name, from, to string }{
		{"number port", `"port":"20600"`, `"port":20600`},
		{"no generation", `"generation":"0",`, ``},
		{"no transport", `"transport"`, `"transportX"`},
	} {
		bad := strings.Replace(string(data), mutate.from, mutate.to, 1)
		if bad == string(data) {
			t.Fatalf("%s: no change", mutate.name)
		}
		if _, err := ParseJoinResponse([]byte(bad)); err == nil {
			t.Errorf("%s accepted", mutate.name)
		}
	}
}

func TestBuildResponseRejects(t *testing.T) {
	ok := ResponseParams{
		Ufrag: "sfu4", Pwd: "abcdefghijklmnopqrstuv0123", FingerprintSHA256: make([]byte, 32), Endpoint: "e1",
		Addresses: []Address{{IP: netip.MustParseAddr("192.0.2.1"), Port: 1}},
	}
	if _, err := BuildResponse(ok); err != nil {
		t.Fatal(err)
	}
	for name, p := range map[string]ResponseParams{
		"short pwd":    func() ResponseParams { p := ok; p.Pwd = "x"; return p }(),
		"sha-1":        func() ResponseParams { p := ok; p.FingerprintSHA256 = make([]byte, 20); return p }(),
		"no addresses": func() ResponseParams { p := ok; p.Addresses = nil; return p }(),
		"zero port": func() ResponseParams {
			p := ok
			p.Addresses = []Address{{IP: netip.MustParseAddr("192.0.2.1")}}
			return p
		}(),
		"bad endpoint": func() ResponseParams { p := ok; p.Endpoint = "a b"; return p }(),
	} {
		if _, err := BuildResponse(p); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestFingerprintRoundTrip(t *testing.T) {
	digest := []byte{0x00, 0xab, 0xff, 0x10}
	s := EncodeFingerprint(digest)
	if s != "00:AB:FF:10" {
		t.Fatal(s)
	}
	back, err := DecodeFingerprint(strings.ToLower(s), 4)
	if err != nil || !bytes.Equal(back, digest) {
		t.Fatalf("%x %v", back, err)
	}
}

func TestColibri(t *testing.T) {
	// As tgcalls writes it (GroupInstanceCustomImpl.cpp:3517-3577).
	msg := `{"colibriClass":"ReceiverVideoConstraints","constraints":{"e1":{"maxHeight":720,"minHeight":180},"e2":{"maxHeight":180,"minHeight":180}},"defaultConstraints":{"maxHeight":0},"onStageEndpoints":["e1"]}`
	class, err := ColibriClass([]byte(msg))
	if err != nil || class != ClassReceiverVideoConstraints {
		t.Fatalf("%q %v", class, err)
	}
	m, err := ParseReceiverVideoConstraints([]byte(msg))
	if err != nil {
		t.Fatal(err)
	}
	if m.Constraints["e1"].MaxHeight != 720 || m.Constraints["e2"].MinHeight != 180 || len(m.OnStageEndpoints) != 1 {
		t.Errorf("%+v", m)
	}
	// Web leaves minHeight out when it has none.
	if _, err := ParseReceiverVideoConstraints([]byte(`{"colibriClass":"ReceiverVideoConstraints","constraints":{},"defaultConstraints":{"maxHeight":0},"onStageEndpoints":[]}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseReceiverVideoConstraints([]byte(`{"colibriClass":"SenderVideoConstraints"}`)); err == nil {
		t.Error("wrong class accepted")
	}
	if _, err := ColibriClass(make([]byte, MaxColibriMessage+1)); err == nil {
		t.Error("oversized message accepted")
	}
	want := `{"colibriClass":"SenderVideoConstraints","videoConstraints":{"idealHeight":360}}`
	if got := string(MarshalSenderVideoConstraints(360)); got != want {
		t.Errorf("%s", got)
	}
	back, err := ParseReceiverVideoConstraints(MarshalReceiverVideoConstraints([]string{"e1"}, map[string]VideoConstraint{"e1": {MinHeight: 180, MaxHeight: 720}}))
	if err != nil || back.Constraints["e1"].MaxHeight != 720 {
		t.Fatalf("%+v %v", back, err)
	}
}
