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

	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/teamgram/proto/mtproto"
)

// TestMessageLink: the forms of core.telegram.org/api/links#message-links
// on the server's link host.
func TestMessageLink(t *testing.T) {
	plain := &mtproto.Message{Id: 5}
	album := &mtproto.Message{Id: 6, GroupedId: wrapperspb.Int64(77)}
	inThread := &mtproto.Message{Id: 9, ReplyTo: &mtproto.MessageReplyHeader{ReplyToMsgId: 8, ReplyToTopId: wrapperspb.Int32(3)}}
	directComment := &mtproto.Message{Id: 10, ReplyTo: &mtproto.MessageReplyHeader{ReplyToMsgId: 8}}
	for _, c := range []struct {
		username        string
		msg             *mtproto.Message
		grouped, thread bool
		want            string
	}{
		{"news", plain, false, false, "https://pluma.chat/news/5"},
		{"", plain, false, false, "https://pluma.chat/c/1000000005/5"},
		{"news", album, false, false, "https://pluma.chat/news/6?single"},
		{"news", album, true, false, "https://pluma.chat/news/6"},
		{"news", inThread, false, true, "https://pluma.chat/news/3/9"},
		{"news", inThread, false, false, "https://pluma.chat/news/9"},
		{"news", directComment, false, true, "https://pluma.chat/news/8/10"},
		{"news", directComment, false, false, "https://pluma.chat/news/10"},
	} {
		if got := messageLink(c.username, 1000000005, c.msg, c.grouped, c.thread); got != c.want {
			t.Errorf("%s %v grouped=%v thread=%v: %s, want %s", c.username, c.msg.Id, c.grouped, c.thread, got, c.want)
		}
	}
}
