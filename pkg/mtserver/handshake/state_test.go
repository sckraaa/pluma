package handshake

import (
	"bytes"
	"testing"
)

const testNow = 1_700_000_000

func TestHandshakeStore(t *testing.T) {
	st := func(i byte) HandshakeStateCtx {
		return HandshakeStateCtx{State: StatePQRes, Nonce: bytes.Repeat([]byte{i}, 16), ServerNonce: []byte{i}}
	}

	t.Run("bounded, oldest first", func(t *testing.T) {
		hs := NewStore(3, 0)
		for i := byte(0); i < 10; i++ {
			hs.Put(st(i), testNow)
		}
		if hs.Len() != 3 {
			t.Fatalf("len %d", hs.Len())
		}
		if _, ok := hs.Get(st(6).Nonce, testNow); ok {
			t.Fatal("old state kept")
		}
		if _, ok := hs.Get(st(9).Nonce, testNow); !ok {
			t.Fatal("new state dropped")
		}
	})

	t.Run("expires", func(t *testing.T) {
		hs := NewStore(10, 60)
		hs.Put(st(1), testNow)
		if _, err := hs.Begin(st(1).Nonce, st(1).ServerNonce, StatePQRes, StateDHParams, testNow+60); err == nil {
			t.Fatal("expired state used")
		}
		if hs.Len() != 0 {
			t.Fatal("expired state kept")
		}
	})

	t.Run("transitions", func(t *testing.T) {
		hs := NewStore(10, 0)
		hs.Put(st(1), testNow)
		n, sn := st(1).Nonce, st(1).ServerNonce
		if _, err := hs.Begin(n, []byte{9}, StatePQRes, StateDHParams, testNow); err == nil {
			t.Fatal("wrong server_nonce accepted")
		}
		if _, err := hs.Begin(n, sn, StateDHParamsRes, StateDHGen, testNow); err == nil {
			t.Fatal("wrong state accepted")
		}
		snap, err := hs.Begin(n, sn, StatePQRes, StateDHParams, testNow)
		if err != nil || snap.State != StateDHParams {
			t.Fatalf("begin: %v %v", snap.State, err)
		}
		if _, err := hs.Begin(n, sn, StatePQRes, StateDHParams, testNow); err == nil {
			t.Fatal("the same step started twice")
		}
		// A new req_pq with the nonce restarts it: the step's result is dropped.
		hs.Put(st(1), testNow)
		if hs.Finish(n, StateDHParams, testNow, func(*HandshakeStateCtx) bool { return true }) {
			t.Fatal("finished a restarted handshake")
		}
		got, _ := hs.Get(n, testNow)
		if got.NewNonce != nil || got.State != StatePQRes {
			t.Fatal("state changed")
		}
	})

	t.Run("at least one", func(t *testing.T) {
		hs := NewStore(0, 0)
		hs.Put(st(1), testNow)
		hs.Put(st(2), testNow)
		if hs.Len() != 1 {
			t.Fatalf("len %d", hs.Len())
		}
	})
}
