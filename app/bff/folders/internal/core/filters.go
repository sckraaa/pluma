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
	"strings"
	"unicode/utf16"

	"github.com/teamgram/proto/mtproto"
	sync "github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"

	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// The folders of chats as Telegram serves them (task 013), with or without
// the Premium the admin grants (ADR 0016, task 014). The official clients
// enforce the same limits from the app config before they ask; the checks
// stand for the server against other clients. The limits that depend on
// Premium are checked where the folder is stored, in the user's box
// transaction (dialog service).
const (
	// filterIdMin and filterIdMax bound the ids a client gives its folders:
	// 0 is "All chats" (dialogFilterDefault) and 1 the archive.
	filterIdMin = 2
	filterIdMax = 255
	// filterTitleMax is the longest folder title in characters (dialogFilter:
	// "max 12 UTF-8 chars"); the clients cut a title there.
	filterTitleMax = 12
	// filterChatsMax bounds the chats of a folder before their peers are
	// looked up: the limit with Premium; the limit of the user is the
	// dialog service's to check.
	filterChatsMax = 200
)

// errFilterChatsTooMuch: more chats in a folder than any user may have.
var errFilterChatsTooMuch = status.Error(mtproto.ErrBadRequest, "FILTER_INCLUDE_TOO_MUCH")

// premium returns whether the user has Premium now.
func (c *FoldersCore) premium() (bool, error) {
	me, err := c.svcCtx.Dao.UserClient.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{Id: c.MD.UserId})
	if err != nil {
		return false, err
	}
	return me.Premium(), nil
}

// dialogFilters returns the user's folders as the clients expect them, and
// whether folder tags are on. "All chats" (dialogFilterDefault, id 0) is
// first, unless the user has Premium and put it elsewhere; the Android
// client reads its place as the place of the main chat list. Tags are a
// Premium feature: they are on only while the user has Premium.
func (c *FoldersCore) dialogFilters() ([]*mtproto.DialogFilter, bool, error) {
	premium, err := c.premium()
	if err != nil {
		return nil, false, err
	}
	stored, err := c.svcCtx.Dao.DialogClient.DialogGetDialogFilters(c.ctx, &dialog.TLDialogGetDialogFilters{
		UserId: c.MD.UserId,
	})
	if err != nil {
		return nil, false, err
	}
	filters := withAllChats(stored.GetDatas(), premium)
	if !premium {
		return filters, false, nil
	}
	tags, err := c.svcCtx.Dao.DialogClient.DialogGetDialogFilterTags(c.ctx, &dialog.TLDialogGetDialogFilterTags{
		UserId: c.MD.UserId,
	})
	if err != nil {
		return nil, false, err
	}
	return filters, mtproto.FromBool(tags), nil
}

// withAllChats returns the stored folders, in their order, with "All
// chats": at the place of its row (id 0) for a user with Premium, first
// otherwise.
func withAllChats(stored []*dialog.DialogFilterExt, premium bool) []*mtproto.DialogFilter {
	allChats := mtproto.MakeTLDialogFilterDefault(nil).To_DialogFilter()
	filters := make([]*mtproto.DialogFilter, 0, len(stored)+1)
	placed := false
	for _, ext := range stored {
		f := ext.GetDialogFilter()
		switch {
		case f == nil:
		case ext.GetId() == 0:
			if premium && !placed {
				filters = append(filters, allChats)
				placed = true
			}
		default:
			filters = append(filters, f)
		}
	}
	if !placed {
		filters = append([]*mtproto.DialogFilter{allChats}, filters...)
	}
	return filters
}

