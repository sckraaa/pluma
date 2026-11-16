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
	"github.com/teamgram/proto/mtproto"
)

// AccountDeleteBusinessChatLink
// account.deleteBusinessChatLink#60073674 slug:string = Bool;
//
// Removes a link of the account: from then on it resolves to
// CHATLINK_SLUG_EXPIRED. The clients name the link by the whole link
// (business.SlugOf). A slug of another account, or of no link, is
// CHATLINK_SLUG_EXPIRED.
func (c *BusinessCore) AccountDeleteBusinessChatLink(in *mtproto.TLAccountDeleteBusinessChatLink) (*mtproto.Bool, error) {
	const method = "account.deleteBusinessChatLink"
	if err := c.notBot(method); err != nil {
		return nil, err
	}
	slug, err := c.slugArg(method, in.GetSlug())
	if err != nil {
		return nil, err
	}
	if err := c.svcCtx.Dao.Store.DeleteChatLink(c.ctx, c.MD.UserId, slug); err != nil {
		err = chatLinkError(err)
		c.Logger.Errorf("%s - error: %v", method, err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
