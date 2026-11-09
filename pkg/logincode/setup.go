package logincode

import (
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/pkg/phoneallowlist"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/kv"
)

// MustNewService returns the Service of a bff: the allowlist in MySQL, codes
// and counters in Redis. It stops the process if the config is unusable
// (e.g. LOGIN_CODE_HMAC_KEY missing or too short).
func MustNewService(c Config, mysql sqlx.Config, kvConf kv.KvConf) *Service {
	db := sqlx.NewMySQL(&mysql)
	s, err := NewService(c, phoneallowlist.New(db), kv.NewStore(kvConf))
	logx.Must(err)
	s.SetRegistrationSwitch(NewRegistration(db))
	return s
}
