package mtclient

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"sync"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/crypto"
)

// DefaultLayer is the API layer the server implements (ADR 0008).
const DefaultLayer = 228

const (
	// ackDelay is how long received content messages wait for an outgoing
	// request to carry their msgs_ack in a container before it is sent alone.
	ackDelay = 100 * time.Millisecond
	// maxResends bounds how often one request is re-sent after
	// bad_server_salt or bad_msg_notification.
	maxResends = 5
	// maxUnpackedSize bounds what gzip_packed may unpack to.
	maxUnpackedSize = 16 << 20
	// receivedWindow is how many server msg_ids the client remembers to
	// recognise repeated messages (as tdesktop does, with the same size).
	receivedWindow = 400
)

// Config describes the client and how it connects.
type Config struct {
	// Addr is host:port of an MTProto TCP port of the server.
	Addr string
	// PublicKey is the server RSA public key (tools/genserverkey).
	PublicKey *rsa.PublicKey
	// Layer is the API layer of invokeWithLayer (default DefaultLayer).
	Layer int32
	// DC is sent in the key exchange (default 2).
	DC int32

	// initConnection parameters; empty ones get defaults describing this
	// client.
	APIID          int32
	DeviceModel    string
	SystemVersion  string
	AppVersion     string
	SystemLangCode string
	LangPack       string
	LangCode       string

	// Logf, if set, receives a line for every message sent and received:
	// TL types and message ids only, never content.
	Logf func(format string, args ...any)
}

func (cfg *Config) setDefaults() {
	if cfg.Layer == 0 {
		cfg.Layer = DefaultLayer
	}
	if cfg.APIID == 0 {
		cfg.APIID = 1
	}
	if cfg.DeviceModel == "" {
		cfg.DeviceModel = "mtclient"
	}
	if cfg.SystemVersion == "" {
		cfg.SystemVersion = "go"
	}
	if cfg.AppVersion == "" {
		cfg.AppVersion = "1.0"
	}
	if cfg.SystemLangCode == "" {
		cfg.SystemLangCode = "en"
	}
	if cfg.LangCode == "" {
		cfg.LangCode = "en"
	}
}

// Client is an MTProto 2.0 session over one connection: it encrypts and
// numbers messages, unpacks containers and gzip_packed, acknowledges content
// messages, ignores messages the server repeats, re-sends requests after
// bad_server_salt and time-related bad_msg_notification, matches rpc_result
// to requests and queues the updates the server pushes.
//
// It is meant for tests: the conformance and end-to-end tests drive the
// server with it the way the official clients do.
type Client struct {
	cfg       Config
	t         Transport
	key       *AuthKey
	ak        *crypto.AuthKey
	sessionID int64

	mu         sync.Mutex
	salt       int64
	timeOffset time.Duration
	lastMsgID  int64
	contentSeq int32
	calls      map[int64]*call   // by the msg_id the request was last sent with
	containers map[int64][]int64 // container msg_id -> request msg_ids in it
	acks       []int64
	ackTimer   *time.Timer
	received   msgIDWindow // msg_ids of the messages received last
	duplicates int         // messages ignored as repeated (Duplicates)
	updates    []*mtproto.Updates
	updated    chan struct{} // closed and replaced when updates are queued or the client stops
	waiters    int           // WaitUpdate calls blocked on updated (for tests)
	err        error         // why the client stopped
	closing    bool

	done chan struct{} // closed when the read loop stops
}

type call struct {
	// encode returns the request body for the msg_id it is sent with (the
	// binding message of auth.bindTempAuthKey depends on it).
	encode  func(msgID int64) ([]byte, error)
	name    string
	msgID   int64
	resends int
	done    chan struct{}
	result  []byte
	err     error
}

