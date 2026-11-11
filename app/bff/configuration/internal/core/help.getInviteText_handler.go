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
	"github.com/teamgram/teamgram-server/pkg/env2"
)

// HelpGetInviteText
// help.getInviteText#4d392343 = help.InviteText;
//
// What a user sends to invite a friend. Only Android asks, and it takes
// the message as the link of its own invitation: it keeps it as
// "invitelink" and puts it in place of the link of the strings InviteText2
// and InviteTextNum of its language pack, which already say the rest in
// the user's language (ContactsController.java:351-385; without an answer
// the link is telegram.org/dl). So the message is the address where the
// application is downloaded, the site of the link host, and nothing else:
// a sentence here would be repeated inside the client's own sentence.
func (c *ConfigurationCore) HelpGetInviteText(in *mtproto.TLHelpGetInviteText) (*mtproto.Help_InviteText, error) {
	return mtproto.MakeTLHelpInviteText(&mtproto.Help_InviteText{
		Message: InviteLink(),
	}).To_Help_InviteText(), nil
}

// InviteLink is the address an invitation leads to.
func InviteLink() string {
	return "https://" + env2.TDotMe
}
