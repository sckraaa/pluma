// Copyright 2022 Teamgram Authors
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
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package core

import (
	"strings"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// UserUpdateBotData
// user.updateBotData flags:# user_id:long bot_chat_history:flags.15?Bool bot_nochats:flags.16?Bool bot_inline_geo:flags.21?Bool bot_attach_menu:flags.27?Bool bot_inline_placeholder:flags.19?string = Bool;
//
// A call with no field only raises bot_info_version, which tells clients to
// fetch the bot's info again (core.telegram.org/api/bots/commands). The
// cached user carries the flags (user.bot_chat_history and the like), so it
// is dropped.
func (c *UserCore) UserUpdateBotData(in *user.TLUserUpdateBotData) (*mtproto.Bool, error) {
	var (
		sets = []string{"bot_info_version = bot_info_version + 1"}
		args []interface{}
	)
	flag := func(column string, v *mtproto.Bool) {
		if v != nil {
			sets = append(sets, column+" = ?")
			args = append(args, mtproto.FromBool(v))
		}
	}
	flag("bot_chat_history", in.GetBotChatHistory())
	flag("bot_nochats", in.GetBotNochats())
	flag("bot_inline_geo", in.GetBotInlineGeo())
	flag("bot_attach_menu", in.GetBotAttachMenu())
	flag("bot_has_main_app", in.GetBotHasMainApp())
	if p := in.GetBotInlinePlaceholder(); p != nil {
		sets = append(sets, "bot_inline_placeholder = ?")
		args = append(args, p.GetValue())
	}
	r, err := c.svcCtx.Dao.DB.Exec(c.ctx, "UPDATE bots SET "+strings.Join(sets, ", ")+" WHERE bot_id = ?", append(args, in.GetBotId())...)
	if err != nil {
		c.Logger.Errorf("user.updateBotData - error: %v", err)
		return nil, err
	}
	if n, err := r.RowsAffected(); err != nil {
		return nil, err
	} else if n == 0 {
		return nil, mtproto.ErrBotInvalid
	}
	if err = c.svcCtx.Dao.DropUserCache(c.ctx, in.GetBotId()); err != nil {
		c.Logger.Errorf("user.updateBotData - cache of %d: %v", in.GetBotId(), err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