// Dial connects to cfg.Addr, creates a new permanent auth key, starts a
// session and sends initConnection: the client is ready for requests.
func Dial(ctx context.Context, cfg Config) (*Client, error) {
	c, err := DialNewKey(ctx, cfg, KeyOptions{DC: cfg.DC})
	if err != nil {
		return nil, err
	}
	if _, err = c.InitConnection(ctx); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

// DialNewKey connects to cfg.Addr, creates a key with opts (a temporary one
// for PFS) and starts a session on the same connection. It does not send
// initConnection.
func DialNewKey(ctx context.Context, cfg Config, opts KeyOptions) (*Client, error) {
	if cfg.PublicKey == nil {
		return nil, errors.New("mtclient: Config.PublicKey is required")
	}
	t, err := DialIntermediate(ctx, cfg.Addr)
	if err != nil {
		return nil, err
	}
	key, err := CreateAuthKey(ctx, t, cfg.PublicKey, opts)
	if err != nil {
		_ = t.Close()
		return nil, err
	}
	return New(t, key, cfg), nil
}

// DialKey connects to cfg.Addr and starts a new session with an existing key
// (e.g. a second connection of the same device). It does not send
// initConnection.
func DialKey(ctx context.Context, cfg Config, key *AuthKey) (*Client, error) {
	t, err := DialIntermediate(ctx, cfg.Addr)
	if err != nil {
		return nil, err
	}
	return New(t, key, cfg), nil
}

// New starts a session with key over t, which the client owns from now on.
func New(t Transport, key *AuthKey, cfg Config) *Client {
	cfg.setDefaults()
	var sid [8]byte
	if _, err := rand.Read(sid[:]); err != nil {
		panic(fmt.Sprintf("mtclient: crypto/rand: %v", err))
	}
	c := &Client{
		cfg:        cfg,
		t:          t,
		key:        key,
		ak:         crypto.NewClientAuthKey(key.ID, key.Key),
		sessionID:  int64(binary.LittleEndian.Uint64(sid[:]) | 1),
		salt:       key.ServerSalt,
		timeOffset: key.TimeOffset,
		calls:      map[int64]*call{},
		containers: map[int64][]int64{},
		updated:    make(chan struct{}),
		done:       make(chan struct{}),
	}
	go c.readLoop()
	return c
}

// AuthKey returns the key of the session.
func (c *Client) AuthKey() *AuthKey { return c.key }

// SessionID returns the session_id of the session.
func (c *Client) SessionID() int64 { return c.sessionID }

// Layer returns the API layer the client encodes requests for.
func (c *Client) Layer() int32 { return c.cfg.Layer }

// Close closes the connection; pending calls fail with ErrClosed.
func (c *Client) Close() error {
	c.mu.Lock()
	c.closing = true
	c.mu.Unlock()
	err := c.t.Close()
	<-c.done
	return err
}

// Duplicates returns how many messages the server sent with a msg_id the
// client had already received, or with one lower than every msg_id it still
// remembers: they are acknowledged again and otherwise ignored, so a repeated
// update is never queued twice. A server repeats a message only when it got
// no acknowledgement for it in time, which in a short exchange with this
// client (acks go out within ackDelay) should not happen: tests can expect 0.
func (c *Client) Duplicates() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.duplicates
}

// Err returns why the client stopped, or nil while it runs.
func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// InitConnection sends invokeWithLayer(layer, initConnection(..., help.getConfig))
// as clients do first on every connection, and returns the config.
func (c *Client) InitConnection(ctx context.Context) (*mtproto.Config, error) {
	query, err := encodeObject(&mtproto.TLHelpGetConfig{Constructor: mtproto.CRC32_help_getConfig}, c.cfg.Layer)
	if err != nil {
		return nil, err
	}
	o, err := c.Invoke(ctx, c.WrapInit(query))
	if err != nil {
		return nil, err
	}
	cfg, ok := o.(interface{ To_Config() *mtproto.Config })
	if !ok {
		return nil, fmt.Errorf("mtclient: help.getConfig returned %T", o)
	}
	return cfg.To_Config(), nil
}

// WrapInit wraps an encoded query in invokeWithLayer(initConnection(query)).
func (c *Client) WrapInit(query []byte) mtproto.TLObject {
	init := &mtproto.TLInitConnection{
		Constructor:    mtproto.CRC32_initConnection_c1cd5ea9, // the current constructor (layer 228)
		ApiId:          c.cfg.APIID,
		DeviceModel:    c.cfg.DeviceModel,
		SystemVersion:  c.cfg.SystemVersion,
		AppVersion:     c.cfg.AppVersion,
		SystemLangCode: c.cfg.SystemLangCode,
		LangPack:       c.cfg.LangPack,
		LangCode:       c.cfg.LangCode,
		Query:          query,
	}
	b, err := encodeObject(init, c.cfg.Layer)
	if err != nil {
		// initConnection always has a constructor.
		panic(err)
	}
	return &mtproto.TLInvokeWithLayer{Constructor: mtproto.CRC32_invokeWithLayer, Layer: c.cfg.Layer, Query: b}
}

