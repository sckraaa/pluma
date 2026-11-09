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
	"testing"

	"github.com/teamgram/teamgram-server/pkg/pushdevices"
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
