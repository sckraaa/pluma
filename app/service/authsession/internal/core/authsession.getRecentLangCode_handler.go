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
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
)

// AuthsessionGetRecentLangCode
// authsession.getRecentLangCode user_id:long except_auth_key_id:long = String;
//
// The lang_code of initConnection of the user's session that connected
// last, apart from the permanent key except_auth_key_id (task 019): the
// language of a service message that is stored for the account.
func (c *AuthsessionCore) AuthsessionGetRecentLangCode(in *authsession.TLAuthsessionGetRecentLangCode) (*mtproto.String, error) {
	langCode, err := c.svcCtx.Dao.RecentLangCode(c.ctx, in.GetUserId(), in.GetExceptAuthKeyId())
	if err != nil {
		c.Logger.Errorf("authsession.getRecentLangCode - user %d: %v", in.GetUserId(), err)
		return nil, err
	}
	return mtproto.MakeTLString(&mtproto.String{V: langCode}).To_String(), nil
}