// Invoke sends the request req and returns the result object; an rpc_error
// answer is returned as *RPCError. Results that are not TL objects (vectors)
// need InvokeRaw.
func (c *Client) Invoke(ctx context.Context, req mtproto.TLObject) (mtproto.TLObject, error) {
	raw, err := c.InvokeRaw(ctx, req)
	if err != nil {
		return nil, err
	}
	d := mtproto.NewDecodeBuf(raw)
	o := d.Object()
	if err = d.GetError(); err != nil {
		return nil, fmt.Errorf("mtclient: decode result of %T: %w", req, err)
	}
	return o, nil
}

// InvokeRaw sends the request req and returns the serialized result (after
// unpacking gzip_packed).
func (c *Client) InvokeRaw(ctx context.Context, req mtproto.TLObject) ([]byte, error) {
	body, err := encodeObject(req, c.cfg.Layer)
	if err != nil {
		return nil, err
	}
	return c.do(ctx, typeName(req), func(int64) ([]byte, error) { return body, nil })
}

// Ping sends ping and waits for the pong.
func (c *Client) Ping(ctx context.Context) error {
	var id [8]byte
	_, _ = rand.Read(id[:])
	body, err := encodeObject(&mtproto.TLPing{Constructor: mtproto.CRC32_ping, PingId: int64(binary.LittleEndian.Uint64(id[:]))}, c.cfg.Layer)
	if err != nil {
		return err
	}
	_, err = c.do(ctx, "ping", func(int64) ([]byte, error) { return body, nil })
	return err
}

func (c *Client) do(ctx context.Context, name string, encode func(msgID int64) ([]byte, error)) ([]byte, error) {
	cl := &call{encode: encode, name: name, done: make(chan struct{})}
	c.mu.Lock()
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		return nil, err
	}
	err := c.sendCallLocked(cl)
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}

	select {
	case <-cl.done:
		return cl.result, cl.err
	case <-ctx.Done():
		c.mu.Lock()
		if c.calls[cl.msgID] == cl {
			delete(c.calls, cl.msgID)
		}
		c.mu.Unlock()
		return nil, fmt.Errorf("mtclient: %s: %w", name, ctx.Err())
	}
}

// sendCallLocked sends cl with a new msg_id, together with the pending acks.
func (c *Client) sendCallLocked(cl *call) error {
	msgID := c.newMsgIDLocked()
	body, err := cl.encode(msgID)
	if err != nil {
		return err
	}
	cl.msgID = msgID
	c.calls[msgID] = cl
	msgs := []outMsg{{id: msgID, seqno: c.seqNoLocked(true), body: body, name: cl.name}}
	if len(c.acks) > 0 {
		msgs = append(msgs, c.ackMsgLocked())
	}
	if err = c.writeLocked(msgs); err != nil {
		delete(c.calls, msgID)
		return err
	}
	return nil
}

type outMsg struct {
	id    int64
	seqno int32
	body  []byte
	name  string
}

// ackMsgLocked takes the pending acks into a msgs_ack message.
func (c *Client) ackMsgLocked() outMsg {
	ids := c.acks
	c.acks = nil
	if c.ackTimer != nil {
		c.ackTimer.Stop()
		c.ackTimer = nil
	}
	x := mtproto.NewEncodeBuf(16 + 8*len(ids))
	x.Int(int32(mtproto.CRC32_msgs_ack))
	x.VectorLong(ids)
	return outMsg{id: c.newMsgIDLocked(), seqno: c.seqNoLocked(false), body: x.GetBuf(), name: "msgs_ack"}
}

