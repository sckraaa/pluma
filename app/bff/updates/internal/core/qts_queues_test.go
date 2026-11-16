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

	"github.com/teamgram/teamgram-server/pkg/qtsqueue"
	"github.com/teamgram/teamgram-server/pkg/secretchat"
)

func secretPage(more bool, qts int32, numbers ...int32) *secretchat.Difference {
	d := &secretchat.Difference{Qts: qts, More: more}
	for _, n := range numbers {
		d.Messages = append(d.Messages, &secretchat.QueuedMessage{Qts: n})
	}
	return d
}

func queuePage(more bool, numbers ...int32) *qtsqueue.Difference {
	d := &qtsqueue.Difference{More: more}
	for _, n := range numbers {
		d.Updates = append(d.Updates, &qtsqueue.Update{Qts: n})
	}
	return d
}

// The two queues of qts make one page: a queue of other updates cut short
// ends the page of the secret chats at its last qts, which becomes the
// state's, and the difference a slice; otherwise both go whole.
func TestMergeQueues(t *testing.T) {
	s := secretPage(false, 9, 2, 4, 7, 9)
	mergeQueues(s, queuePage(false, 3, 5, 6, 8))
	if len(s.Messages) != 4 || s.Qts != 9 || s.More {
		t.Fatalf("both whole: %d messages, qts %d, more %v", len(s.Messages), s.Qts, s.More)
	}

	s = secretPage(false, 9, 2, 4, 7, 9)
	mergeQueues(s, queuePage(true, 3, 5))
	if len(s.Messages) != 2 || s.Messages[1].Qts != 4 || s.Qts != 5 || !s.More || s.MarksState() {
		t.Fatalf("other updates cut at 5: %d messages, qts %d, more %v", len(s.Messages), s.Qts, s.More)
	}

	// The secret chats cut short already: the other updates were read up
	// to their end.
	s = secretPage(true, 4, 2, 4)
	mergeQueues(s, queuePage(false, 3))
	if len(s.Messages) != 2 || s.Qts != 4 || !s.More {
		t.Fatalf("secret chats cut at 4: %d messages, qts %d, more %v", len(s.Messages), s.Qts, s.More)
	}
}
