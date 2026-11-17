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
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// AccountReorderUsernames
// account.reorderUsernames#ef500eab order:Vector<string> = Bool;
//
// Orders the shown usernames of the account; order lists every one of them
// once ("all active usernames must be specified",
// core.telegram.org/method/account.reorderUsernames): ORDER_INVALID
// otherwise, USERNAME_NOT_MODIFIED for the order they have, which is the
// answer to the one name every account has here. tdesktop ignores the
// error (api_user_names.cpp:227-229).
func (c *UsernamesCore) AccountReorderUsernames(in *mtproto.TLAccountReorderUsernames) (*mtproto.Bool, error) {
	return c.editAccountUsernames("account.reorderUsernames", &userpb.TLUserEditUsernames{
		Action: userpb.UsernamesReorder,
		Active: mtproto.BoolFalse,
		Order:  in.GetOrder(),
	})
}