// writeLocked encrypts and sends msgs: one message as is, several in a
// msg_container whose msg_id is greater than theirs.
func (c *Client) writeLocked(msgs []outMsg) error {
	m := msgs[0]
	if len(msgs) > 1 {
		x := mtproto.NewEncodeBuf(512)
		x.Int(int32(mtproto.CRC32_msg_container))
		x.Int(int32(len(msgs)))
		inner := make([]int64, 0, len(msgs))
		for _, im := range msgs {
			x.Long(im.id)
			x.Int(im.seqno)
			x.Int(int32(len(im.body)))
			x.Bytes(im.body)
			inner = append(inner, im.id)
			c.logf("-> %s msg_id=%d seqno=%d (in container)", im.name, im.id, im.seqno)
		}
		m = outMsg{id: c.newMsgIDLocked(), seqno: c.seqNoLocked(false), body: x.GetBuf(), name: "msg_container"}
		c.containers[m.id] = inner
	}
	c.logf("-> %s msg_id=%d seqno=%d", m.name, m.id, m.seqno)

	plain := make([]byte, 32+len(m.body))
	binary.LittleEndian.PutUint64(plain[0:], uint64(c.salt))
	binary.LittleEndian.PutUint64(plain[8:], uint64(c.sessionID))
	binary.LittleEndian.PutUint64(plain[16:], uint64(m.id))
	binary.LittleEndian.PutUint32(plain[24:], uint32(m.seqno))
	binary.LittleEndian.PutUint32(plain[28:], uint32(len(m.body)))
	copy(plain[32:], m.body)
	msgKey, encrypted, err := c.ak.AesIgeEncrypt(plain)
	if err != nil {
		return err
	}
	p := make([]byte, 0, 24+len(encrypted))
	p = binary.LittleEndian.AppendUint64(p, uint64(c.key.ID))
	p = append(p, msgKey...)
	p = append(p, encrypted...)
	return c.t.WritePacket(p)
}

func (c *Client) newMsgIDLocked() int64 {
	c.lastMsgID = nextMsgID(c.lastMsgID, c.timeOffset)
	return c.lastMsgID
}

// seqNoLocked returns the msg_seqno of the next message: twice the number of
// content-related messages sent before it, plus one if it is one itself.
func (c *Client) seqNoLocked(content bool) int32 {
	if !content {
		return c.contentSeq * 2
	}
	c.contentSeq++
	return c.contentSeq*2 - 1
}

func (c *Client) logf(format string, args ...any) {
	if c.cfg.Logf != nil {
		c.cfg.Logf("[%x] "+format, append([]any{uint64(c.key.ID) >> 48}, args...)...)
	}
}

func (c *Client) readLoop() {
	defer close(c.done)
	for {
		p, err := c.t.ReadPacket()
		if err != nil {
			c.stop(err)
			return
		}
		if err = c.handlePacket(p); err != nil {
			c.stop(err)
			_ = c.t.Close()
			return
		}
	}
}

// stop fails every pending call and stops the client.
func (c *Client) stop(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closing {
		err = ErrClosed
	}
	if c.err == nil {
		c.err = err
	}
	for id, cl := range c.calls {
		delete(c.calls, id)
		cl.err = c.err
		close(cl.done)
	}
	if c.ackTimer != nil {
		c.ackTimer.Stop()
	}
	c.notifyLocked()
}

// notifyLocked wakes every WaitUpdate call.
func (c *Client) notifyLocked() {
	close(c.updated)
	c.updated = make(chan struct{})
}

func (c *Client) handlePacket(p []byte) error {
	if len(p) < 24 {
		return fmt.Errorf("mtclient: packet of %d bytes", len(p))
	}
	if keyID := int64(binary.LittleEndian.Uint64(p)); keyID != c.key.ID {
		return fmt.Errorf("mtclient: message for auth_key_id %d", keyID)
	}
	plain, err := c.ak.AesIgeDecrypt(p[8:24], p[24:])
	if err != nil {
		return err
	}
	if sid := int64(binary.LittleEndian.Uint64(plain[8:])); sid != c.sessionID {
		return fmt.Errorf("mtclient: message for session %d", sid)
	}
	msgID := int64(binary.LittleEndian.Uint64(plain[16:]))
	seqno := int32(binary.LittleEndian.Uint32(plain[24:]))
	n := binary.LittleEndian.Uint32(plain[28:])

	c.mu.Lock()
	defer c.mu.Unlock()
	return c.handleMessageLocked(msgID, seqno, plain[32:32+n])
}

