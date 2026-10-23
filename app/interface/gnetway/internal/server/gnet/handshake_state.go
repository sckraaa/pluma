package gnet

import (
	"bytes"
	"container/list"
	"errors"
	"sync"
)

// Handshake states (vars.go) a handshake goes through:
//
//	req_pq(_multi)        → STATE_pq_res
//	req_DH_params         STATE_pq_res → STATE_DH_params (computing) → STATE_DH_params_res
//	set_client_DH_params  STATE_DH_params_res → STATE_dh_gen (computing) →
//	                      done (dh_gen_ok, the state is dropped) or
//	                      STATE_DH_params_res again (dh_gen_retry)
//
// A message that does not match the state of its nonce is rejected, so
// out-of-order, repeated and concurrent messages never reach the crypto code
// with missing or half-written state.

const (
	// maxConnHandshakes bounds the handshakes in progress on one connection
	// (a client runs perm, temp and media temp key handshakes).
	maxConnHandshakes = 3
	// maxHttpHandshakes bounds the handshakes in progress over the HTTP
	// transport, whose requests may arrive on different connections.
	maxHttpHandshakes = 4096
	// httpHandshakeTTL is how long (seconds) an HTTP handshake may take.
	httpHandshakeTTL = 60
)

var errHandshakeState = errors.New("handshake: message does not match the handshake state")

// handshakeStore keeps the state of handshakes in progress by nonce. It is
// safe for concurrent use: the event loop starts steps and the worker pool
// finishes them. It hands out copies, so a step computed in the pool never
// shares memory with state the event loop may change.
type handshakeStore struct {
	mu    sync.Mutex
	max   int
	ttl   int64 // seconds; 0: states do not expire
	order *list.List
	byKey map[string]*list.Element
}

type handshakeEntry struct {
	key      string
	state    HandshakeStateCtx
	deadline int64
}

func newHandshakeStore(max int, ttl int64) *handshakeStore {
	return &handshakeStore{
		max:   max,
		ttl:   ttl,
		order: list.New(),
		byKey: make(map[string]*list.Element),
	}
}

// put starts a handshake (after req_pq), replacing one with the same nonce.
// When the store is full the oldest handshake is dropped.
func (hs *handshakeStore) put(state HandshakeStateCtx, now int64) {
	key := string(state.Nonce)

	hs.mu.Lock()
	defer hs.mu.Unlock()

	hs.expireLocked(now)
	if e, ok := hs.byKey[key]; ok {
		hs.removeLocked(e)
	}
	for hs.order.Len() >= hs.max {
		hs.removeLocked(hs.order.Front())
	}
	entry := &handshakeEntry{key: key, state: state}
	if hs.ttl > 0 {
		entry.deadline = now + hs.ttl
	}
	hs.byKey[key] = hs.order.PushBack(entry)
}

// begin starts a step: the handshake for nonce must be in state from with
// the given server_nonce; it moves to state to and a copy of it is returned.
func (hs *handshakeStore) begin(nonce, serverNonce []byte, from, to int32, now int64) (HandshakeStateCtx, error) {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	e := hs.getLocked(nonce, now)
	if e == nil {
		return HandshakeStateCtx{}, errHandshakeState
	}
	entry := e.Value.(*handshakeEntry)
	if entry.state.State != from || !bytes.Equal(entry.state.ServerNonce, serverNonce) {
		return HandshakeStateCtx{}, errHandshakeState
	}
	entry.state.State = to
	return entry.state, nil
}

// finish ends a step started by begin: if the handshake for nonce is still in
// state at, update changes it (it is dropped if update returns false).
// finish reports whether the handshake was still there.
func (hs *handshakeStore) finish(nonce []byte, at int32, now int64, update func(st *HandshakeStateCtx) bool) bool {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	e := hs.getLocked(nonce, now)
	if e == nil {
		return false
	}
	entry := e.Value.(*handshakeEntry)
	if entry.state.State != at {
		return false
	}
	if !update(&entry.state) {
		hs.removeLocked(e)
	}
	return true
}

// drop forgets the handshake for nonce (a failed step).
func (hs *handshakeStore) drop(nonce []byte) {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	if e, ok := hs.byKey[string(nonce)]; ok {
		hs.removeLocked(e)
	}
}

// get returns a copy of the state for nonce (for tests and logging).
func (hs *handshakeStore) get(nonce []byte, now int64) (HandshakeStateCtx, bool) {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	e := hs.getLocked(nonce, now)
	if e == nil {
		return HandshakeStateCtx{}, false
	}
	return e.Value.(*handshakeEntry).state, true
}

func (hs *handshakeStore) len() int {
	hs.mu.Lock()
	defer hs.mu.Unlock()
	return hs.order.Len()
}

func (hs *handshakeStore) getLocked(nonce []byte, now int64) *list.Element {
	e, ok := hs.byKey[string(nonce)]
	if !ok {
		return nil
	}
	if d := e.Value.(*handshakeEntry).deadline; d != 0 && now >= d {
		hs.removeLocked(e)
		return nil
	}
	return e
}

func (hs *handshakeStore) expireLocked(now int64) {
	for e := hs.order.Front(); e != nil; e = hs.order.Front() {
		if d := e.Value.(*handshakeEntry).deadline; d == 0 || now < d {
			return
		}
		hs.removeLocked(e)
	}
}

func (hs *handshakeStore) removeLocked(e *list.Element) {
	delete(hs.byKey, e.Value.(*handshakeEntry).key)
	hs.order.Remove(e)
}
