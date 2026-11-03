//go:build integration

package presence

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/teamgram/teamgram-server/pkg/testinfra"
	"github.com/zeromicro/go-zero/core/stores/kv"
)

// The script runs on the production Redis here; the unit tests run it on
// miniredis, whose Lua is an imitation.

var itRedis *testinfra.Redis

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	if itRedis, err = testinfra.StartRedis(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	_ = itRedis.Terminate(ctx)
	os.Exit(code)
}

// itUser keeps the users of a run apart from those of another run on the
// same server (go test -count=N).
var itUserSeq int64 = int64(os.Getpid()) << 20

func itUser() int64 {
	itUserSeq++
	return itUserSeq
}

func TestRedisDevices(t *testing.T) {
	s := NewStore(kv.NewStore(itRedis.KvConf()))
	runSteps(t, s, itUser(), 1_800_000_000, devices)
}

func TestRedisDevicesAtOnce(t *testing.T) {
	s := NewStore(kv.NewStore(itRedis.KvConf()))
	if n := pushesAtOnce(t, s, itUser(), 16); n != 1 {
		t.Fatalf("%d pushes for one change, want 1", n)
	}
}
