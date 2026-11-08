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

package historyttl

import (
	"testing"

	"github.com/teamgram/proto/mtproto"
)

func TestValid(t *testing.T) {
	for period, want := range map[int32]bool{
		0:       true,
		60:      true,
		86400:   true,
		Max:     true,
		1:       false,
		59:      false,
		Max + 1: false,
		-1:      false,
	} {
		if got := Valid(period); got != want {
			t.Errorf("Valid(%d) = %v, want %v", period, got, want)
		}
	}
}

func TestIsChange(t *testing.T) {
	change := mtproto.MakeTLMessageService(&mtproto.Message{Action: Action(86400, 0)}).To_Message()
	if !IsChange(change) {
		t.Error("the service message of a change is not recognized")
	}
	text := mtproto.MakeTLMessage(&mtproto.Message{Message: "hi"}).To_Message()
	if IsChange(text) {
		t.Error("a text message taken for a change")
	}
}

func TestUpdateOffLeavesPeriodOut(t *testing.T) {
	peer := mtproto.MakePeerUser(1)
	if p := Update(peer, 0).GetTtlPeriod(); p != nil {
		t.Errorf("ttl_period of off = %v, want none", p)
	}
	if p := Update(peer, 86400).GetTtlPeriod(); p.GetValue() != 86400 {
		t.Errorf("ttl_period = %v, want 86400", p)
	}
}
