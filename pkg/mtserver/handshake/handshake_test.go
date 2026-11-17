package handshake

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
	"github.com/teamgram/teamgram-server/pkg/serverkey"
)

var (
	testKeyOnce sync.Once
	testKey     *rsa.PrivateKey
)

// newTestServer returns a Server with a throwaway RSA key.
func newTestServer(t *testing.T) (*Server, *rsa.PrivateKey) {
	t.Helper()
	testKeyOnce.Do(func() {
		k, err := rsa.GenerateKey(rand.Reader, serverkey.Bits)
		if err != nil {
			panic(err)
		}
		testKey = k
	})
	s, err := New([]*serverkey.Key{{
		Path:        "test",
		Private:     testKey,
		PEM:         pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(testKey)}),
		Fingerprint: serverkey.Fingerprint(&testKey.PublicKey),
	}})
	if err != nil {
		t.Fatal(err)
	}
	return s, testKey
}

// savedKey is a key memSaver stored.
type savedKey struct {
	key       AuthKey
	expiresIn int32
}

// memSaver keeps keys in memory, as a server without a session service
// does; refuse, when set, decides whether a key is stored.
type memSaver struct {
	mu     sync.Mutex
	keys   []savedKey
	refuse func(key *AuthKey) error
}

func (m *memSaver) Save(_ context.Context, key *AuthKey, expiresIn int32) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.refuse != nil {
		if err := m.refuse(key); err != nil {
			return err
		}
	}
	for _, k := range m.keys {
		if k.key.AuthKeyId == key.AuthKeyId {
			return ErrKeyIdTaken
		}
	}
	m.keys = append(m.keys, savedKey{key: *key, expiresIn: expiresIn})
	return nil
}

func (m *memSaver) saved() []savedKey {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]savedKey(nil), m.keys...)
}

// exchangeTransport is a client connection to an Exchange: packets go to
// Handle, answers come back in order, the DH steps run on goroutines as in
// a worker pool.
type exchangeTransport struct {
	s   *Server
	x   *Exchange
	out chan []byte
	err chan error
}

func newExchangeTransport(s *Server, keys KeySaver, admit func() error) *exchangeTransport {
	t := &exchangeTransport{s: s, out: make(chan []byte, 4), err: make(chan error, 4)}
	t.x = &Exchange{
		States: NewStore(3, 0),
		Keys:   keys,
		Now:    func() int64 { return time.Now().Unix() },
		Admit:  admit,
		Submit: func(f func()) error { go f(); return nil },
		Reply:  func(answer mtproto.TLObject) { t.out <- Payload(answer) },
		Fail:   func(err error) { t.err <- err },
	}
	return t
}

func (t *exchangeTransport) WritePacket(p []byte) error {
	answer, _, err := t.s.Handle(t.x, p)
	if err != nil {
		return err
	}
	if answer != nil {
		t.out <- Payload(answer)
	}
	return nil
}

func (t *exchangeTransport) ReadPacket() ([]byte, error) {
	select {
	case p := <-t.out:
		return p, nil
	case err := <-t.err:
		return nil, err
	case <-time.After(30 * time.Second):
		return nil, errors.New("no answer")
	}
}

func (t *exchangeTransport) Close() error { return nil }

// TestKeyExchange runs the key exchange of pkg/mtclient against the
// handshake: the KeySaver gets the key the client computed, with its type,
// first server salt and expires_in.
func TestKeyExchange(t *testing.T) {
	s, key := newTestServer(t)
	for _, c := range []struct {
		name     string
		opts     mtclient.KeyOptions
		wantType int
	}{
		{"permanent (p_q_inner_data_dc)", mtclient.KeyOptions{DC: 201}, mtproto.AuthKeyTypePerm},
		{"temporary (p_q_inner_data_temp_dc)", mtclient.KeyOptions{DC: 201, Temp: true, ExpiresIn: 3600}, mtproto.AuthKeyTypeTemp},
		{"media temporary", mtclient.KeyOptions{DC: -201, Temp: true, ExpiresIn: 600}, mtproto.AuthKeyTypeMediaTemp},
	} {
		t.Run(c.name, func(t *testing.T) {
			saver := &memSaver{}
			tr := newExchangeTransport(s, saver, nil)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			got, err := mtclient.CreateAuthKey(ctx, tr, &key.PublicKey, c.opts)
			if err != nil {
				t.Fatal(err)
			}
			saved := saver.saved()
			if len(saved) != 1 {
				t.Fatalf("saved %d keys", len(saved))
			}
			k := saved[0]
			if k.key.AuthKeyId != got.ID || !bytes.Equal(k.key.AuthKey, got.Key) {
				t.Fatal("the server saved another key than the client computed")
			}
			if k.key.AuthKeyType != c.wantType {
				t.Fatalf("key type %d, want %d", k.key.AuthKeyType, c.wantType)
			}
			if k.key.ServerSalt != got.ServerSalt {
				t.Fatalf("server salt %d, the client has %d", k.key.ServerSalt, got.ServerSalt)
			}
			if k.expiresIn != c.opts.ExpiresIn {
				t.Fatalf("expires_in %d, want %d", k.expiresIn, c.opts.ExpiresIn)
			}
			if n := tr.x.States.Len(); n != 0 {
				t.Fatalf("%d handshakes left", n)
			}
		})
	}
}

