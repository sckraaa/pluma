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
	"errors"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/moderation"
)

// An empty option is the root; a reason leads to the comment page under its
// own option; only that option ends the report, so an empty comment cannot
// be confused with choosing the reason again.
func TestReportTreeSteps(t *testing.T) {
	root := chooseOption("en")
	if root.GetPredicateName() != mtproto.Predicate_reportResultChooseOption || len(root.GetOptions()) != len(reasons) {
		t.Fatalf("root: %v", root)
	}
	for _, o := range root.GetOptions() {
		reason, final, err := stepOf(o.GetOption())
		if err != nil || final || reason != string(o.GetOption()) {
			t.Fatalf("option %q: %v %v %v", o.GetOption(), reason, final, err)
		}
		c := addComment(reason)
		if c.GetPredicateName() != mtproto.Predicate_reportResultAddComment {
			t.Fatalf("reason %q: %v", reason, c)
		}
		r, final, err := stepOf(c.GetOption())
		if err != nil || !final || r != reason {
			t.Fatalf("comment of %q: %v %v %v", reason, r, final, err)
		}
	}
	if !addComment("spam").GetOptional() {
		t.Errorf("the comment of spam is optional")
	}
	if addComment("other").GetOptional() {
		t.Errorf("the comment of other is not optional")
	}
}

func TestReportTreeBadOptions(t *testing.T) {
	for _, bad := range []string{"nonsense", "nonsense:c", ":c"} {
		if _, _, err := stepOf([]byte(bad)); !errors.Is(err, mtproto.ErrOptionInvalid) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if reason, final, err := stepOf(nil); reason != "" || final || err != nil {
		t.Errorf("empty option: %q %v %v", reason, final, err)
	}
}

func TestReportTreeLanguages(t *testing.T) {
	for _, r := range reasons {
		for _, lang := range []string{"en", "ru"} {
			root := chooseOption(lang)
			if root.GetTitle() == "" {
				t.Errorf("no title in %s", lang)
			}
			for _, o := range root.GetOptions() {
				if o.GetText() == "" {
					t.Errorf("option %q has no text in %s", r.key, lang)
				}
			}
		}
	}
}

func TestReasonOf(t *testing.T) {
	spam := mtproto.MakeTLInputReportReasonSpam(nil).To_ReportReason()
	if got := reasonOf(spam); got != "spam" {
		t.Errorf("spam: %q", got)
	}
	if got := reasonOf(nil); got != "other" {
		t.Errorf("nil: %q", got)
	}
}

func TestTargetOf(t *testing.T) {
	const reporter = int64(1)
	peer := &mtproto.PeerUtil{PeerType: mtproto.PEER_USER, PeerId: 2}
	s := &moderation.Snapshot{Messages: []moderation.SnapshotMessage{{Id: 1, FromId: reporter}, {Id: 2, FromId: 3}}}
	if got := targetOf(s, peer, reporter); got != 3 {
		t.Errorf("sender of the first foreign message: %d", got)
	}
	if got := targetOf(&moderation.Snapshot{}, peer, reporter); got != 2 {
		t.Errorf("the reported user: %d", got)
	}
	if got := targetOf(&moderation.Snapshot{}, &mtproto.PeerUtil{PeerType: mtproto.PEER_CHANNEL, PeerId: 2}, reporter); got != 0 {
		t.Errorf("a channel: %d", got)
	}
}

func TestFromIdOf(t *testing.T) {
	in := mtproto.MakeTLMessage(&mtproto.Message{PeerId: mtproto.MakePeerUser(7)}).To_Message()
	if got := fromIdOf(in, 1); got != 7 {
		t.Errorf("incoming private message without from_id: %d", got)
	}
	out := mtproto.MakeTLMessage(&mtproto.Message{Out: true, PeerId: mtproto.MakePeerUser(7)}).To_Message()
	if got := fromIdOf(out, 1); got != 1 {
		t.Errorf("outgoing: %d", got)
	}
	from := mtproto.MakeTLMessage(&mtproto.Message{FromId: mtproto.MakePeerUser(9)}).To_Message()
	if got := fromIdOf(from, 1); got != 9 {
		t.Errorf("from_id: %d", got)
	}
}
