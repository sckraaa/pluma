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

package reporttree

import (
	"errors"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

// An empty option is the root; a reason leads to the comment page under its
// own option; only that option ends the report, so an empty comment cannot
// be confused with choosing the reason again.
func TestReportTreeSteps(t *testing.T) {
	root := ChooseOption("en")
	if root.GetPredicateName() != mtproto.Predicate_reportResultChooseOption || len(root.GetOptions()) != len(reasons) {
		t.Fatalf("root: %v", root)
	}
	for _, o := range root.GetOptions() {
		reason, final, err := StepOf(o.GetOption())
		if err != nil || final || reason != string(o.GetOption()) {
			t.Fatalf("option %q: %v %v %v", o.GetOption(), reason, final, err)
		}
		c := AddComment(reason)
		if c.GetPredicateName() != mtproto.Predicate_reportResultAddComment {
			t.Fatalf("reason %q: %v", reason, c)
		}
		r, final, err := StepOf(c.GetOption())
		if err != nil || !final || r != reason {
			t.Fatalf("comment of %q: %v %v %v", reason, r, final, err)
		}
	}
	if !AddComment("spam").GetOptional() {
		t.Errorf("the comment of spam is optional")
	}
	if AddComment("other").GetOptional() {
		t.Errorf("the comment of other is not optional")
	}
}

func TestReportTreeBadOptions(t *testing.T) {
	for _, bad := range []string{"nonsense", "nonsense:c", ":c"} {
		if _, _, err := StepOf([]byte(bad)); !errors.Is(err, mtproto.ErrOptionInvalid) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if reason, final, err := StepOf(nil); reason != "" || final || err != nil {
		t.Errorf("empty option: %q %v %v", reason, final, err)
	}
}

func TestReportTreeLanguages(t *testing.T) {
	for _, r := range reasons {
		for _, lang := range []string{"en", "ru"} {
			root := ChooseOption(lang)
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
	if got := ReasonOf(spam); got != "spam" {
		t.Errorf("spam: %q", got)
	}
	if got := ReasonOf(nil); got != "other" {
		t.Errorf("nil: %q", got)
	}
}
