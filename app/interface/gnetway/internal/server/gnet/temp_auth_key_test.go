package gnet

import (
	"crypto/rand"
	"errors"
	"testing"

	"github.com/teamgram/marmota/pkg/cache"
	"github.com/teamgram/proto/mtproto"
)

const (
	testNow       = int64(1_800_000_000)
	testTempKeyId = int64(0x7e3f_0000_0000_0001)
	testPermKeyId = int64(0x1c71_0000_0000_0002)
	testVictimId  = int64(0x0b0b_0000_0000_0003)
)

func testKeyBytes(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 256)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	return key
}

func testKeyInfo(t *testing.T, id int64, keyType int, permAuthKeyId int64) *mtproto.AuthKeyInfo {
	t.Helper()
	k := mtproto.NewAuthKeyInfo(id, testKeyBytes(t), keyType)
	if keyType != mtproto.AuthKeyTypePerm {
		k.PermAuthKeyId = permAuthKeyId
	}
	return k
}

// bindPayload is a decrypted message (without salt and session_id) whose
// container carries auth.bindTempAuthKey naming the victim's perm key and a
// request that would act for the victim.
func bindPayload(t *testing.T) []byte {
	t.Helper()
	x := mtproto.NewEncodeBuf(512)
	msg := &mtproto.TLMessage2{
		MsgId: 0x6000_0000_0000_0004,
		Seqno: 0,
		Object: &mtproto.TLMsgContainer{
			Messages: []*mtproto.TLMessage2{
				{
					MsgId: 0x6000_0000_0000_0008,
					Seqno: 1,
					Object: &mtproto.TLAuthBindTempAuthKey{
						Constructor:      mtproto.CRC32_auth_bindTempAuthKey,
						PermAuthKeyId:    testVictimId,
						Nonce:            1,
						ExpiresAt:        int32(testNow + 86400),
						EncryptedMessage: make([]byte, 104),
					},
				},
				{
					MsgId:  0x6000_0000_0000_000c,
					Seqno:  3,
					Object: &mtproto.TLHelpGetConfig{Constructor: mtproto.CRC32_help_getConfig},
				},
			},
		},
	}
	if err := msg.Encode(x, 228); err != nil {
		t.Fatal(err)
	}
	return x.GetBuf()
}

func TestNewSessionClientDataRoutesByKeyDataOnly(t *testing.T) {
	cases := []struct {
		name        string
		key         *mtproto.AuthKeyInfo
		wantRoute   int64
		wantPermId  int64
		wantKeyType int32
	}{
		{
			name:        "unbound temp key: own wrapper, no perm key, whatever the message names",
			key:         testKeyInfo(t, testTempKeyId, mtproto.AuthKeyTypeTemp, 0),
			wantRoute:   testTempKeyId,
			wantPermId:  0,
			wantKeyType: mtproto.AuthKeyTypeTemp,
		},
		{
			name:        "unbound media temp key",
			key:         testKeyInfo(t, testTempKeyId, mtproto.AuthKeyTypeMediaTemp, 0),
			wantRoute:   testTempKeyId,
			wantPermId:  0,
			wantKeyType: mtproto.AuthKeyTypeMediaTemp,
		},
		{
			name:        "temp key bound by authsession",
			key:         testKeyInfo(t, testTempKeyId, mtproto.AuthKeyTypeTemp, testPermKeyId),
			wantRoute:   testPermKeyId,
			wantPermId:  testPermKeyId,
			wantKeyType: mtproto.AuthKeyTypeTemp,
		},
		{
			name:        "perm key",
			key:         testKeyInfo(t, testPermKeyId, mtproto.AuthKeyTypePerm, 0),
			wantRoute:   testPermKeyId,
			wantPermId:  testPermKeyId,
			wantKeyType: mtproto.AuthKeyTypePerm,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			payload := bindPayload(t)
			routeKey, data := newSessionClientData("gw", newAuthKeyUtil(c.key), 1, "192.0.2.1", 5, 6, payload)
			if routeKey != c.wantRoute || data.PermAuthKeyId != c.wantPermId || data.KeyType != c.wantKeyType {
				t.Fatalf("route %d perm %d type %d, want route %d perm %d type %d",
					routeKey, data.PermAuthKeyId, data.KeyType, c.wantRoute, c.wantPermId, c.wantKeyType)
			}
			if data.AuthKeyId != c.key.AuthKeyId || data.SessionId != 5 || data.Salt != 6 || data.ConnType != 1 {
				t.Fatalf("unexpected data: %v", data)
			}
			if routeKey == testVictimId || data.PermAuthKeyId == testVictimId {
				t.Fatal("message content attributed the key to the perm key it names")
			}
		})
	}
}

