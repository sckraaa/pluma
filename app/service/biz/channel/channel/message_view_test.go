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

package channel

import (
	"testing"

	"github.com/teamgram/proto/mtproto"
)

func TestSentAs(t *testing.T) {
	group := mtproto.MakePeerChannel(100)
	asChannel := &mtproto.Message{PeerId: group, FromId: mtproto.MakePeerChannel(200)}
	asGroup := &mtproto.Message{PeerId: group, FromId: group}
	fromUser := &mtproto.Message{PeerId: group, FromId: mtproto.MakePeerUser(7)}
	post := &mtproto.Message{PeerId: mtproto.MakePeerChannel(200), Post: true}
	signedPost := &mtproto.Message{PeerId: mtproto.MakePeerChannel(200), Post: true, FromId: mtproto.MakePeerChannel(200)}
	postCopy := &mtproto.Message{PeerId: group, FromId: mtproto.MakePeerChannel(200), FwdFrom: &mtproto.MessageFwdHeader{
		FromId: mtproto.MakePeerChannel(200), SavedFromPeer: mtproto.MakePeerChannel(200),
	}}
	forwardedAs := &mtproto.Message{PeerId: group, FromId: mtproto.MakePeerChannel(200), FwdFrom: &mtproto.MessageFwdHeader{
		FromId: mtproto.MakePeerUser(9),
	}}
	for name, tc := range map[string]struct {
		m    *mtproto.Message
		want bool
	}{
		"as a channel":           {asChannel, true},
		"as the group":           {asGroup, true},
		"forwarded as a channel": {forwardedAs, true},
		"from a user":            {fromUser, false},
		"a post":                 {post, false},
		"a signed post":          {signedPost, false},
		"the copy of a post":     {postCopy, false},
	} {
		if got := SentAs(tc.m); got != tc.want {
			t.Errorf("%s: SentAs = %v, want %v", name, got, tc.want)
		}
	}
}

func TestViewSentMessage(t *testing.T) {
	m := &mtproto.Message{PeerId: mtproto.MakePeerChannel(100), FromId: mtproto.MakePeerChannel(200)}
	if !ViewSentMessage(m, 7, true).GetOut() {
		t.Error("the author's view of its message as a channel is not out")
	}
	if ViewSentMessage(m, 8, false).GetOut() {
		t.Error("another member's view is out")
	}
	if ViewSentMessage(m, 0, true).GetOut() {
		t.Error("no viewer sees it out")
	}
	if m.GetOut() {
		t.Error("the stored message changed")
	}
	own := &mtproto.Message{PeerId: mtproto.MakePeerChannel(100), FromId: mtproto.MakePeerUser(7)}
	if !ViewSentMessage(own, 7, false).GetOut() {
		t.Error("a message from the viewer itself is not out")
	}
}
