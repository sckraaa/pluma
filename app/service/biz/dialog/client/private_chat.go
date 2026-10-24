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

package dialogclient

import (
	"context"
	"errors"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
)

// HasPrivateChat tells whether userId has a private chat with peerId: a
// dialog of userId with the user (task 014, pkg/premium).
func HasPrivateChat(ctx context.Context, c DialogClient, userId, peerId int64) (bool, error) {
	_, err := c.DialogGetDialogById(ctx, &dialog.TLDialogGetDialogById{
		UserId:   userId,
		PeerType: mtproto.PEER_USER,
		PeerId:   peerId,
	})
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, mtproto.ErrPeerIdInvalid):
		return false, nil
	default:
		return false, err
	}
}
