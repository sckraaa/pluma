/*
 * Created from 'scheme.tl' by 'mtprotoc'
 *
 * Copyright (c) 2021-present,  Teamgram Studio (https://teamgram.io).
 *  All rights reserved.
 *
 * Author: teamgramio (teamgram.io@gmail.com)
 */

package core

import (
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
)

/*
# auth.bindTempAuthKey

	Binds a temporary authorization key temp_auth_key_id to the permanent
	authorization key perm_auth_key_id
	(https://core.telegram.org/method/auth.bindTempAuthKey,
	https://core.telegram.org/api/pfs).

## Possible errors
| Code | Type | Description |
| ---- | ---- | ----------- |
| 400 | ENCRYPTED_MESSAGE_INVALID | Encrypted message is incorrect |
| 400 | EXPIRES_AT_INVALID | The specified expires_at timestamp is invalid |
| 400 | TEMP_AUTH_KEY_ALREADY_BOUND | The passed temporary key is already bound to another perm_auth_key_id |
| 400 | TEMP_AUTH_KEY_EMPTY | The request was not performed with a temporary authorization key |

Official clients (Android ConnectionsManager/Handshake, tdesktop
DcKeyBinder, iOS MTBindKeyMessageService) send the request alone over the new
temporary key with RequestFlagUseUnboundKey semantics and retry or recreate
the key on error.
*/

// AuthsessionBindTempAuthKey
// authsession.bindTempAuthKey perm_auth_key_id:long nonce:long expires_at:int encrypted_message:bytes = Bool;
//
// The session forwards the request with the RPC metadata of the connection it
// arrived on: AuthId is the temporary key, SessionId and ClientMsgId are the
// session and msg_id of the request. The binding message is checked against
// them; see bindTempAuthKey.
func (c *AuthsessionCore) AuthsessionBindTempAuthKey(in *authsession.TLAuthsessionBindTempAuthKey) (*mtproto.Bool, error) {
	if c.MD == nil {
		err := mtproto.ErrTempAuthKeyEmpty
		c.Logger.Errorf("authsession.bindTempAuthKey - error: %v (no rpc metadata)", err)
		return nil, err
	}

	err := bindTempAuthKey(
		c.ctx,
		c.svcCtx.Dao,
		&bindTempAuthKeyRequest{
			TempAuthKeyId:    c.MD.GetAuthId(),
			TempSessionId:    c.MD.GetSessionId(),
			MsgId:            c.MD.GetClientMsgId(),
			PermAuthKeyId:    in.GetPermAuthKeyId(),
			Nonce:            in.GetNonce(),
			ExpiresAt:        in.GetExpiresAt(),
			EncryptedMessage: in.GetEncryptedMessage(),
		},
		time.Now().Unix())
	if err != nil {
		c.Logger.Errorf("authsession.bindTempAuthKey - error: %v (temp_auth_key_id: %d, perm_auth_key_id: %d)",
			err,
			c.MD.GetAuthId(),
			in.GetPermAuthKeyId())
		return nil, err
	}

	c.Logger.Infof("authsession.bindTempAuthKey - bound temp_auth_key_id %d to perm_auth_key_id %d",
		c.MD.GetAuthId(),
		in.GetPermAuthKeyId())

	return mtproto.BoolTrue, nil
}
