package presence

import (
	"context"
	"sync"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/kv"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

func newTestStore(t *testing.T) (*Store, *miniredis.Miniredis) {
	t.Helper()
	// miniredis does not implement CLIENT SETINFO; DisableIdentity keeps the failing
	// connect-time command from tripping the go-zero redis breaker.
	mr := miniredis.RunT(t)
	return NewStore(kv.NewStore(kv.KvConf{cache.NodeConf{
		RedisConf: redis.RedisConf{Host: mr.Addr(), Type: redis.NodeType, DisableIdentity: true},
		Weight:    100,
	}})), mr
}

type step struct {
	name    string
	at      int64 // seconds after t0
	authKey int64
	online  bool
	want    Change // SeenUntil relative to t0
}

// devices is the life of one user with a phone (key 1) and a Desktop (key 2).
var devices = []step{
	{"phone comes online", 0, 1, true, Change{Window, true, true}},
	{"phone repeats soon", 10, 1, true, Change{10 + Window, true, false}},
	{"phone repeats with little of the told window left", 30, 1, true, Change{30 + Window, true, true}},
	{"desktop comes online, the user already is", 35, 2, true, Change{35 + Window, true, false}},
	{"phone goes offline, desktop keeps the user online", 40, 1, false, Change{35 + Window, true, false}},
	{"desktop goes offline", 50, 2, false, Change{50, false, true}},
	{"desktop offline again", 55, 2, false, Change{55, false, false}},
	{"phone back after a while", 1000, 1, true, Change{1000 + Window, true, true}},
	{"desktop offline while the phone is online", 1010, 2, false, Change{1000 + Window, true, false}},
	// The phone's window passed without offline (killed app): viewers have
	// turned it into "last seen" themselves; desktop's offline still tells
	// them the later moment.
	{"desktop offline after the phone's window passed", 1100, 2, false, Change{1100, false, true}},
}

func runSteps(t *testing.T, s *Store, userId int64, t0 int64, steps []step) {
	t.Helper()
	ctx := context.Background()
	for _, st := range steps {
		got, err := s.Update(ctx, userId, st.authKey, st.online, t0+st.at)
		if err != nil {
			t.Fatalf("%s: %v", st.name, err)
		}
		want := st.want
		want.SeenUntil += t0
		if got != want {
			t.Errorf("%s: got %+v, want %+v", st.name, got, want)
		}
	}
}

// Every request of a client inside its period extends the window the viewers
// know, whatever the period of the clients (30 s to 55 s).
func TestClientPeriods(t *testing.T) {
	for _, period := range []int64{30, 50, 55} {
		s, _ := newTestStore(t)
		ctx := context.Background()
		const t0 = 1_800_000_000
		var told int64
		for at := int64(0); at <= 600; at += period {
			c, err := s.Update(ctx, 9, 1, true, t0+at)
			if err != nil {
				t.Fatal(err)
			}
			if told != 0 && t0+at >= told {
				t.Fatalf("period %d s: request at %d s after the told window ended at %d", period, at, told-t0)
			}
			if !c.Push {
				t.Fatalf("period %d s: request at %d s told nobody", period, at)
			}
			told = c.SeenUntil
		}
	}
}

func TestDevices(t *testing.T) {
	s, _ := newTestStore(t)
	runSteps(t, s, 7, 1_800_000_000, devices)
}

func TestUsersApart(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	const now = 1_800_000_000
	if c, _ := s.Update(ctx, 1, 10, true, now); !c.Push {
		t.Fatalf("first user: %+v", c)
	}
	if c, _ := s.Update(ctx, 2, 10, true, now); !c.Push || !c.Online {
		t.Fatalf("second user with the same key id: %+v", c)
	}
}

func TestOfflineNeverTold(t *testing.T) {
	s, _ := newTestStore(t)
	c, err := s.Update(context.Background(), 3, 1, false, 1_800_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if c.Push || c.Online {
		t.Fatalf("offline of a user nobody was told about: %+v", c)
	}
}

func TestKeyExpires(t *testing.T) {
	s, mr := newTestStore(t)
	if _, err := s.Update(context.Background(), 4, 1, true, 1_800_000_000); err != nil {
		t.Fatal(err)
	}
	if ttl := mr.TTL(key(4)); ttl.Seconds() != keyTTL {
		t.Fatalf("ttl %v, want %d s", ttl, keyTTL)
	}
}

func TestRedisDown(t *testing.T) {
	s, mr := newTestStore(t)
	mr.Close()
	if _, err := s.Update(context.Background(), 5, 1, true, 1_800_000_000); err == nil {
		t.Fatal("no error with Redis down")
	}
}

// pushesAtOnce has n devices of one user come online at the same moment and
// returns how many of them were told to push.
func pushesAtOnce(t *testing.T, s *Store, userId int64, n int) int {
	t.Helper()
	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		pushes int
	)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(authKey int64) {
			defer wg.Done()
			c, err := s.Update(context.Background(), userId, authKey, true, 1_800_000_000)
			if err != nil {
				t.Error(err)
				return
			}
			if c.Push {
				mu.Lock()
				pushes++
				mu.Unlock()
			}
		}(int64(i + 1))
	}
	wg.Wait()
	return pushes
}

func TestDevicesAtOnce(t *testing.T) {
	s, _ := newTestStore(t)
	if n := pushesAtOnce(t, s, 6, 8); n != 1 {
		t.Fatalf("%d pushes for one change, want 1", n)
	}
}
