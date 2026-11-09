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

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestOverTopic(t *testing.T) {
	own := &mtproto.PeerNotifySettings{MuteUntil: wrapperspb.Int32(0), Silent: mtproto.BoolTrue}
	if got := overTopic(own, nil); got != own {
		t.Errorf("no settings of the topic: %v", got)
	}
	got := overTopic(own, &mtproto.PeerNotifySettings{MuteUntil: wrapperspb.Int32(9)})
	if got.GetMuteUntil().GetValue() != 9 || !mtproto.FromBool(got.GetSilent()) {
		t.Errorf("the topic's mute over the dialog's silent: %v", got)
	}
	if got := overTopic(nil, &mtproto.PeerNotifySettings{MuteUntil: wrapperspb.Int32(3)}); got.GetMuteUntil().GetValue() != 3 {
		t.Errorf("no dialog settings: %v", got)
	}
}
