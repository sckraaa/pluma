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
	"slices"
	"unicode/utf8"

	"github.com/teamgram/proto/mtproto"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/business"
	"github.com/teamgram/teamgram-server/pkg/env2"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// The business chat links (task 105, part 2; ADR 0067, item 5): a link
// https://pluma.chat/m/<slug> that opens the chat with its owner with a
// text in the input field.

// chatLinkURL is the link of slug the clients show and share: the link
// host (ADR 0036), the path of core.telegram.org/api/links
// ("t.me/m/<slug>").
func chatLinkURL(slug string) string {
	return "https://" + env2.TDotMe + "/m/" + slug
}

// chatLinkTL is l as businessChatLink.
func chatLinkTL(l business.ChatLink) *mtproto.BusinessChatLink {
	out := &mtproto.BusinessChatLink{
		Link:     chatLinkURL(l.Slug),
		Message:  l.Message,
		Entities: l.Entities,
		Views:    l.Views,
	}
	if l.Title != "" {
		out.Title = wrapperspb.String(l.Title)
	}
	return mtproto.MakeTLBusinessChatLink(out).To_BusinessChatLink()
}

// chatLinkError is err of the store as the error of the methods.
func chatLinkError(err error) error {
	switch {
	case errors.Is(err, business.ErrChatLinkNotFound):
		return mtproto.ErrChatlinkSlugExpired
	case errors.Is(err, business.ErrChatLinksTooMuch):
		return mtproto.ErrChatlinksTooMuch
	}
	return err
}

// slugArg is the slug a client names a link by (business.SlugOf: the
// clients send the whole link); empty is CHATLINK_SLUG_EMPTY, one that no
// link could have is CHATLINK_SLUG_EXPIRED, as an unknown one
// (core.telegram.org/method/account.resolveBusinessChatLink).
func (c *BusinessCore) slugArg(method, arg string) (string, error) {
	slug := business.SlugOf(arg)
	if slug == "" {
		err := mtproto.ErrChatlinkSlugEmpty
		c.Logger.Errorf("%s - error: %v", method, err)
		return "", err
	}
	if !business.ValidSlug(slug) {
		err := mtproto.ErrChatlinkSlugExpired
		c.Logger.Errorf("%s - error: %v", method, err)
		return "", err
	}
	return slug, nil
}

// chatLinkOf checks in, the link the caller creates or edits, and returns
// it as it is kept:
//   - the text becomes a message, so it is held to message_length_max
//     (4096 UTF-16 code units, as the clients count it): MESSAGE_TOO_LONG.
//     An empty text is a link: Android creates a new link with an empty
//     text and lets the owner write it then
//     (ui/Business/BusinessLinksController.java:170-186);
//   - the title is held to 32 UTF-16 code units, the limit of the clients'
//     editors (Android ui/Business/BusinessLinksActivity.java:118, iOS
//     BusinessLinkNameAlertController.swift:48): TITLE_INVALID (Telegram
//     publishes neither the limit nor an error; the name is this
//     server's);
//   - the entities are those of the text in the input field, kept as a
//     draft keeps them (messages.saveDraft): a mention typed as
//     inputMessageEntityMentionName becomes a mention by id, of a user
//     that exists (else it is dropped, as a message would not carry it),
//     and an entity out of the text is dropped. More entities than units
//     of text is ENTITIES_TOO_LONG.
func (c *BusinessCore) chatLinkOf(method string, in *mtproto.InputBusinessChatLink) (business.ChatLink, error) {
	text, title := in.GetMessage(), in.GetTitle().GetValue()
	if !utf8.ValidString(text) || business.UTF16Length(text) > business.ChatLinkMessageLengthMax {
		err := mtproto.ErrMessageTooLong
		c.Logger.Errorf("%s - error: %v", method, err)
		return business.ChatLink{}, err
	}
	if !utf8.ValidString(title) || business.UTF16Length(title) > business.ChatLinkTitleLengthMax {
		err := mtproto.ErrTitleInvalid
		c.Logger.Errorf("%s - error: %v", method, err)
		return business.ChatLink{}, err
	}
	units := business.UTF16Length(text)
	if len(in.GetEntities()) > units {
		err := mtproto.ErrEntitiesTooLong
		c.Logger.Errorf("%s - %d entities: %v", method, len(in.GetEntities()), err)
		return business.ChatLink{}, err
	}
	entities := make([]*mtproto.MessageEntity, 0, len(in.GetEntities()))
	for _, e := range in.GetEntities() {
		if e.GetOffset() < 0 || e.GetLength() <= 0 || int(e.GetOffset())+int(e.GetLength()) > units {
			continue
		}
		if e.GetPredicateName() == mtproto.Predicate_inputMessageEntityMentionName {
			id := e.GetUserId_INPUTUSER().GetUserId()
			if e.GetUserId_INPUTUSER().GetPredicateName() == mtproto.Predicate_inputUserSelf {
				id = c.MD.UserId
			}
			if id == 0 {
				continue
			}
			e = mtproto.MakeTLMessageEntityMentionName(&mtproto.MessageEntity{
				Offset:       e.GetOffset(),
				Length:       e.GetLength(),
				UserId_INT64: id,
			}).To_MessageEntity()
		}
		entities = append(entities, e)
	}
	if ids := mentionedUsers(entities); len(ids) > 0 {
		users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{Id: ids})
		if err != nil {
			c.Logger.Errorf("%s - users: %v", method, err)
			return business.ChatLink{}, err
		}
		entities = slices.DeleteFunc(entities, func(e *mtproto.MessageEntity) bool {
			if e.GetPredicateName() != mtproto.Predicate_messageEntityMentionName {
				return false
			}
			u, ok := users.GetImmutableUser(e.GetUserId_INT64())
			return !ok || u.Deleted()
		})
	}
	if len(entities) == 0 {
		entities = nil
	}
	return business.ChatLink{UserID: c.MD.UserId, Message: text, Entities: entities, Title: title}, nil
}

// mentionedUsers are the users the mentions by id of entities name.
func mentionedUsers(entities []*mtproto.MessageEntity) []int64 {
	var ids []int64
	for _, e := range entities {
		if e.GetPredicateName() == mtproto.Predicate_messageEntityMentionName && !slices.Contains(ids, e.GetUserId_INT64()) {
			ids = append(ids, e.GetUserId_INT64())
		}
	}
	return ids
}

// usersFor are the users of ids as the caller sees them, its privacy
// applied; a user that is no more is left out.
func (c *BusinessCore) usersFor(method string, ids []int64) ([]*mtproto.User, error) {
	if len(ids) == 0 {
		return []*mtproto.User{}, nil
	}
	all := ids
	if !slices.Contains(all, c.MD.UserId) {
		all = append(slices.Clone(ids), c.MD.UserId)
	}
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{
		Id:      all,
		Privacy: true,
		HasTo:   true,
		To:      []int64{c.MD.UserId},
	})
	if err != nil {
		c.Logger.Errorf("%s - users: %v", method, err)
		return nil, err
	}
	return users.GetUserListByIdList(c.MD.UserId, ids...), nil
}
