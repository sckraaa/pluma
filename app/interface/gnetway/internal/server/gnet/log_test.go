package gnet

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"math/big"
	"strings"
	"sync"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/crypto"

	"github.com/zeromicro/go-zero/core/logx"
)

// syncBuffer collects log output written from the pool's goroutines.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// captureLogs sends logx output, debug level included, to a buffer for the
// rest of the test.
func captureLogs(t *testing.T) *syncBuffer {
	t.Helper()
	out := &syncBuffer{}
	logx.SetWriter(logx.NewWriter(out))
	logx.SetLevel(logx.DebugLevel)
	t.Cleanup(func() {
		logx.SetWriter(logx.NewWriter(io.Discard))
		logx.SetLevel(logx.InfoLevel)
	})
	return out
}

// leaks reports whether logs contain any 6-byte window of secret, raw or
// hex-encoded in either case.
func leaks(logs string, secret []byte) bool {
	lower := strings.ToLower(logs)
	for i := 0; i+6 <= len(secret); i++ {
		w := secret[i : i+6]
		if strings.Contains(logs, string(w)) || strings.Contains(lower, hex.EncodeToString(w)) {
			return true
		}
	}
	return false
}

// TestHandshakeLogsNoSecrets runs handshakes with debug logging and looks for
// new_nonce, g_b and the auth key in the output (audit finding #8).
func TestHandshakeLogsNoSecrets(t *testing.T) {
	logs := captureLogs(t)
	for i, temp := range []bool{false, true} {
		h := newTestHarness(t)
		c := h.newConn(int64(i+1), nil)
		tc, _ := h.handshake(c, temp)

		gB := new(big.Int).Exp(big.NewInt(tc.g), tc.b, tc.dhPrime).Bytes()
		out := logs.String()
		for what, secret := range map[string][]byte{
			"new_nonce": tc.newNonce,
			"auth_key":  tc.authKey,
			"g_b":       gB,
			"b":         tc.b.Bytes(),
		} {
			if leaks(out, secret) {
				t.Errorf("temp=%v: logs contain %s", temp, what)
			}
		}
	}
}

// TestDecryptFailureLogsNoPacket sends garbage under a known auth_key_id:
// the log names the key and the sizes, not the packet (audit finding #9).
func TestDecryptFailureLogsNoPacket(t *testing.T) {
	logs := captureLogs(t)
	h := newTestHarness(t)
	known := testKeyInfo(t, testPermKeyId, mtproto.AuthKeyTypePerm, 0)
	h.s.PutAuthKey(newCheckedAuthKeyUtil(known, testNow, 0))

	frame := make([]byte, 8, 8+16+256)
	binary.LittleEndian.PutUint64(frame, uint64(testPermKeyId))
	frame = append(frame, crypto.GenerateNonce(16+256)...)
	c := h.newConn(1, nil)
	h.sendFrame(c, frame)

	out := logs.String()
	if !strings.Contains(out, fmt.Sprint(testPermKeyId)) {
		t.Fatalf("decryption failure not logged with its auth_key_id: %q", out)
	}
	if leaks(out, frame[8:]) {
		t.Errorf("logs contain the packet: %q", out)
	}
	if leaks(out, known.AuthKey) {
		t.Errorf("logs contain the auth key")
	}
}

// TestHandshakeStateFormat checks that no verb prints new_nonce or a.
func TestHandshakeStateFormat(t *testing.T) {
	st := HandshakeStateCtx{
		State:    3,
		Nonce:    crypto.GenerateNonce(16),
		NewNonce: crypto.GenerateNonce(32),
		A:        crypto.GenerateNonce(256),
	}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X"} {
		for _, v := range []any{st, &st} {
			out := fmt.Sprintf(verb, v)
			if leaks(out, st.NewNonce) || leaks(out, st.A) {
				t.Errorf("%s of %T prints a secret: %s", verb, v, out)
			}
			if !strings.Contains(out, "state: 3") {
				t.Errorf("%s of %T: %q", verb, v, out)
			}
		}
	}
}
