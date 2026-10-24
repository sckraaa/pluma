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
	"context"
	"errors"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"

	"github.com/zeromicro/go-zero/core/logx"
)

const testUser = 7

// testCore checks folders of testUser; it has no service clients, so the
// cases here must be decided before a peer is looked up.
func testCore() *FoldersCore {
	ctx := context.Background()
	return &FoldersCore{ctx: ctx, Logger: logx.WithContext(ctx), MD: &metadata.RpcMetadata{UserId: testUser}}
}

func title(text string, entities ...*mtproto.MessageEntity) *mtproto.TextWithEntities {
	return mtproto.MakeTLTextWithEntities(&mtproto.TextWithEntities{Text: text, Entities: entities}).To_TextWithEntities()
}

func folder(id int32, name string) *mtproto.DialogFilter {
	return mtproto.MakeTLDialogFilter(&mtproto.DialogFilter{
		Id:                     id,
		Title_TEXTWITHENTITIES: title(name),
		Contacts:               true,
	}).To_DialogFilter()
}

func self() *mtproto.InputPeer {
	return mtproto.MakeTLInputPeerSelf(nil).To_InputPeer()
}

func TestCheckFilterRejects(t *testing.T) {
	chatlist := folder(3, "Work")
	chatlist.PredicateName = mtproto.Predicate_dialogFilterChatlist
	empty := folder(3, "Work")
	empty.Contacts = false
	tooMany := folder(3, "Work")
	for range filterChatsLimit + 1 {
		tooMany.IncludePeers = append(tooMany.IncludePeers, self())
	}
	channel := folder(3, "Work")
	channel.IncludePeers = []*mtproto.InputPeer{mtproto.MakeTLInputPeerChannel(&mtproto.InputPeer{ChannelId: 5}).To_InputPeer()}

	for _, tc := range []struct {
		name   string
		filter *mtproto.DialogFilter
		want   error
	}{
		{"a shared folder", chatlist, mtproto.ErrFilterNotSupported},
		{"All chats", mtproto.MakeTLDialogFilterDefault(nil).To_DialogFilter(), mtproto.ErrFilterIdInvalid},
		{"another id", folder(4, "Work"), mtproto.ErrFilterIdInvalid},
		{"an empty title", folder(3, "  "), mtproto.ErrFilterTitleEmpty},
		{"no chats", empty, mtproto.ErrFilterIncludeEmpty},
		{"too many chats", tooMany, errFilterChatsTooMuch},
		{"a channel", channel, mtproto.ErrPeerIdInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := testCore().checkFilter(3, tc.filter); !errors.Is(err, tc.want) {
				t.Fatalf("error %v, want %v", err, tc.want)
			}
		})
	}
}

func TestCheckFilterKeeps(t *testing.T) {
	in := folder(3, "Work")
	in.PinnedPeers = []*mtproto.InputPeer{self()}
	in.HasMyInvites = true
	out, err := testCore().checkFilter(3, in)
	if err != nil {
		t.Fatal(err)
	}
	if out == in {
		t.Fatal("the request's folder is stored itself, not a copy")
	}
	if out.GetTitle_TEXTWITHENTITIES().GetText() != "Work" || !out.Contacts || len(out.PinnedPeers) != 1 || out.HasMyInvites {
		t.Fatalf("stored %v", out)
	}
}

func TestCutTitle(t *testing.T) {
	// A custom emoji in the first characters stays; one past the cut goes.
	kept := mtproto.MakeTLMessageEntityCustomEmoji(&mtproto.MessageEntity{Offset: 0, Length: 2, DocumentId: 1}).To_MessageEntity()
	lost := mtproto.MakeTLMessageEntityCustomEmoji(&mtproto.MessageEntity{Offset: 12, Length: 2, DocumentId: 2}).To_MessageEntity()
	long := title("😀 Работа и проекты", kept, lost)
	cut := cutTitle(long, filterTitleMax)
	if got := []rune(cut.GetText()); len(got) != filterTitleMax || string(got) != "😀 Работа и п" {
		t.Fatalf("cut title %q", cut.GetText())
	}
	if len(cut.GetEntities()) != 1 || cut.GetEntities()[0].GetDocumentId() != 1 {
		t.Fatalf("entities after the cut: %v", cut.GetEntities())
	}
	short := title("Work")
	if cutTitle(short, filterTitleMax) != short {
		t.Fatal("a short title is copied")
	}
}
