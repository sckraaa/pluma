package mtclient

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/crypto"
)

// bindAuthKeyInnerSize is bind_auth_key_inner#75a3f765 nonce:long
// temp_auth_key_id:long perm_auth_key_id:long temp_session_id:long
// expires_at:int.
const bindAuthKeyInnerSize = 4 + 8 + 8 + 8 + 8 + 4

// BindTempAuthKey binds the temporary key of this session to the permanent
// key perm (auth.bindTempAuthKey, https://core.telegram.org/method/auth.bindTempAuthKey)
// until expiresAt (unix time; at most the temporary key's own expiry). After
// it the session acts for the user of perm; send initConnection again.
func (c *Client) BindTempAuthKey(ctx context.Context, perm *AuthKey, expiresAt int64) error {
	if !c.key.Temp {
		return errors.New("mtclient: BindTempAuthKey needs a session with a temporary key")
	}
	if perm == nil || perm.Temp {
		return errors.New("mtclient: BindTempAuthKey needs a permanent key")
	}
	var nb [8]byte
	if _, err := rand.Read(nb[:]); err != nil {
		return err
	}
	nonce := int64(binary.LittleEndian.Uint64(nb[:]))

	encode := func(msgID int64) ([]byte, error) {
		msg, err := bindMessage(perm, msgID, nonce, c.key.ID, c.sessionID, int32(expiresAt))
		if err != nil {
			return nil, err
		}
		return encodeObject(&mtproto.TLAuthBindTempAuthKey{
			Constructor:      mtproto.CRC32_auth_bindTempAuthKey,
			PermAuthKeyId:    perm.ID,
			Nonce:            nonce,
			ExpiresAt:        int32(expiresAt),
			EncryptedMessage: msg,
		}, c.cfg.Layer)
	}
	raw, err := c.do(ctx, "auth.bindTempAuthKey", encode)
	if err != nil {
		return err
	}
	d := mtproto.NewDecodeBuf(raw)
	o := d.Object()
	if _, ok := o.(*mtproto.TLBoolTrue); !ok || d.GetError() != nil {
		return fmt.Errorf("mtclient: auth.bindTempAuthKey returned %s (%v)", typeName(o), d.GetError())
	}
	return nil
}

// bindMessage builds encrypted_message of auth.bindTempAuthKey: the binding
// message, carrying the msg_id of the request and seqno 0, encrypted with the
// permanent key as in MTProto v1.
func bindMessage(perm *AuthKey, msgID, nonce, tempKeyID, tempSessionID int64, expiresAt int32) ([]byte, error) {
	plain := make([]byte, 32+bindAuthKeyInnerSize)
	if _, err := rand.Read(plain[:16]); err != nil { // random:int128 in place of salt and session_id
		return nil, err
	}
	binary.LittleEndian.PutUint64(plain[16:], uint64(msgID))
	binary.LittleEndian.PutUint32(plain[24:], 0)
	binary.LittleEndian.PutUint32(plain[28:], bindAuthKeyInnerSize)
	inner := plain[32:]
	binary.LittleEndian.PutUint32(inner, uint32(mtproto.CRC32_bind_auth_key_inner))
	binary.LittleEndian.PutUint64(inner[4:], uint64(nonce))
	binary.LittleEndian.PutUint64(inner[12:], uint64(tempKeyID))
	binary.LittleEndian.PutUint64(inner[20:], uint64(perm.ID))
	binary.LittleEndian.PutUint64(inner[28:], uint64(tempSessionID))
	binary.LittleEndian.PutUint32(inner[36:], uint32(expiresAt))

	msgKey, encrypted, err := crypto.NewClientAuthKey(perm.ID, perm.Key).AesIgeEncryptV1(plain)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, 24+len(encrypted))
	out = binary.LittleEndian.AppendUint64(out, uint64(perm.ID))
	out = append(out, msgKey...)
	return append(out, encrypted...), nil
}

// TempKeyExpiresAt returns the expires_at to bind a temporary key with: its
// own expiry, or now + d if that is earlier.
func TempKeyExpiresAt(temp *AuthKey, d time.Duration) int64 {
	at := time.Now().Add(d).Unix()
	if temp.ExpiresAt != 0 && temp.ExpiresAt < at {
		return temp.ExpiresAt
	}
	return at
}
