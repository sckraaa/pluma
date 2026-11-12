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

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/ringtones"
	"github.com/teamgram/teamgram-server/pkg/ringtones/saved"
)

// AccountSaveRingtone
// account.saveRingtone#3dea5b03 id:InputDocument unsave:Bool = account.SavedRingtone;
//
// Adds a sound to the notification sounds of the account or, with unsave,
// removes it. The other devices of the account are told to load the list
// again (updateSavedRingtones).
//
// A sound to add is an MP3 document within the limits: one uploaded with
// account.uploadRingtone, which is always MP3, or an audio file of a chat
// (Android "Save for notifications", MediaDataController.java:8794-8828).
// It is kept as it is: account.savedRingtone. A sound in another format
// would have to be converted (account.savedRingtoneConverted,
// core.telegram.org/method/account.saveRingtone); that is done only for
// uploaded files, so such a document is RINGTONE_INVALID.
func (c *CatalogCore) AccountSaveRingtone(in *mtproto.TLAccountSaveRingtone) (*mtproto.Account_SavedRingtone, error) {
	if mtproto.FromBool(in.GetUnsave()) {
		if in.GetId().GetPredicateName() != mtproto.Predicate_inputDocument || in.GetId().GetId() == 0 {
			err := ringtones.ErrInvalid
			c.Logger.Errorf("account.saveRingtone - error: %v", err)
			return nil, err
		}
		if err := c.svcCtx.Ringtones.Unsave(c.ctx, c.MD.UserId, in.GetId().GetId()); err != nil {
			c.Logger.Errorf("account.saveRingtone - error: %v", err)
			return nil, err
		}
		return mtproto.MakeTLAccountSavedRingtone(nil).To_Account_SavedRingtone(), nil
	}
	d, err := c.ringtoneDocument(in.GetId())
	if err != nil {
		c.Logger.Errorf("account.saveRingtone - error: %v", err)
		return nil, err
	}
	if err = c.svcCtx.Ringtones.Save(c.ctx, c.MD.UserId, d.GetId()); err != nil {
		c.Logger.Errorf("account.saveRingtone - error: %v", err)
		if errors.Is(err, saved.ErrTooMany) {
			err = ringtones.ErrInvalid
		}
		return nil, err
	}
	return mtproto.MakeTLAccountSavedRingtone(nil).To_Account_SavedRingtone(), nil
}
