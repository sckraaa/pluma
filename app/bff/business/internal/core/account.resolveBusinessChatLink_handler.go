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

	"github.com/teamgram/proto/mtproto"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"

	"github.com/zeromicro/go-zero/core/metric"
	"google.golang.org/grpc/status"
)

// chatLinkResolves counts account.resolveBusinessChatLink by result: ok,
// empty (CHATLINK_SLUG_EMPTY), expired (CHATLINK_SLUG_EXPIRED) or error.
var chatLinkResolves = metric.NewCounterVec(&metric.CounterVecOpts{
	Namespace: "business",
	Subsystem: "chatlink",
	Name:      "resolve_total",
	Help:      "Business chat links opened by account.resolveBusinessChatLink, by result.",
	Labels:    []string{"result"},
})

// resolveResult is the label of chatLinkResolves for the error of a call.
func resolveResult(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, mtproto.ErrChatlinkSlugEmpty):
		return "empty"
	case errors.Is(err, mtproto.ErrChatlinkSlugExpired):
		return "expired"
	}
	return "error"
}

// AccountResolveBusinessChatLink
// account.resolveBusinessChatLink#5492e5ee slug:string = account.ResolvedBusinessChatLinks;
//
// Opens a link: the chat with its owner and the text the client puts in
// the input field (core.telegram.org/api/links#business-chat-links;
// Android ui/LaunchActivity.java:5564-5583 opens the chat of peer with
// the text). An empty slug is CHATLINK_SLUG_EMPTY; a slug of no link, of
// a removed one or of an account that is no more is CHATLINK_SLUG_EXPIRED
// (core.telegram.org/method/account.resolveBusinessChatLink). Anybody may
// open a link, Premium or not. Opening counts a view, unless the owner
// opens it ("views are counted when resolving the link",
// core.telegram.org/api/business#business-chat-links). users holds the
// owner and the users the mentions name, as the caller sees them.
func (c *BusinessCore) AccountResolveBusinessChatLink(in *mtproto.TLAccountResolveBusinessChatLink) (*mtproto.Account_ResolvedBusinessChatLinks, error) {
	r, err := c.resolveChatLink(in)
	chatLinkResolves.Inc(resolveResult(err))
	return r, err
}

func (c *BusinessCore) resolveChatLink(in *mtproto.TLAccountResolveBusinessChatLink) (*mtproto.Account_ResolvedBusinessChatLinks, error) {
	const method = "account.resolveBusinessChatLink"
	slug, err := c.slugArg(method, in.GetSlug())
	if err != nil {
		return nil, err
	}
	link, err := c.svcCtx.Dao.Store.ChatLink(c.ctx, slug)
	if err != nil {
		c.Logger.Errorf("%s - error: %v", method, err)
		return nil, err
	}
	if link == nil {
		err = mtproto.ErrChatlinkSlugExpired
		c.Logger.Errorf("%s - error: %v", method, err)
		return nil, err
	}
	owner, err := c.svcCtx.Dao.UserClient.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{Id: link.UserID})
	if st, ok := status.FromError(err); err != nil && (!ok || st.Code() != mtproto.ErrBadRequest) {
		c.Logger.Errorf("%s - the owner %d: %v", method, link.UserID, err)
		return nil, err
	}
	if err != nil || owner == nil || owner.Deleted() {
		// An account removed since: its links went with it for the
		// clients, whatever rows are left.
		err = mtproto.ErrChatlinkSlugExpired
		c.Logger.Errorf("%s - the owner %d: %v", method, link.UserID, err)
		return nil, err
	}
	ids := append([]int64{link.UserID}, slices.DeleteFunc(mentionedUsers(link.Entities), func(id int64) bool { return id == link.UserID })...)
	users, err := c.usersFor(method, ids)
	if err != nil {
		return nil, err
	}
	if err := c.svcCtx.Dao.Store.CountChatLinkView(c.ctx, slug, c.MD.UserId); err != nil {
		c.Logger.Errorf("%s - the view: %v", method, err)
		return nil, err
	}
	return mtproto.MakeTLAccountResolvedBusinessChatLinks(&mtproto.Account_ResolvedBusinessChatLinks{
		Peer:     mtproto.MakePeerUser(link.UserID),
		Message:  link.Message,
		Entities: link.Entities,
		Chats:    []*mtproto.Chat{},
		Users:    users,
	}).To_Account_ResolvedBusinessChatLinks(), nil
}