// checkFilter returns filter as it is stored for folder id: a copy with the
// id set and the title cut to filterTitleMax, or the error Telegram
// answers.
func (c *FoldersCore) checkFilter(id int32, filter *mtproto.DialogFilter) (*mtproto.DialogFilter, error) {
	switch filter.GetPredicateName() {
	case mtproto.Predicate_dialogFilter:
	case mtproto.Predicate_dialogFilterChatlist:
		// Shared folders (chatlists.*) are not served.
		return nil, mtproto.ErrFilterNotSupported
	default:
		return nil, mtproto.ErrFilterIdInvalid
	}
	if filter.GetId() != id {
		return nil, mtproto.ErrFilterIdInvalid
	}
	f := proto.Clone(filter).(*mtproto.DialogFilter)
	f.HasMyInvites = false

	title := f.GetTitle_TEXTWITHENTITIES()
	if title == nil || strings.TrimSpace(title.GetText()) == "" {
		return nil, mtproto.ErrFilterTitleEmpty
	}
	f.Title_TEXTWITHENTITIES = cutTitle(title, filterTitleMax)

	if !f.Contacts && !f.NonContacts && !f.Groups && !f.Broadcasts && !f.Bots &&
		len(f.GetIncludePeers()) == 0 && len(f.GetPinnedPeers()) == 0 {
		return nil, mtproto.ErrFilterIncludeEmpty
	}
	if len(f.GetIncludePeers())+len(f.GetPinnedPeers()) > filterChatsMax || len(f.GetExcludePeers()) > filterChatsMax {
		return nil, errFilterChatsTooMuch
	}
	if err := c.checkPeers(f.GetPinnedPeers(), f.GetIncludePeers(), f.GetExcludePeers()); err != nil {
		return nil, err
	}
	return f, nil
}

// cutTitle returns title cut to max characters, without the entities that
// do not fit in the rest (their offsets are in UTF-16 code units).
func cutTitle(title *mtproto.TextWithEntities, max int) *mtproto.TextWithEntities {
	runes := []rune(title.GetText())
	if len(runes) <= max {
		return title
	}
	text := string(runes[:max])
	units := int32(len(utf16.Encode([]rune(text))))
	cut := mtproto.MakeTLTextWithEntities(&mtproto.TextWithEntities{Text: text}).To_TextWithEntities()
	for _, e := range title.GetEntities() {
		if e.GetOffset()+e.GetLength() <= units {
			cut.Entities = append(cut.Entities, e)
		}
	}
	return cut
}

// checkPeers answers PEER_ID_INVALID for a peer of a folder the user cannot
// have a chat with: not a user, a basic group or Saved Messages (Pluma has
// no channels), a user that does not exist or a group the user never was
// in.
func (c *FoldersCore) checkPeers(lists ...[]*mtproto.InputPeer) error {
	var users, chats []int64
	seenUser, seenChat := map[int64]bool{}, map[int64]bool{}
	for _, list := range lists {
		for _, in := range list {
			p := mtproto.FromInputPeer2(c.MD.UserId, in)
			switch {
			case p.IsSelf() || (p.PeerType == mtproto.PEER_USER && p.PeerId == c.MD.UserId):
			case p.PeerType == mtproto.PEER_USER:
				if !seenUser[p.PeerId] {
					seenUser[p.PeerId] = true
					users = append(users, p.PeerId)
				}
			case p.IsChat():
				if !seenChat[p.PeerId] {
					seenChat[p.PeerId] = true
					chats = append(chats, p.PeerId)
				}
			default:
				return mtproto.ErrPeerIdInvalid
			}
		}
	}
	if len(users) > 0 {
		found, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: users})
		if err != nil {
			return err
		}
		for _, id := range users {
			if _, ok := found.GetImmutableUser(id); !ok {
				return mtproto.ErrPeerIdInvalid
			}
		}
	}
	if len(chats) > 0 {
		found, err := c.svcCtx.Dao.ChatClient.ChatGetChatListByIdList(c.ctx, &chatpb.TLChatGetChatListByIdList{
			SelfId: c.MD.UserId,
			IdList: chats,
		})
		if err != nil {
			return err
		}
		member := make(map[int64]bool, len(chats))
		found.Walk(func(_ int, chat *mtproto.MutableChat) {
			if _, ok := chat.GetImmutableChatParticipant(c.MD.UserId); ok {
				member[chat.Id()] = true
			}
		})
		for _, id := range chats {
			if !member[id] {
				return mtproto.ErrPeerIdInvalid
			}
		}
	}
	return nil
}

// pushToOtherDevices tells the user's other devices about a change of the
// folders, as Telegram does. The change is stored: a push that fails does
// not fail the request, the devices see the folders on their next
// messages.getDialogFilters.
func (c *FoldersCore) pushToOtherDevices(update *mtproto.Update) {
	if _, err := c.svcCtx.Dao.SyncClient.SyncUpdatesNotMe(c.ctx, &sync.TLSyncUpdatesNotMe{
		UserId:        c.MD.UserId,
		PermAuthKeyId: c.MD.PermAuthKeyId,
		Updates:       mtproto.MakeUpdatesByUpdates(update),
	}); err != nil {
		c.Logger.Errorf("folders: push to the other devices of user %d: %v", c.MD.UserId, err)
	}
}
