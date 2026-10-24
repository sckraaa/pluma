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
	"errors"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
	"github.com/teamgram/teamgram-server/app/service/authsession/internal/dao"
)

// AuthsessionSetAuthKey
// authsession.setAuthKey auth_key:AuthKeyInfo future_salt:FutureSalt = Bool;
//
// boolFalse means the auth_key_id is taken by another key, which the key
// exchange answers with dh_gen_retry; any other failure is an error (the
// key exchange answers dh_gen_fail). The first salt is stored only with the
// key, never over the salt of the key that has the id.
func (c *AuthsessionCore) AuthsessionSetAuthKey(in *authsession.TLAuthsessionSetAuthKey) (*mtproto.Bool, error) {
	keyInfo := in.GetAuthKey()

	err := c.svcCtx.Dao.SetAuthKeyV2(c.ctx, keyInfo, in.ExpiresIn)
	if errors.Is(err, dao.ErrAuthKeyIdTaken) {
		c.Logger.Infof("authsession.setAuthKey - auth_key_id %d is taken", keyInfo.GetAuthKeyId())
		return mtproto.BoolFalse, nil
	}
	if err != nil {
		c.Logger.Errorf("authsession.setAuthKey - error: %v", err)
		return nil, err
	}

	if in.FutureSalt != nil {
		if salt := in.FutureSalt.To_FutureSalt(); salt != nil {
			if err = c.svcCtx.Dao.PutSaltCache(c.ctx, keyInfo.AuthKeyId, salt); err != nil {
				c.Logger.Errorf("authsession.setAuthKey - salt: %v", err)
				return nil, err
			}
		}
	}

	return mtproto.BoolTrue, nil
}
