package mtclient

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"math/big"
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/crypto"
)

func TestFactorizePQ(t *testing.T) {
	// The pq of the gateway and of the protocol documentation.
	p, q, err := factorizePQ(0x17ED48941A08F981)
	if err != nil || p != 0x494C553B || q != 0x53911073 {
		t.Fatalf("factorizePQ = %#x, %#x, %v", p, q, err)
	}
	for i := 0; i < 20; i++ {
		a, _ := rand.Prime(rand.Reader, 31)
		b, _ := rand.Prime(rand.Reader, 31)
		if a.Cmp(b) == 0 {
			continue
		}
		pq := new(big.Int).Mul(a, b).Uint64()
		p, q, err = factorizePQ(pq)
		if err != nil || p*q != pq || p > q || (p != a.Uint64() && p != b.Uint64()) {
			t.Fatalf("factorizePQ(%d) = %d, %d, %v", pq, p, q, err)
		}
	}
}

// TestRSAPadDecrypts undoes RSA_PAD with the private key, as the server does.
func TestRSAPadDecrypts(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("p_q_inner_data: the payload of req_DH_params")
	enc, err := RSAPad(rand.Reader, &priv.PublicKey, data)
	if err != nil || len(enc) != 256 {
		t.Fatalf("RSAPad: %d bytes, %v", len(enc), err)
	}

	m := new(big.Int).Exp(new(big.Int).SetBytes(enc), priv.D, priv.N).FillBytes(make([]byte, 256))
	tempKeyXor, aesEncrypted := m[:32], m[32:]
	h := sha256.Sum256(aesEncrypted)
	tempKey := make([]byte, 32)
	for i := range tempKey {
		tempKey[i] = tempKeyXor[i] ^ h[i]
	}
	dataWithHash, err := crypto.NewAES256IGECryptor(tempKey, make([]byte, 32)).Decrypt(aesEncrypted)
	if err != nil {
		t.Fatal(err)
	}
	dataWithPadding := slices.Clone(dataWithHash[:192])
	slices.Reverse(dataWithPadding)
	check := sha256.Sum256(append(slices.Clone(tempKey), dataWithPadding...))
	if !bytes.Equal(check[:], dataWithHash[192:]) {
		t.Fatal("hash of data_with_padding does not match")
	}
	if !bytes.HasPrefix(dataWithPadding, data) {
		t.Fatal("data not recovered")
	}

	if _, err = RSAPad(rand.Reader, &priv.PublicKey, make([]byte, 145)); err == nil {
		t.Fatal("RSAPad accepted 145 bytes")
	}
}

func TestNextMsgID(t *testing.T) {
	var last int64
	for i := 0; i < 1000; i++ {
		id := nextMsgID(last, 0)
		if id%4 != 0 || id <= last {
			t.Fatalf("msg_id %d after %d", id, last)
		}
		last = id
	}
	if d := last>>32 - time.Now().Unix(); d < -1 || d > 1 {
		t.Fatalf("msg_id time is %d s off", d)
	}
	if d := nextMsgID(0, time.Hour)>>32 - time.Now().Unix(); d < 3599 || d > 3601 {
		t.Fatalf("time offset not applied: %d s", d)
	}
}