func TestUnboundTempKeyCacheIsNotPoisoned(t *testing.T) {
	s := &Server{cache: cache.NewLRUCache(1 << 20)}
	key := newCheckedAuthKeyUtil(testKeyInfo(t, testTempKeyId, mtproto.AuthKeyTypeTemp, 0), testNow, 0)
	s.PutAuthKey(key)

	_, _ = newSessionClientData("gw", key, 1, "192.0.2.1", 5, 6, bindPayload(t))

	cached := s.GetAuthKey(testTempKeyId)
	if cached == nil || cached.PermAuthKeyId() != 0 {
		t.Fatalf("cached temp key = %v, want unbound", cached)
	}
	if !cached.needsRecheck(testNow) {
		t.Fatal("unbound temp key must be re-read from authsession before every message")
	}

	s.RemoveAuthKey(testTempKeyId)
	if s.GetAuthKey(testTempKeyId) != nil {
		t.Fatal("RemoveAuthKey kept the key")
	}
}

func TestAuthKeyUtilRecheckAndExpiry(t *testing.T) {
	perm := newCheckedAuthKeyUtil(testKeyInfo(t, testPermKeyId, mtproto.AuthKeyTypePerm, 0), testNow, 0)
	bound := newCheckedAuthKeyUtil(testKeyInfo(t, testTempKeyId, mtproto.AuthKeyTypeTemp, testPermKeyId), testNow, testNow+3600)
	unbound := newCheckedAuthKeyUtil(testKeyInfo(t, testTempKeyId, mtproto.AuthKeyTypeTemp, 0), testNow, 0)

	cases := []struct {
		name        string
		key         *authKeyUtil
		now         int64
		wantRecheck bool
		wantExpired bool
	}{
		{"perm key within the recheck interval", perm, testNow + authKeyRecheckInterval - 1, false, false},
		{"perm key after the recheck interval (it may be destroyed)", perm, testNow + authKeyRecheckInterval, true, false},
		{"perm key never expires", perm, testNow + 365*86400, true, false},
		{"bound temp key within the recheck interval", bound, testNow + authKeyRecheckInterval - 1, false, false},
		{"bound temp key after the recheck interval", bound, testNow + authKeyRecheckInterval, true, false},
		{"bound temp key at its expiry", bound, testNow + 3600, true, true},
		{"unbound temp key is always rechecked", unbound, testNow, true, false},
	}
	for _, c := range cases {
		if got := c.key.needsRecheck(c.now); got != c.wantRecheck {
			t.Errorf("%s: needsRecheck = %v, want %v", c.name, got, c.wantRecheck)
		}
		if got := c.key.expired(c.now); got != c.wantExpired {
			t.Errorf("%s: expired = %v, want %v", c.name, got, c.wantExpired)
		}
	}
}

func TestCheckQueriedAuthKey(t *testing.T) {
	bound := testKeyInfo(t, testTempKeyId, mtproto.AuthKeyTypeTemp, testPermKeyId)
	known := newCheckedAuthKeyUtil(bound, testNow-10, testNow+3600)

	key, err := checkQueriedAuthKey(testTempKeyId, bound, testNow, known)
	if err != nil {
		t.Fatalf("checkQueriedAuthKey: %v", err)
	}
	if key.PermAuthKeyId() != testPermKeyId || key.expiresAt != testNow+3600 || key.recheckAt != testNow+authKeyRecheckInterval {
		t.Fatalf("unexpected key: perm %d expiresAt %d recheckAt %d", key.PermAuthKeyId(), key.expiresAt, key.recheckAt)
	}

	if _, err = checkQueriedAuthKey(testTempKeyId, bound, testNow+3600, known); !errors.Is(err, mtproto.ErrAuthKeyUnregistered) {
		t.Fatalf("expired key: error = %v, want AUTH_KEY_UNREGISTERED", err)
	}
	if _, err = checkQueriedAuthKey(testPermKeyId, bound, testNow, nil); err == nil {
		t.Fatal("key for another auth_key_id accepted")
	}
	if _, err = checkQueriedAuthKey(testTempKeyId, nil, testNow, nil); err == nil {
		t.Fatal("nil key accepted")
	}
	short := testKeyInfo(t, testTempKeyId, mtproto.AuthKeyTypeTemp, testPermKeyId)
	short.AuthKey = short.AuthKey[:128]
	if _, err = checkQueriedAuthKey(testTempKeyId, short, testNow, nil); err == nil {
		t.Fatal("short key accepted")
	}

	perm := testKeyInfo(t, testPermKeyId, mtproto.AuthKeyTypePerm, 0)
	key, err = checkQueriedAuthKey(testPermKeyId, perm, testNow, nil)
	if err != nil || key.recheckAt != testNow+authKeyRecheckInterval || key.expiresAt != 0 {
		t.Fatalf("perm key: %v, recheckAt %d, expiresAt %d", err, key.recheckAt, key.expiresAt)
	}
}
