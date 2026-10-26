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
	"github.com/teamgram/proto/mtproto"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// AccountSetPrivacy
// account.setPrivacy#c9f81ce8 key:InputPrivacyKey rules:Vector<InputPrivacyRule> = account.PrivacyRules;
func (c *PrivacySettingsCore) AccountSetPrivacy(in *mtproto.TLAccountSetPrivacy) (*mtproto.Account_PrivacyRules, error) {
	var (
		key = mtproto.FromInputPrivacyKeyType(in.Key)
	)

	// TODO(@benqi): Check request valid.
	if key == mtproto.KEY_TYPE_INVALID {
		err := mtproto.ErrPrivacyKeyInvalid
		c.Logger.Errorf("account.setPrivacy - error: %v", err)
		return nil, err
	}

	ruleList := mtproto.ToPrivacyRuleListByInput(c.MD.UserId, in.Rules)
	rValue := mtproto.MakeTLAccountPrivacyRules(&mtproto.Account_PrivacyRules{
		Rules: ruleList,
		Users: []*mtproto.User{},
		Chats: []*mtproto.Chat{},
	}).To_Account_PrivacyRules()

	idHelper := mtproto.NewIDListHelper(c.MD.UserId)
	idHelper.PickByRules(ruleList)
	idHelper.Visit(
		func(userIdList []int64) {
			users, _ := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx,
				&userpb.TLUserGetMutableUsers{
					Id: userIdList,
				})
			rValue.Users = users.GetUserListByIdList(c.MD.UserId, userIdList...)
		},
		func(chatIdList []int64) {
			chats, _ := c.svcCtx.Dao.ChatClient.ChatGetChatListByIdList(c.ctx,
				&chatpb.TLChatGetChatListByIdList{
					IdList: chatIdList,
				})
			rValue.Chats = chats.GetChatListByIdList(c.MD.UserId, chatIdList...)
		},
		func(channelIdList []int64) {
			// TODO
		})

	// biz pushes updatePrivacy with seq to every device of the account,
	// this one included, in the transaction of the change (ADR 0020); the
	// users and chats the rules name go with it.
	if _, err := c.svcCtx.Dao.UserClient.UserSetPrivacy(c.ctx, &userpb.TLUserSetPrivacy{
		UserId:  c.MD.UserId,
		KeyType: int32(key),
		Rules:   ruleList,
		Users:   rValue.Users,
		Chats:   rValue.Chats,
	}); err != nil {
		c.Logger.Errorf("account.setPrivacy - error: %v", err)
		return nil, err
	}

	return rValue, nil
}