// handleMessageLocked processes one message (a container element or a whole
// message). A message whose msg_id was received before is acknowledged again
// (the server repeats what it has no acknowledgement for) and ignored.
func (c *Client) handleMessageLocked(msgID int64, seqno int32, body []byte) error {
	if msgID%4 != 1 && msgID%4 != 3 {
		return fmt.Errorf("mtclient: server msg_id %d", msgID)
	}
	if seqno%2 == 1 {
		c.acks = append(c.acks, msgID)
		if c.ackTimer == nil {
			c.ackTimer = time.AfterFunc(ackDelay, c.flushAcks)
		}
	}
	if !c.received.add(msgID) {
		c.duplicates++
		c.logf("<- msg_id=%d received before or too old: ignored", msgID)
		return nil
	}
	return c.handleBodyLocked(msgID, body)
}

// msgIDWindow holds the msg_ids of the last receivedWindow messages received.
// A message is new if its msg_id is not there and, once the window is full,
// greater than the lowest one in it (MTProto: the client ignores a message
// whose msg_id is lower than all remembered ones or equal to one of them).
type msgIDWindow struct {
	ids []int64 // ascending
}

// add records id and reports whether it is new.
func (w *msgIDWindow) add(id int64) bool {
	i, found := slices.BinarySearch(w.ids, id)
	if found || (i == 0 && len(w.ids) >= receivedWindow) {
		return false
	}
	w.ids = slices.Insert(w.ids, i, id)
	if len(w.ids) > receivedWindow {
		w.ids = slices.Delete(w.ids, 0, 1)
	}
	return true
}

func (c *Client) handleBodyLocked(msgID int64, body []byte) error {
	if len(body) < 4 {
		return errors.New("mtclient: empty message")
	}
	switch mtproto.TLConstructor(int32(binary.LittleEndian.Uint32(body))) {
	case mtproto.CRC32_msg_container:
		c.logf("<- msg_container msg_id=%d", msgID)
		d := mtproto.NewDecodeBuf(body[4:])
		count := d.Int()
		for i := int32(0); i < count && d.GetError() == nil; i++ {
			id := d.Long()
			seq := d.Int()
			size := d.Int()
			b := d.Bytes(int(size))
			if d.GetError() != nil {
				break
			}
			if err := c.handleMessageLocked(id, seq, b); err != nil {
				return err
			}
		}
		return d.GetError()

	case mtproto.CRC32_gzip_packed:
		b, err := gunzipObject(body)
		if err != nil {
			return err
		}
		return c.handleBodyLocked(msgID, b)

	case mtproto.CRC32_rpc_result:
		if len(body) < 16 {
			return errors.New("mtclient: short rpc_result")
		}
		reqID := int64(binary.LittleEndian.Uint64(body[4:]))
		result := body[12:]
		if mtproto.TLConstructor(int32(binary.LittleEndian.Uint32(result))) == mtproto.CRC32_gzip_packed {
			var err error
			if result, err = gunzipObject(result); err != nil {
				return err
			}
		}
		c.resolveLocked(reqID, result)
		return nil
	}

	d := mtproto.NewDecodeBuf(body)
	o := d.Object()
	if err := d.GetError(); err != nil {
		// A message this client cannot decode (e.g. a newer constructor):
		// skip it rather than drop the session.
		c.logf("<- undecodable message msg_id=%d: %v", msgID, err)
		return nil
	}
	c.logf("<- %s msg_id=%d", typeName(o), msgID)

	switch m := o.(type) {
	case *mtproto.TLNewSessionCreated:
		c.salt = m.GetServerSalt()
	case *mtproto.TLBadServerSalt:
		c.salt = m.GetNewServerSalt()
		c.resendLocked(m.GetBadMsgId(), nil)
	case *mtproto.TLBadMsgNotification:
		switch code := m.GetErrorCode(); code {
		case 16, 17:
			// msg_id too low or too high: take the server time from its
			// msg_id and send again.
			c.timeOffset = time.Duration(msgID>>32-time.Now().Unix()) * time.Second
			c.resendLocked(m.GetBadMsgId(), nil)
		case 32, 33:
			// msg_seqno too low or too high: send again with the next one.
			c.resendLocked(m.GetBadMsgId(), nil)
		default:
			c.resendLocked(m.GetBadMsgId(), &BadMsgError{Code: code})
		}
	case *mtproto.TLPong:
		c.resolveLocked(m.GetMsgId(), body)
	case interface{ To_Updates() *mtproto.Updates }:
		c.updates = append(c.updates, m.To_Updates())
		c.notifyLocked()
	default:
		// msgs_ack, msg_detailed_info, future_salts, ...: nothing to do.
	}
	return nil
}