// TestBindMessage decrypts the binding message as authsession does.
func TestBindMessage(t *testing.T) {
	perm := &AuthKey{Key: crypto.GenerateNonce(256)}
	perm.ID = AuthKeyID(perm.Key)
	const (
		msgID     = int64(0x6000_0000_0000_0004)
		nonce     = int64(0x0123_4567_89ab_cdef)
		tempKey   = int64(0x7e3f_0000_0000_0001)
		session   = int64(0x5e55)
		expiresAt = int32(1_800_086_400)
	)
	msg, err := bindMessage(perm, msgID, nonce, tempKey, session, expiresAt)
	if err != nil {
		t.Fatal(err)
	}
	if int64(binary.LittleEndian.Uint64(msg)) != perm.ID {
		t.Fatal("encrypted_message does not start with perm_auth_key_id")
	}
	plain, err := crypto.NewAuthKey(perm.ID, perm.Key).AesIgeDecryptV1(msg[8:24], msg[24:])
	if err != nil {
		t.Fatal(err)
	}
	if int64(binary.LittleEndian.Uint64(plain[16:])) != msgID ||
		binary.LittleEndian.Uint32(plain[24:]) != 0 ||
		binary.LittleEndian.Uint32(plain[28:]) != bindAuthKeyInnerSize {
		t.Fatal("binding message header")
	}
	body := plain[32:]
	if binary.LittleEndian.Uint32(body) != uint32(mtproto.CRC32_bind_auth_key_inner) ||
		int64(binary.LittleEndian.Uint64(body[4:])) != nonce ||
		int64(binary.LittleEndian.Uint64(body[12:])) != tempKey ||
		int64(binary.LittleEndian.Uint64(body[20:])) != perm.ID ||
		int64(binary.LittleEndian.Uint64(body[28:])) != session ||
		int32(binary.LittleEndian.Uint32(body[36:])) != expiresAt {
		t.Fatal("bind_auth_key_inner fields")
	}
}

