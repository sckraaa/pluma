package logic

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/authorization/model"
	"github.com/teamgram/teamgram-server/pkg/logincode"
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/kv"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"google.golang.org/grpc/status"
)

const newPhone = "79991234567"

type memStore map[string]model.PhoneCodeTransaction

func (s memStore) GetCachePhoneCode(_ context.Context, authKeyId int64, phone string) (*model.PhoneCodeTransaction, error) {
	v, ok := s[fmt.Sprint(authKeyId, phone)]
	if !ok {
		return nil, nil
	}
	return &v, nil
}

func (s memStore) PutCachePhoneCode(_ context.Context, authKeyId int64, phone string, v *model.PhoneCodeTransaction) error {
	s[fmt.Sprint(authKeyId, phone)] = *v
	return nil
}

func (s memStore) DeleteCachePhoneCode(_ context.Context, authKeyId int64, phone string) error {
	delete(s, fmt.Sprint(authKeyId, phone))
	return nil
}

type allowlist map[string]bool

func (a allowlist) Contains(_ context.Context, phone string) (bool, error) { return a[phone], nil }

func rpcMessage(err error) string {
	if st, ok := status.FromError(err); ok && err != nil {
		return st.Message()
	}
	return fmt.Sprint(err)
}

// TestChangePhoneRequiresCode guards against the upstream bug where
// account.changePhone ignored the result of the code check.
func TestChangePhoneRequiresCode(t *testing.T) {
	ctx := context.Background()
	mr := miniredis.RunT(t)
	codes, err := logincode.NewService(logincode.Config{HmacKey: strings.Repeat("k", 32)}, allowlist{newPhone: true}, kv.NewStore(kv.KvConf{cache.NodeConf{
		RedisConf: redis.RedisConf{Host: mr.Addr(), Type: redis.NodeType},
		Weight:    100,
	}}))
	if err != nil {
		t.Fatal(err)
	}
	store := memStore{}
	m := NewAuthSignLogic(store, codes)

	if err := m.DoChangePhone(ctx, 1, newPhone, "123456", "nohash", ""); rpcMessage(err) != "PHONE_CODE_EXPIRED" {
		t.Fatalf("without a request: %v", err)
	}

	codeData, err := m.DoSendChangePhoneCode(ctx, 1, 2, newPhone, "203.0.113.1")
	if err != nil {
		t.Fatal(err)
	}
	if got := codeData.ToAuthSentCode().GetType().GetPredicateName(); got != mtproto.Predicate_auth_sentCodeTypeSms {
		t.Fatalf("sent code type %s", got)
	}
	// Nothing works before the administrator issues a code.
	if err := m.DoChangePhone(ctx, 1, newPhone, "123456", codeData.PhoneCodeHash, ""); rpcMessage(err) != "PHONE_CODE_INVALID" {
		t.Fatalf("before the administrator: %v", err)
	}
	r, err := codes.Reissue(ctx, newPhone, "")
	if err != nil {
		t.Fatal(err)
	}
	wrong := "000000"
	if r.Code == wrong {
		wrong = "111111"
	}
	if err := m.DoChangePhone(ctx, 1, newPhone, wrong, codeData.PhoneCodeHash, ""); rpcMessage(err) != "PHONE_CODE_INVALID" {
		t.Fatalf("wrong code: %v", err)
	}
	if err := m.DoChangePhone(ctx, 2, newPhone, r.Code, codeData.PhoneCodeHash, ""); rpcMessage(err) != "PHONE_CODE_EXPIRED" {
		t.Fatalf("another auth key: %v", err)
	}
	if err := m.DoChangePhone(ctx, 1, newPhone, r.Code, codeData.PhoneCodeHash, ""); err != nil {
		t.Fatalf("right code: %v", err)
	}
	if len(store) != 0 {
		t.Fatal("request kept after use")
	}
	if err := m.DoChangePhone(ctx, 1, newPhone, r.Code, codeData.PhoneCodeHash, ""); rpcMessage(err) != "PHONE_CODE_EXPIRED" {
		t.Fatalf("code reused: %v", err)
	}
}