// TestKeyExchangeRetry: ErrKeyIdTaken from the KeySaver is dh_gen_retry,
// after which the client completes with another key; any other error is
// dh_gen_fail.
func TestKeyExchangeRetry(t *testing.T) {
	s, key := newTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var first int64
	saver := &memSaver{}
	saver.refuse = func(k *AuthKey) error {
		if first == 0 {
			first = k.AuthKeyId
			return ErrKeyIdTaken
		}
		return nil
	}
	got, err := mtclient.CreateAuthKey(ctx, newExchangeTransport(s, saver, nil), &key.PublicKey, mtclient.KeyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	saved := saver.saved()
	if len(saved) != 1 || saved[0].key.AuthKeyId != got.ID || got.ID == first {
		t.Fatalf("after dh_gen_retry: %d keys", len(saved))
	}

	saver = &memSaver{refuse: func(*AuthKey) error { return errors.New("storage unavailable") }}
	_, err = mtclient.CreateAuthKey(ctx, newExchangeTransport(s, saver, nil), &key.PublicKey, mtclient.KeyOptions{})
	if err == nil || !strings.Contains(err.Error(), "dh_gen_fail") {
		t.Fatalf("err = %v, want dh_gen_fail", err)
	}
}

// TestAdmit: a refused req_DH_params is rejected before any RSA work, and
// its handshake is forgotten.
func TestAdmit(t *testing.T) {
	s, key := newTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	limit := errors.New("too many key exchanges")
	saver := &memSaver{}
	tr := newExchangeTransport(s, saver, func() error { return limit })
	if _, err := mtclient.CreateAuthKey(ctx, tr, &key.PublicKey, mtclient.KeyOptions{}); !errors.Is(err, limit) {
		t.Fatalf("err = %v, want the limit", err)
	}
	if len(saver.saved()) != 0 || tr.x.States.Len() != 0 {
		t.Fatal("a refused handshake left a key or state")
	}
}

// TestHandleRejects: only the handshake objects are taken unencrypted, and
// steps out of order are rejected.
func TestHandleRejects(t *testing.T) {
	s, _ := newTestServer(t)
	tr := newExchangeTransport(s, &memSaver{}, nil)
	frame := func(obj mtproto.TLObject) []byte {
		p := Payload(obj)
		binary.LittleEndian.PutUint64(p[8:], uint64(time.Now().Unix())<<32)
		return p
	}
	nonce := make([]byte, 16)
	for _, c := range []struct {
		name  string
		frame []byte
	}{
		{"not a handshake object", frame(&mtproto.TLPing{Constructor: mtproto.CRC32_ping, PingId: 1})},
		{"req_DH_params without req_pq", frame(&mtproto.TLReq_DHParams{
			Constructor: mtproto.CRC32_req_DH_params,
			Nonce:       nonce, ServerNonce: nonce, P: "\x01", Q: "\x01", EncryptedData: string(make([]byte, 256)),
		})},
		{"short nonce", frame(&mtproto.TLReqPqMulti{Constructor: mtproto.CRC32_req_pq_multi, Nonce: nonce[:8]})},
		{"truncated", frame(&mtproto.TLReqPqMulti{Constructor: mtproto.CRC32_req_pq_multi, Nonce: nonce})[:24]},
	} {
		if _, _, err := s.Handle(tr.x, c.frame); err == nil {
			t.Errorf("%s: accepted", c.name)
		}
	}
	if answer, pending, err := s.Handle(tr.x, frame(mtproto.MakeTLMsgsAck(&mtproto.MsgsAck{MsgIds: []int64{1}}))); err != nil || answer != nil || pending {
		t.Errorf("msgs_ack: %v %v %v", answer, pending, err)
	}
}

// TestFingerprints: resPQ advertises every key, and the list cannot be
// changed through Fingerprints.
func TestFingerprints(t *testing.T) {
	s, key := newTestServer(t)
	fp := int64(serverkey.Fingerprint(&key.PublicKey))
	got := s.Fingerprints()
	if len(got) != 1 || got[0] != fp {
		t.Fatalf("fingerprints %v", got)
	}
	got[0] = 0
	if s.Fingerprints()[0] != fp || s.Cryptor(fp) == nil || s.Cryptor(fp+1) != nil {
		t.Fatal(fmt.Sprint("fingerprints changed or keys not found: ", s.Fingerprints()))
	}
	if _, err := New(nil); err == nil {
		t.Fatal("a server without keys")
	}
}