func TestIntermediateTransport(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	done := make(chan error, 1)
	var tr Transport
	go func() {
		var err error
		tr, err = NewIntermediate(a)
		done <- err
	}()
	tag := make([]byte, 4)
	if _, err := b.Read(tag); err != nil || binary.LittleEndian.Uint32(tag) != intermediateTag {
		t.Fatalf("tag %x, %v", tag, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	go func() {
		w := func(v ...uint32) {
			for _, x := range v {
				_ = binary.Write(b, binary.LittleEndian, x)
			}
		}
		w(quickAckBit | 0x1234)  // a quick ACK is skipped
		w(8, 1, 2)               // a packet
		w(4, uint32(0xfffffe6c)) // transport error -404
	}()
	p, err := tr.ReadPacket()
	if err != nil || len(p) != 8 || binary.LittleEndian.Uint32(p) != 1 {
		t.Fatalf("packet %x, %v", p, err)
	}
	if _, err = tr.ReadPacket(); !IsTransportError(err, -404) {
		t.Fatalf("got %v, want transport error -404", err)
	}
}

// fakeServer is the server side of a session over a pipe: it decrypts what
// the client sends and encrypts replies with the server half of the key.
type fakeServer struct {
	t       *testing.T
	conn    net.Conn
	ak      *crypto.AuthKey
	session int64
	lastID  int64
	salt    int64
}

type clientMsg struct {
	salt  int64
	msgID int64
	seqno int32
	body  []byte
}

func newSessionPair(t *testing.T) (*Client, *fakeServer) {
	t.Helper()
	a, b := net.Pipe()
	key := &AuthKey{Key: crypto.GenerateNonce(256), ServerSalt: 111}
	key.ID = AuthKeyID(key.Key)

	tr := make(chan Transport, 1)
	go func() {
		x, err := NewIntermediate(a)
		if err != nil {
			t.Error(err)
		}
		tr <- x
	}()
	tag := make([]byte, 4)
	if _, err := b.Read(tag); err != nil {
		t.Fatal(err)
	}
	c := New(<-tr, key, Config{})
	t.Cleanup(func() { _ = c.Close(); _ = b.Close() })
	return c, &fakeServer{t: t, conn: b, ak: crypto.NewAuthKey(key.ID, key.Key), salt: 222}
}

// read returns the next message from the client; a container is returned as
// its elements.
func (s *fakeServer) read() []clientMsg {
	s.t.Helper()
	var n uint32
	if err := binary.Read(s.conn, binary.LittleEndian, &n); err != nil {
		s.t.Fatal(err)
	}
	p := make([]byte, n)
	if _, err := io.ReadFull(s.conn, p); err != nil {
		s.t.Fatal(err)
	}
	plain, err := s.ak.AesIgeDecrypt(p[8:24], p[24:])
	if err != nil {
		s.t.Fatal(err)
	}
	salt := int64(binary.LittleEndian.Uint64(plain))
	s.session = int64(binary.LittleEndian.Uint64(plain[8:]))
	m := clientMsg{
		salt:  salt,
		msgID: int64(binary.LittleEndian.Uint64(plain[16:])),
		seqno: int32(binary.LittleEndian.Uint32(plain[24:])),
		body:  plain[32 : 32+binary.LittleEndian.Uint32(plain[28:])],
	}
	if m.msgID%4 != 0 {
		s.t.Fatalf("client msg_id %d", m.msgID)
	}
	if mtproto.TLConstructor(int32(binary.LittleEndian.Uint32(m.body))) != mtproto.CRC32_msg_container {
		return []clientMsg{m}
	}
	d := mtproto.NewDecodeBuf(m.body[4:])
	var out []clientMsg
	for i, count := int32(0), d.Int(); i < count; i++ {
		im := clientMsg{salt: salt, msgID: d.Long(), seqno: d.Int()}
		im.body = d.Bytes(int(d.Int()))
		if im.msgID >= m.msgID || im.seqno > m.seqno {
			s.t.Fatalf("container %d/%d holds %d/%d", m.msgID, m.seqno, im.msgID, im.seqno)
		}
		out = append(out, im)
	}
	return out
}

// write sends body as a server message with the given seqno and a new
// msg_id.
func (s *fakeServer) write(seqno int32, body []byte) int64 {
	s.t.Helper()
	s.lastID = nextMsgID(s.lastID, 0) | 1
	s.writeID(s.lastID, seqno, body)
	return s.lastID
}

// writeID sends body as a server message with the given msg_id and seqno.
func (s *fakeServer) writeID(msgID int64, seqno int32, body []byte) {
	s.t.Helper()
	plain := make([]byte, 32+len(body))
	binary.LittleEndian.PutUint64(plain, uint64(s.salt))
	binary.LittleEndian.PutUint64(plain[8:], uint64(s.session))
	binary.LittleEndian.PutUint64(plain[16:], uint64(msgID))
	binary.LittleEndian.PutUint32(plain[24:], uint32(seqno))
	binary.LittleEndian.PutUint32(plain[28:], uint32(len(body)))
	copy(plain[32:], body)
	msgKey, enc, err := s.ak.AesIgeEncrypt(plain)
	if err != nil {
		s.t.Fatal(err)
	}
	p := binary.LittleEndian.AppendUint32(nil, uint32(24+len(enc)))
	p = binary.LittleEndian.AppendUint64(p, uint64(AuthKeyID(s.ak.AuthKey())))
	p = append(append(p, msgKey...), enc...)
	if _, err = s.conn.Write(p); err != nil {
		s.t.Fatal(err)
	}
}

func tl(v ...any) []byte {
	x := mtproto.NewEncodeBuf(64)
	for _, e := range v {
		switch e := e.(type) {
		case mtproto.TLConstructor:
			x.Int(int32(e))
		case int32:
			x.Int(e)
		case int64:
			x.Long(e)
		case []byte:
			x.Bytes(e)
		case string:
			x.String(e)
		}
	}
	return x.GetBuf()
}

const (
	crcBoolTrue       = mtproto.CRC32_boolTrue
	crcUpdatesTooLong = mtproto.CRC32_updatesTooLong
)

func gzipPacked(t *testing.T, obj []byte) []byte {
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	_, _ = w.Write(obj)
	_ = w.Close()
	x := mtproto.NewEncodeBuf(64)
	x.Int(int32(mtproto.CRC32_gzip_packed))
	x.StringBytes(b.Bytes())
	return x.GetBuf()
}

func container(msgs ...[]byte) []byte {
	ids := make([]int64, len(msgs))
	for i := range ids {
		ids[i] = int64(i)*4 + 1
	}
	return containerIDs(ids, msgs...)
}

// containerIDs is a msg_container of content messages with the given msg_ids.
func containerIDs(ids []int64, msgs ...[]byte) []byte {
	x := mtproto.NewEncodeBuf(256)
	x.Int(int32(mtproto.CRC32_msg_container))
	x.Int(int32(len(msgs)))
	for i, m := range msgs {
		x.Long(ids[i])
		x.Int(int32(2*i + 1))
		x.Int(int32(len(m)))
		x.Bytes(m)
	}
	return x.GetBuf()
}

// TestSession drives a session against a scripted server: a wrong salt and
// a re-send, a container with new_session_created, a gzip_packed rpc_result
// and a pushed update, acknowledgements, rpc_error and ping.
func TestSession(t *testing.T) {
	c, s := newSessionPair(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	type result struct {
		o   mtproto.TLObject
		err error
	}
	res := make(chan result, 1)
	go func() {
		o, err := c.Invoke(ctx, &mtproto.TLHelpGetConfig{Constructor: mtproto.CRC32_help_getConfig})
		res <- result{o, err}
	}()

	first := s.read()
	if len(first) != 1 || first[0].salt != 111 || first[0].seqno != 1 {
		t.Fatalf("first message %+v", first)
	}
	s.write(0, tl(mtproto.CRC32_bad_server_salt, first[0].msgID, first[0].seqno, int32(48), s.salt))

	again := s.read()
	if len(again) != 1 || again[0].salt != s.salt || again[0].msgID <= first[0].msgID || !bytes.Equal(again[0].body, first[0].body) {
		t.Fatalf("re-sent message %+v", again)
	}
	if again[0].seqno != 3 {
		t.Fatalf("re-sent seqno %d, want 3", again[0].seqno)
	}

	s.write(0, container(
		tl(mtproto.CRC32_new_session_created, again[0].msgID, int64(42), s.salt),
		append(tl(mtproto.CRC32_rpc_result, again[0].msgID), gzipPacked(t, tl(crcBoolTrue))...),
		tl(crcUpdatesTooLong),
	))
	r := <-res
	if r.err != nil {
		t.Fatal(r.err)
	}
	if _, ok := r.o.(*mtproto.TLBoolTrue); !ok {
		t.Fatalf("result %T", r.o)
	}
	u, err := c.WaitUpdate(ctx, func(u *mtproto.Updates) bool { return u.GetPredicateName() == mtproto.Predicate_updatesTooLong })
	if err != nil || u == nil {
		t.Fatalf("pushed update: %v", err)
	}

	// The three content messages of the container are acknowledged on their
	// own after ackDelay.
	acks := s.read()
	if len(acks) != 1 || mtproto.TLConstructor(int32(binary.LittleEndian.Uint32(acks[0].body))) != mtproto.CRC32_msgs_ack || acks[0].seqno%2 != 0 {
		t.Fatalf("expected msgs_ack, got %+v", acks)
	}
	d := mtproto.NewDecodeBuf(acks[0].body[4:])
	if ids := d.VectorLong(); len(ids) != 3 {
		t.Fatalf("acked %v, want the 3 container elements", ids)
	}

	// rpc_error
	go func() {
		o, err := c.Invoke(ctx, &mtproto.TLHelpGetConfig{Constructor: mtproto.CRC32_help_getConfig})
		res <- result{o, err}
	}()
	req := s.read()
	s.write(1, tl(mtproto.CRC32_rpc_result, req[0].msgID, mtproto.CRC32_rpc_error, int32(420), "FLOOD_WAIT_7"))
	if r = <-res; !IsRPCError(r.err, "FLOOD_WAIT_") {
		t.Fatalf("got %v, want FLOOD_WAIT", r.err)
	}

	// ping (the ack of the rpc_error goes with it in a container, or on its
	// own if ackDelay passed first)
	pong := make(chan error, 1)
	go func() { pong <- c.Ping(ctx) }()
	var ping *clientMsg
	for i := 0; i < 3 && ping == nil; i++ {
		for _, m := range s.read() {
			if mtproto.TLConstructor(int32(binary.LittleEndian.Uint32(m.body))) == mtproto.CRC32_ping {
				ping = &m
			}
		}
	}
	if ping == nil {
		t.Fatal("no ping")
	}
	s.write(0, tl(mtproto.CRC32_pong, ping.msgID, int64(binary.LittleEndian.Uint64(ping.body[4:]))))
	if err = <-pong; err != nil {
		t.Fatal(err)
	}
}

func TestSessionStopsOnTransportError(t *testing.T) {
	c, s := newSessionPair(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	errc := make(chan error, 1)
	go func() {
		_, err := c.Invoke(ctx, &mtproto.TLHelpGetConfig{Constructor: mtproto.CRC32_help_getConfig})
		errc <- err
	}()
	s.read()
	_, _ = s.conn.Write(binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint32(nil, 4), uint32(0xfffffe6c)))
	if err := <-errc; !IsTransportError(err, -404) {
		t.Fatalf("got %v, want transport error -404", err)
	}
	if !IsTransportError(c.Err(), -404) {
		t.Fatalf("Err() = %v", c.Err())
	}
	if _, err := c.WaitUpdate(ctx, func(*mtproto.Updates) bool { return true }); !IsTransportError(err, -404) {
		t.Fatalf("WaitUpdate after the error: %v", err)
	}
}

func TestCloseFailsPendingCalls(t *testing.T) {
	c, s := newSessionPair(t)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := c.Invoke(context.Background(), &mtproto.TLHelpGetConfig{Constructor: mtproto.CRC32_help_getConfig})
		if !errors.Is(err, ErrClosed) {
			t.Errorf("pending call: %v, want ErrClosed", err)
		}
	}()
	s.read()
	_ = c.Close()
	wg.Wait()
	if _, err := c.Invoke(context.Background(), &mtproto.TLHelpGetConfig{Constructor: mtproto.CRC32_help_getConfig}); !errors.Is(err, ErrClosed) {
		t.Fatalf("call after Close: %v", err)
	}
}

func TestConstructorAndVector(t *testing.T) {
	if got := Constructor(mtproto.Predicate_messages_sendMessage, 228); uint32(got) != 0xfef48f62 {
		t.Fatalf("messages.sendMessage in layer 228: %#x", uint32(got))
	}
	raw := tl(mtproto.CRC32_vector, int32(2), crcBoolTrue, crcBoolTrue)
	v, err := DecodeVector(raw)
	if err != nil || len(v) != 2 {
		t.Fatalf("DecodeVector: %v, %v", v, err)
	}
	if _, err = DecodeVector(tl(crcBoolTrue)); err == nil {
		t.Fatal("DecodeVector accepted a non-vector")
	}
}

func TestMsgIDWindow(t *testing.T) {
	var w msgIDWindow
	for i := int64(1); i <= receivedWindow; i++ {
		if !w.add(i * 4) {
			t.Fatalf("msg_id %d rejected", i*4)
		}
	}
	if w.add(4) || w.add(receivedWindow*4) {
		t.Fatal("a msg_id in the window was accepted again")
	}
	if w.add(2) {
		t.Fatal("a msg_id below the full window was accepted")
	}
	// Out of order but above the lowest one: accepted, the lowest drops out.
	if !w.add(6) {
		t.Fatal("a new msg_id inside the window's range was rejected")
	}
	if len(w.ids) != receivedWindow || w.ids[0] != 6 {
		t.Fatalf("window holds %d ids from %d", len(w.ids), w.ids[0])
	}
	if w.add(4) || w.add(5) {
		t.Fatal("msg_ids below the window were accepted")
	}

	// Below the lowest one while the window is not full: accepted (the
	// elements of a container have lower msg_ids than the container).
	var v msgIDWindow
	if !v.add(100) || !v.add(96) || v.add(96) {
		t.Fatal("window that is not full")
	}
}

func isUpdatesTooLong(u *mtproto.Updates) bool {
	return u.GetPredicateName() == mtproto.Predicate_updatesTooLong
}

func isUpdateShort(u *mtproto.Updates) bool {
	return u.GetPredicateName() == mtproto.Predicate_updateShort
}

// updateShort is updateShort{update: updateConfig, date}.
func updateShort(date int32) []byte {
	return tl(mtproto.CRC32_updateShort, mtproto.CRC32_updateConfig, date)
}

// TestRepeatedMessagesIgnored: a message the server sends again with the same
// msg_id, alone or inside a container, is acknowledged again but not handled
// twice (here: the update is queued once).
func TestRepeatedMessagesIgnored(t *testing.T) {
	c, s := newSessionPair(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// The server learns the session id from the client's first message.
	go func() { _ = c.Ping(ctx) }()
	s.read()

	id := s.write(1, tl(crcUpdatesTooLong))
	s.writeID(id, 1, tl(crcUpdatesTooLong))
	inner := nextMsgID(id, 0) | 1
	s.write(0, containerIDs([]int64{id, inner}, tl(crcUpdatesTooLong), updateShort(1)))
	// Handled in order: once updateShort is queued, the repeats were seen.
	if _, err := c.WaitUpdate(ctx, isUpdateShort); err != nil {
		t.Fatal(err)
	}
	if _, err := c.WaitUpdate(ctx, isUpdatesTooLong); err != nil {
		t.Fatal(err)
	}
	short, cancelShort := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancelShort()
	if u, err := c.WaitUpdate(short, isUpdatesTooLong); err == nil {
		t.Fatalf("the repeated update was queued again: %v", u)
	}
	if n := c.Duplicates(); n != 2 {
		t.Fatalf("Duplicates() = %d, want 2", n)
	}

	// Every copy of a content message is acknowledged.
	var acked []int64
	for len(acked) < 4 {
		for _, m := range s.read() {
			if mtproto.TLConstructor(int32(binary.LittleEndian.Uint32(m.body))) == mtproto.CRC32_msgs_ack {
				acked = append(acked, mtproto.NewDecodeBuf(m.body[4:]).VectorLong()...)
			}
		}
	}
	slices.Sort(acked)
	if want := []int64{id, id, id, inner}; !slices.Equal(acked, want) {
		t.Fatalf("acknowledged %v, want %v", acked, want)
	}
}

// waitForWaiters returns once n WaitUpdate calls are blocked.
func waitForWaiters(t *testing.T, c *Client, n int) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(time.Millisecond) {
		c.mu.Lock()
		w := c.waiters
		c.mu.Unlock()
		if w == n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d WaitUpdate calls blocked, want %d", w, n)
		}
	}
}

// TestWaitUpdateWakesEveryWaiter: goroutines waiting on one client for
// different updates each get theirs, and all of them learn that the client
// stopped.
func TestWaitUpdateWakesEveryWaiter(t *testing.T) {
	c, s := newSessionPair(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go func() { _ = c.Ping(ctx) }()
	s.read()

	type result struct {
		name string
		err  error
	}
	results := make(chan result, 4)
	wait := func(name string, match func(*mtproto.Updates) bool) {
		_, err := c.WaitUpdate(ctx, match)
		results <- result{name, err}
	}
	go wait("updatesTooLong", isUpdatesTooLong)
	go wait("updateShort", isUpdateShort)
	waitForWaiters(t, c, 2)

	// The update the second waiter wants arrives first.
	s.write(1, updateShort(1))
	if r := <-results; r.name != "updateShort" || r.err != nil {
		t.Fatalf("%s: %v", r.name, r.err)
	}
	s.write(1, tl(crcUpdatesTooLong))
	if r := <-results; r.name != "updatesTooLong" || r.err != nil {
		t.Fatalf("%s: %v", r.name, r.err)
	}

	go wait("first", isUpdatesTooLong)
	go wait("second", isUpdateShort)
	waitForWaiters(t, c, 2)
	_, _ = s.conn.Write(binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint32(nil, 4), uint32(0xfffffe6c)))
	for range 2 {
		if r := <-results; !IsTransportError(r.err, -404) {
			t.Fatalf("%s after the transport error: %v", r.name, r.err)
		}
	}
}