// resendLocked sends the requests of the message badID (a request or a
// container) again, or fails them with failErr.
func (c *Client) resendLocked(badID int64, failErr error) {
	ids := []int64{badID}
	if inner, ok := c.containers[badID]; ok {
		delete(c.containers, badID)
		ids = inner
	}
	for _, id := range ids {
		cl := c.calls[id]
		if cl == nil {
			continue // an ack, or an answered request
		}
		delete(c.calls, id)
		cl.resends++
		err := failErr
		if err == nil && cl.resends > maxResends {
			err = fmt.Errorf("mtclient: %s re-sent %d times", cl.name, maxResends)
		}
		if err == nil {
			err = c.sendCallLocked(cl)
		}
		if err != nil {
			cl.err = err
			close(cl.done)
		}
	}
}

// resolveLocked completes the request reqID with its serialized result.
func (c *Client) resolveLocked(reqID int64, result []byte) {
	cl := c.calls[reqID]
	if cl == nil {
		c.logf("<- result for unknown msg_id=%d", reqID)
		return
	}
	delete(c.calls, reqID)
	for id, inner := range c.containers {
		for _, x := range inner {
			if x == reqID {
				delete(c.containers, id)
				break
			}
		}
	}
	if len(result) >= 4 && mtproto.TLConstructor(int32(binary.LittleEndian.Uint32(result))) == mtproto.CRC32_rpc_error {
		d := mtproto.NewDecodeBuf(result)
		if e, ok := d.Object().(*mtproto.TLRpcError); ok && d.GetError() == nil {
			cl.err = &RPCError{Code: e.GetErrorCode(), Message: e.GetErrorMessage()}
		} else {
			cl.err = fmt.Errorf("mtclient: undecodable rpc_error: %v", d.GetError())
		}
		c.logf("<- rpc_error for %s: %v", cl.name, cl.err)
	} else {
		cl.result = result
	}
	close(cl.done)
}

// flushAcks sends the pending acks on their own when no request took them.
func (c *Client) flushAcks() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ackTimer = nil
	if len(c.acks) == 0 || c.err != nil {
		return
	}
	if err := c.writeLocked([]outMsg{c.ackMsgLocked()}); err != nil {
		c.logf("send msgs_ack: %v", err)
	}
}

// WaitUpdate waits for a pushed update for which match returns true, removes
// it from the queue and returns it. Updates already received count. Several
// goroutines may wait at once, each for its own updates: every new update
// wakes all of them.
func (c *Client) WaitUpdate(ctx context.Context, match func(*mtproto.Updates) bool) (*mtproto.Updates, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for {
		for i, u := range c.updates {
			if match(u) {
				c.updates = append(c.updates[:i], c.updates[i+1:]...)
				return u, nil
			}
		}
		if c.err != nil {
			return nil, c.err
		}
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("mtclient: waiting for an update: %w", err)
		}
		updated := c.updated
		c.waiters++
		c.mu.Unlock()
		select {
		case <-updated:
		case <-ctx.Done():
		}
		c.mu.Lock()
		c.waiters--
	}
}

// gunzipObject unpacks gzip_packed#3072cfa1 packed_data:bytes.
func gunzipObject(b []byte) ([]byte, error) {
	d := mtproto.NewDecodeBuf(b[4:])
	packed := d.StringBytes()
	if err := d.GetError(); err != nil {
		return nil, fmt.Errorf("mtclient: gzip_packed: %w", err)
	}
	r, err := gzip.NewReader(bytes.NewReader(packed))
	if err != nil {
		return nil, fmt.Errorf("mtclient: gzip_packed: %w", err)
	}
	out, err := io.ReadAll(io.LimitReader(r, maxUnpackedSize+1))
	if err != nil {
		return nil, fmt.Errorf("mtclient: gzip_packed: %w", err)
	}
	if len(out) > maxUnpackedSize {
		return nil, errors.New("mtclient: gzip_packed: unpacked data too large")
	}
	if len(out) < 4 {
		return nil, errors.New("mtclient: gzip_packed: empty object")
	}
	return out, nil
}

func typeName(o any) string {
	if o == nil {
		return "nil"
	}
	return reflect.TypeOf(o).String()
}
