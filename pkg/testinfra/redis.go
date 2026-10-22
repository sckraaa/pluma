package testinfra

import (
	"context"
	"time"

	"github.com/teamgram/teamgram-server/pkg/logpolicy"

	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/kv"
	"github.com/zeromicro/go-zero/core/stores/redis"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Redis is a running Redis server configured as in docker-compose-env.yaml:
// append-only file on, a password required.
type Redis struct {
	Container *testcontainers.DockerContainer
	// Addr is host:port of the server; Password its requirepass.
	Addr     string
	Password string
}

// StartRedis starts the production Redis image. It applies the production
// log policy, as StartMySQL does.
func StartRedis(ctx context.Context) (*Redis, error) {
	logpolicy.Apply()
	r := &Redis{Password: randomSecret()}
	ctr, err := runService(ctx, "redis",
		testcontainers.WithExposedPorts("6379/tcp"),
		testcontainers.WithCmd("redis-server", "--appendonly", "yes", "--requirepass", r.Password),
		testcontainers.WithWaitStrategyAndDeadline(time.Minute,
			wait.ForLog("Ready to accept connections"),
			wait.ForListeningPort("6379/tcp"),
		),
	)
	if err != nil {
		return nil, startError("redis", ctr, err)
	}
	r.Container = ctr
	if r.Addr, err = ctr.PortEndpoint(ctx, "6379/tcp", ""); err != nil {
		_ = ctr.Terminate(ctx)
		return nil, err
	}
	return r, nil
}

// RedisConf is a go-zero node config for the server, as the services write it
// (Host and Pass).
func (r *Redis) RedisConf() redis.RedisConf {
	return redis.RedisConf{Host: r.Addr, Pass: r.Password, Type: redis.NodeType}
}

// CacheConf is the server as a go-zero cache cluster of one node (the Cache
// section of the service configs).
func (r *Redis) CacheConf() cache.CacheConf {
	return cache.CacheConf{{RedisConf: r.RedisConf(), Weight: 100}}
}

// KvConf is the server as a go-zero kv cluster of one node (the KV section of
// the service configs).
func (r *Redis) KvConf() kv.KvConf {
	return kv.KvConf(r.CacheConf())
}

// Terminate stops and removes the container.
func (r *Redis) Terminate(ctx context.Context) error {
	if r == nil || r.Container == nil {
		return nil
	}
	return r.Container.Terminate(ctx)
}
