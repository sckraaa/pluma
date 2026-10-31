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
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/configuration/internal/dao"
)

// HelpDismissSuggestion
// help.dismissSuggestion#f50dbaa1 peer:InputPeer suggestion:string = Bool;
//
// A suggestion of the account goes with inputPeerEmpty and is listed in
// dismissed_suggestions of help.getPromoData from then on; the suggestions
// of a channel go with the channel, and channels are not served yet.
func (c *ConfigurationCore) HelpDismissSuggestion(in *mtproto.TLHelpDismissSuggestion) (*mtproto.Bool, error) {
	switch in.GetPeer().GetPredicateName() {
	case mtproto.Predicate_inputPeerEmpty:
	case mtproto.Predicate_inputPeerChannel, mtproto.Predicate_inputPeerChannelFromMessage:
		c.Logger.Errorf("help.dismissSuggestion blocked, License key from https://teamgram.net required to unlock enterprise features.")
		return nil, mtproto.ErrEnterpriseIsBlocked
	default:
		err := mtproto.ErrPeerIdInvalid
		c.Logger.Errorf("help.dismissSuggestion - error: %v", err)
		return nil, err
	}
	if !dao.ValidSuggestion(in.GetSuggestion()) {
		err := mtproto.ErrInputRequestInvalid
		c.Logger.Errorf("help.dismissSuggestion - error: %v", err)
		return nil, err
	}
	if err := c.svcCtx.Dao.DismissSuggestion(c.ctx, c.MD.UserId, in.GetSuggestion(), time.Now()); err != nil {
		c.Logger.Errorf("help.dismissSuggestion - error: %v", err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
