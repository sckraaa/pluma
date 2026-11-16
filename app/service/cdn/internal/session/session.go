// Copyright 2026 The Pluma Authors
//  All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package session is the encrypted layer of a CDN node (ADR 0065 §5): the
// smallest MTProto 2.0 session the official clients download through. It
// checks msg_id and the server salt, unpacks containers and gzip_packed,
// answers the service messages (ping, ping_delay_disconnect,
// get_future_salts, destroy_session, msgs_state_req, rpc_drop_answer) and
// serves one method, upload.getCdnFile, inside invokeWithLayer and
// initConnection; anything else is rpc_error 400 CDN_METHOD_INVALID. The
// node sends no updates and keeps no answers: a request whose answer is
// lost is sent again by the client and served again.
package session

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/zeromicro/go-zero/core/logx"

	"github.com/teamgram/teamgram-server/app/service/cdn/internal/metrics"
	"github.com/teamgram/teamgram-server/app/service/cdn/internal/store"
	"github.com/teamgram/teamgram-server/pkg/cdn"
)

// Files is where the node reads the cipher of files (store.Store).
type Files interface {
	ReadAt(token []byte, offset int64, limit int) (b []byte, size int64, readErr bool, err error)
}

// Conn is the connection a message arrived on.
type Conn interface {
	// Send queues an encrypted message for the client and returns once it
	// is written; an error means the connection is closing.
	Send(packet []byte) error
	// SendQuickAck queues the quick ACK of a message.
	SendQuickAck(token uint32) error
	// Go runs f off the read loop, at most a few at once: it waits for a
	// free slot. It returns false (f does not run) when the connection
	// closes first.
	Go(f func()) bool
	// DelayDisconnect closes the connection d from now unless called again
	// (ping_delay_disconnect).
	DelayDisconnect(d time.Duration)
}

// Server is the encrypted layer of the node. It is safe for concurrent use.
type Server struct {
	keys   *KeyStore
	files  Files
	tokens *RequestTokens
	now    func() time.Time
	salts  salts
	ids    msgIDs
}

// New returns the layer over keys, files and the request_token key; now is
// time.Now when nil.
func New(keys *KeyStore, files Files, tokens *RequestTokens, now func() time.Time) (*Server, error) {
	if now == nil {
		now = time.Now
	}
	s := &Server{keys: keys, files: files, tokens: tokens, now: now}
	if _, err := rand.Read(s.salts.secret[:]); err != nil {
		return nil, fmt.Errorf("session: salt secret: %w", err)
	}
	return s, nil
}

// Keys returns the key store.
func (s *Server) Keys() *KeyStore { return s.keys }

var (
	// ErrUnknownKey: the node does not hold the auth key; the transport
	// answers -404.
	ErrUnknownKey = errors.New("session: unknown auth key")
	// errMessage: a message that does not decrypt or is malformed; the
	// connection is closed.
	errMessage = errors.New("session: malformed encrypted message")
)

// encryptedFrameMinSize is auth_key_id, msg_key and the smallest encrypted
// data: a 32-byte header and 12 bytes of padding, in 16-byte blocks.
const encryptedFrameMinSize = 8 + 16 + 48

// maxGzipped bounds what one gzip_packed may unpack to: the requests the
// node serves are a few dozen bytes.
const maxGzipped = 64 << 10

// maxWrapping bounds invokeWithLayer, initConnection and the other
// wrappers around one request.
const maxWrapping = 8

// schemaLayer is the layer of the server's TL schema
// (third_party/teamgram-proto, v0.233.x). Every answer of the node is
// encoded in it whatever layer the client names: upload.cdnFile,
// upload.cdnFileReuploadNeeded and rpc_error have had the same constructors
// for every layer the clients speak, and the generated encoders know no
// layer newer than the schema's.
const schemaLayer = 233

// Key returns the key of an encrypted frame, or nil with ErrUnknownKey.
func (s *Server) Key(frame []byte) (*AuthKey, error) {
	if len(frame) < encryptedFrameMinSize {
		return nil, errMessage
	}
	k := s.keys.Get(int64(binary.LittleEndian.Uint64(frame)))
	if k == nil {
		return nil, ErrUnknownKey
	}
	return k, nil
}

// in is a message being processed.
type in struct {
	c         Conn
	key       *AuthKey
	sess      *sessionState
	sessionID int64
	now       time.Time
	acks      []int64 // content messages answered by nothing else
}

// Handle processes an encrypted frame of key k (from Key) that arrived on
// c; quickAck asks for its quick ACK. An error means the connection must be
// closed.
func (s *Server) Handle(c Conn, k *AuthKey, frame []byte, quickAck bool) error {
	data := frame[24:]
	// Bytes after the last whole block are the padding of the padded
	// intermediate transport, not encrypted data.
	plain, err := k.ak.AesIgeDecrypt(frame[8:24], data[:len(data)-len(data)%16])
	if err != nil {
		return errMessage
	}
	if quickAck {
		if err := c.SendQuickAck(quickAckToken(k.ak.AuthKey(), plain)); err != nil {
			return err
		}
	}

	salt := int64(binary.LittleEndian.Uint64(plain))
	sessionID := int64(binary.LittleEndian.Uint64(plain[8:]))
	msgID := int64(binary.LittleEndian.Uint64(plain[16:]))
	seqNo := int32(binary.LittleEndian.Uint32(plain[24:]))
	n := binary.LittleEndian.Uint32(plain[28:]) // checked by AesIgeDecrypt
	body := plain[32 : 32+n]
	if len(body) < 4 {
		return errMessage
	}

	now := s.now()
	m := &in{c: c, key: k, sessionID: sessionID, now: now}
	// A message the node refuses is answered without starting a session.
	if code := checkMsgID(msgID, now); code != 0 {
		return s.badMsg(m, msgID, seqNo, code)
	}
	if !s.salts.valid(k, salt, now) {
		return s.send(m, mtproto.MakeTLBadServerSalt(&mtproto.BadMsgNotification{
			BadMsgId:      msgID,
			BadMsgSeqno:   seqNo,
			ErrorCode:     48,
			NewServerSalt: s.salts.current(k, now),
		}), true, false)
	}

	sess, created := k.session(sessionID)
	m.sess = sess
	if created {
		var unique [8]byte
		if _, err := rand.Read(unique[:]); err != nil {
			return fmt.Errorf("session: %w", err)
		}
		if err := s.send(m, mtproto.MakeTLNewSessionCreated(&mtproto.NewSession{
			FirstMsgId: firstMsgID(msgID, body),
			UniqueId:   int64(binary.LittleEndian.Uint64(unique[:])),
			ServerSalt: s.salts.current(k, now),
		}), false, true); err != nil {
			return err
		}
	}

	if constructorOf(body) == mtproto.CRC32_msg_container {
		err = s.container(m, msgID, body)
	} else {
		err = s.message(m, msgID, seqNo, body)
	}
	if err != nil {
		return err
	}
	if len(m.acks) > 0 {
		return s.send(m, mtproto.MakeTLMsgsAck(&mtproto.MsgsAck{MsgIds: m.acks}), true, false)
	}
	return nil
}

// checkMsgID returns the bad_msg_notification code of a client msg_id, or
// 0: 18 when it is not divisible by 4, 16 or 17 when it is too far in the
// past or the future of the node's clock.
func checkMsgID(msgID int64, now time.Time) int32 {
	switch t := msgID >> 32; {
	case msgID&3 != 0:
		return 18
	case t < now.Unix()-msgIDPast:
		return 16
	case t > now.Unix()+msgIDFuture:
		return 17
	}
	return 0
}

// firstMsgID is the first_msg_id of new_session_created for the message
// msgID with body: the smallest msg_id it carries, which for a container is
// one of its inner messages (the container's own id is the largest). The
// clients resend every request they sent below first_msg_id (tdesktop
// session_private.cpp:1945-1961, Android ConnectionsManager.cpp:1128-1137),
// so the container's id would make them send its requests twice. The home's
// session does the same (app/interface/session sess/session.go).
func firstMsgID(msgID int64, body []byte) int64 {
	if constructorOf(body) != mtproto.CRC32_msg_container {
		return msgID
	}
	first := msgID
	d := mtproto.NewDecodeBuf(body[4:])
	count := d.Int()
	for i := int32(0); i < count && d.GetError() == nil; i++ {
		id := d.Long()
		d.Int() // seqno
		size := d.Int()
		if d.GetError() != nil || size < 0 {
			break
		}
		d.Bytes(int(size))
		if d.GetError() != nil {
			break
		}
		first = min(first, id)
	}
	return first
}

func (s *Server) badMsg(m *in, msgID int64, seqNo, code int32) error {
	return s.send(m, mtproto.MakeTLBadMsgNotification(&mtproto.BadMsgNotification{
		BadMsgId:    msgID,
		BadMsgSeqno: seqNo,
		ErrorCode:   code,
	}), true, false)
}

func constructorOf(b []byte) mtproto.TLConstructor {
	return mtproto.TLConstructor(int32(binary.LittleEndian.Uint32(b)))
}

// container processes the messages of a msg_container
// (https://core.telegram.org/mtproto/service_messages#containers); a
// container inside one is malformed.
func (s *Server) container(m *in, containerID int64, body []byte) error {
	d := mtproto.NewDecodeBuf(body[4:])
	count := d.Int()
	if d.GetError() != nil || count < 0 || count > 1024 {
		return errMessage
	}
	for i := int32(0); i < count; i++ {
		msgID := d.Long()
		seqNo := d.Int()
		size := d.Int()
		if d.GetError() != nil || size < 4 || size%4 != 0 {
			return errMessage
		}
		b := d.Bytes(int(size))
		if d.GetError() != nil {
			return errMessage
		}
		if constructorOf(b) == mtproto.CRC32_msg_container {
			return errMessage
		}
		if code := checkMsgID(msgID, m.now); code != 0 {
			if err := s.badMsg(m, msgID, seqNo, code); err != nil {
				return err
			}
			continue
		}
		if msgID == containerID {
			// 19: an inner message with the msg_id of its container.
			if err := s.badMsg(m, msgID, seqNo, 19); err != nil {
				return err
			}
			continue
		}
		if err := s.message(m, msgID, seqNo, b); err != nil {
			return err
		}
	}
	return nil
}

// message processes one message (not a container).
func (s *Server) message(m *in, msgID int64, seqNo int32, body []byte) error {
	old, _, running := m.sess.receive(msgID)
	switch {
	case old:
		return s.badMsg(m, msgID, seqNo, 20)
	case running:
		// Sent again while it is served: the answer is on its way.
		return nil
	}
	// A message sent again after it was answered is served again: the
	// answer may have been lost with a connection, and the node keeps none.
	// Every request of the node only reads.
	return s.body(m, msgID, body, 0)
}

// body processes the body of message msgID.
func (s *Server) body(m *in, msgID int64, body []byte, depth int) error {
	if len(body) < 4 {
		return errMessage
	}
	switch constructorOf(body) {
	case mtproto.CRC32_gzip_packed:
		if depth > 0 {
			return errMessage
		}
		b, err := gunzip(body)
		if err != nil {
			return errMessage
		}
		return s.body(m, msgID, b, depth+1)

	case mtproto.CRC32_msgs_ack,
		mtproto.CRC32_msgs_state_info,
		mtproto.CRC32_msgs_all_info,
		mtproto.CRC32_msg_detailed_info,
		mtproto.CRC32_msg_new_detailed_info,
		mtproto.CRC32_http_wait:
		// Not content-related, nothing to answer; the node keeps no sent
		// messages to resend, so acknowledgements change nothing.
		return nil

	case mtproto.CRC32_msg_resend_req:
		// The node keeps no sent messages; the client asks again for what it
		// misses. Acknowledged, since it is content-related.
		m.acks = append(m.acks, msgID)
		return nil
	}

	d := mtproto.NewDecodeBuf(body)
	obj := d.Object()
	if d.GetError() != nil {
		obj = nil
	}
	switch req := obj.(type) {
	case *mtproto.TLPing:
		return s.send(m, mtproto.MakeTLPong(&mtproto.Pong{MsgId: msgID, PingId: req.GetPingId()}), true, true)

	case *mtproto.TLPingDelayDisconnect:
		// tdesktop and Android keep idle connections with it; the
		// connection closes disconnect_delay seconds after the last one.
		m.c.DelayDisconnect(time.Duration(req.GetDisconnectDelay()) * time.Second)
		return s.send(m, mtproto.MakeTLPong(&mtproto.Pong{MsgId: msgID, PingId: req.GetPingId()}), true, true)

	case *mtproto.TLGetFutureSalts:
		return s.send(m, s.futureSalts(m, msgID, req.GetNum()), true, true)

	case *mtproto.TLDestroySession:
		res := &mtproto.DestroySessionRes{SessionId: req.GetSessionId()}
		if req.GetSessionId() != m.sessionID && m.key.destroySession(req.GetSessionId()) {
			return s.send(m, mtproto.MakeTLDestroySessionOk(res), true, true)
		}
		return s.send(m, mtproto.MakeTLDestroySessionNone(res), true, true)

	case *mtproto.TLMsgsStateReq:
		ids := req.GetMsgIds()
		if len(ids) > 8192 {
			return errMessage
		}
		info := make([]byte, len(ids))
		for i, id := range ids {
			info[i] = m.sess.stateInfo(id, m.now)
		}
		return s.send(m, mtproto.MakeTLMsgsStateInfo(&mtproto.MsgsStateInfo{ReqMsgId: msgID, Info: string(info)}), true, false)

	case *mtproto.TLRpcDropAnswer:
		// Android cancels a download with it (ConnectionsManager.cpp:2157-2170)
		// and, on rpc_answer_dropped_running, still takes the answer if it
		// comes (:1471-1475). The node does not stop a read in progress.
		var res mtproto.TLObject = mtproto.MakeTLRpcAnswerUnknown(&mtproto.RpcDropAnswer{})
		if m.sess.running(req.GetReqMsgId()) {
			res = mtproto.MakeTLRpcAnswerDroppedRunning(&mtproto.RpcDropAnswer{})
		}
		return s.sendResult(m, msgID, res)
	}

	// A request: served off the read loop.
	if !m.sess.start(msgID) {
		return nil
	}
	query := append([]byte(nil), body...)
	key, sess, sessionID, c := m.key, m.sess, m.sessionID, m.c
	if !c.Go(func() {
		defer sess.finish(msgID)
		res := s.invoke(query)
		if err := s.sendResult(&in{c: c, key: key, sess: sess, sessionID: sessionID, now: s.now()}, msgID, res); err != nil {
			logx.Debugf("cdn session: answer not sent: %v", err)
		}
	}) {
		sess.finish(msgID)
	}
	return nil
}

// futureSalts answers get_future_salts with the salts of the key from the
// current period on.
func (s *Server) futureSalts(m *in, msgID int64, num int32) mtproto.TLObject {
	num = max(1, min(num, maxFutureSalts))
	p := m.now.Unix() / saltPeriod
	list := make([]*mtproto.TLFutureSalt, 0, num)
	for i := int64(0); i < int64(num); i++ {
		list = append(list, mtproto.MakeTLFutureSalt(&mtproto.FutureSalt{
			ValidSince: int32((p + i) * saltPeriod),
			ValidUntil: int32((p+i+1)*saltPeriod + saltGrace),
			Salt:       s.salts.salt(m.key.ID, p+i),
		}))
	}
	return mtproto.MakeTLFutureSalts(&mtproto.FutureSalts{ReqMsgId: msgID, Now: int32(m.now.Unix()), Salts: list})
}

// invoke runs a request and returns its result (an rpc_error for a
// failure).
func (s *Server) invoke(query []byte) mtproto.TLObject {
	for depth := 0; depth < maxWrapping; depth++ {
		d := mtproto.NewDecodeBuf(query)
		obj := d.Object()
		if d.GetError() != nil {
			break
		}
		switch req := obj.(type) {
		case *mtproto.TLInvokeWithLayer:
			query = req.GetQuery()
		case *mtproto.TLInitConnection:
			// Neither logged nor kept (ADR 0065 §5): tdesktop sends the
			// same language, proxy and params as to the home
			// (session_private.cpp:671-707).
			query = req.GetQuery()
		case *mtproto.TLInvokeAfterMsg:
			// The order is the client's: the node answers each request when
			// it is done. tdesktop and Android may wrap any request so
			// (session_private.cpp:805-813, ConnectionsManager.cpp:3119-3130).
			query = req.GetQuery()
		case *mtproto.TLInvokeAfterMsgs:
			query = req.GetQuery()
		case *mtproto.TLInvokeWithoutUpdates:
			// The node sends no updates anyway.
			query = req.GetQuery()
		case *mtproto.TLUploadGetCdnFile:
			res, err := s.getCdnFile(req)
			if err != nil {
				return mtproto.NewRpcError(err)
			}
			return res
		default:
			metrics.Requests.WithLabelValues(metrics.RequestMethodInvalid).Inc()
			return mtproto.NewRpcError(mtproto.ErrCdnMethodInvalid)
		}
	}
	// Undecodable, or wrapped too deep: not a request the node serves.
	metrics.Requests.WithLabelValues(metrics.RequestMethodInvalid).Inc()
	return mtproto.NewRpcError(mtproto.ErrCdnMethodInvalid)
}

// getCdnFile serves upload.getCdnFile (ADR 0065 §5): the bytes of
// [offset, offset+limit) of the cipher, cut at its end; at or past the end,
// empty bytes, which every client takes for the end of the file (Android
// FileLoadOperation.java:1884-1886, iOS FetchV2.swift:926-927). A file the
// node does not have is upload.cdnFileReuploadNeeded with a request_token
// the home checks.
func (s *Server) getCdnFile(req *mtproto.TLUploadGetCdnFile) (mtproto.TLObject, error) {
	offset := req.GetOffset_INT64()
	if req.GetConstructor() == mtproto.CRC32_upload_getCdnFile_2000bcc3 {
		// The constructor of old layers, with an int offset.
		offset = int64(req.GetOffset_INT32())
	}
	limit := req.GetLimit()
	if err := checkFileRange(offset, limit); err != nil {
		metrics.Requests.WithLabelValues(metrics.RequestRangeInvalid).Inc()
		return nil, err
	}
	token := req.GetFileToken()
	if len(token) != cdn.TokenSize {
		// Not a token of the home: the client goes back to it.
		metrics.Requests.WithLabelValues(metrics.RequestTokenInvalid).Inc()
		return nil, mtproto.ErrFileTokenInvalid
	}

	b, _, readErr, err := s.files.ReadAt(token, offset, int(limit))
	switch {
	case errors.Is(err, store.ErrNotFound):
		result := metrics.RequestReupload
		if readErr {
			result = metrics.RequestReadError
		}
		rt, ok := s.tokens.Sign(token, s.now())
		if !ok {
			// No request_token key since the start: a token the home could
			// not check would only cost the client a round trip to fail.
			// FILE_TOKEN_INVALID sends it back to upload.getFile at once
			// (Android FileLoadOperation.java:2583-2589, tdesktop
			// download_manager_mtproto.cpp:958-980).
			metrics.Requests.WithLabelValues(metrics.RequestNoKeys).Inc()
			return nil, mtproto.ErrFileTokenInvalid
		}
		metrics.Requests.WithLabelValues(result).Inc()
		return mtproto.MakeTLUploadCdnFileReuploadNeeded(&mtproto.Upload_CdnFile{RequestToken: rt}), nil
	case err != nil:
		logx.Errorf("cdn session: read: %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	if len(b) == 0 {
		metrics.Requests.WithLabelValues(metrics.RequestEOF).Inc()
	} else {
		metrics.Requests.WithLabelValues(metrics.RequestOK).Inc()
		metrics.ServedBytes.Add(float64(len(b)))
	}
	return mtproto.MakeTLUploadCdnFile(&mtproto.Upload_CdnFile{Bytes: b}), nil
}

// checkFileRange applies the rules of upload.getCdnFile
// (core.telegram.org/cdn): offset and limit divisible by 4 KB, 1 MB
// divisible by limit, the range within one 1 MB chunk; the same rules as
// upload.getFile without precise (app/bff/files upload.getFile_handler.go).
func checkFileRange(offset int64, limit int32) error {
	const chunk = 1 << 20
	switch {
	case limit <= 0, limit > chunk:
		return mtproto.ErrLimitInvalid
	case offset < 0:
		return mtproto.ErrOffsetInvalid
	case offset%4096 != 0:
		return mtproto.ErrOffsetInvalid
	case limit%4096 != 0 || chunk%limit != 0:
		return mtproto.ErrLimitInvalid
	case offset/chunk != (offset+int64(limit)-1)/chunk:
		return mtproto.ErrLimitInvalid
	}
	return nil
}

// sendResult sends rpc_result for request msgID. A result that does not
// encode is answered INTERNAL_SERVER_ERROR, so that the client does not
// wait for it.
func (s *Server) sendResult(m *in, msgID int64, res mtproto.TLObject) error {
	x := mtproto.NewEncodeBuf(512)
	x.Int(int32(mtproto.CRC32_rpc_result))
	x.Long(msgID)
	if err := res.Encode(x, schemaLayer); err != nil {
		logx.Errorf("cdn session: encode %T: %v", res, err)
		x = mtproto.NewEncodeBuf(64)
		x.Int(int32(mtproto.CRC32_rpc_result))
		x.Long(msgID)
		if err := mtproto.NewRpcError(mtproto.ErrInternalServerError).Encode(x, schemaLayer); err != nil {
			return fmt.Errorf("session: encode rpc_error: %w", err)
		}
	}
	return s.sendRaw(m, x.GetBuf(), true, true)
}

// send sends obj; answer marks an answer to a client message (msg_id 1
// modulo 4), content a content-related message (odd seqno).
func (s *Server) send(m *in, obj mtproto.TLObject, answer, content bool) error {
	x := mtproto.NewEncodeBuf(128)
	if err := obj.Encode(x, schemaLayer); err != nil {
		return fmt.Errorf("session: encode: %w", err)
	}
	return s.sendRaw(m, x.GetBuf(), answer, content)
}

func (s *Server) sendRaw(m *in, body []byte, answer, content bool) error {
	now := s.now()
	var seqNo int32
	if m.sess != nil {
		seqNo = m.sess.seqNo(content)
	} else if content {
		seqNo = 1
	}
	plain := make([]byte, 32+len(body))
	binary.LittleEndian.PutUint64(plain, uint64(s.salts.current(m.key, now)))
	binary.LittleEndian.PutUint64(plain[8:], uint64(m.sessionID))
	binary.LittleEndian.PutUint64(plain[16:], uint64(s.ids.next(now, answer)))
	binary.LittleEndian.PutUint32(plain[24:], uint32(seqNo))
	binary.LittleEndian.PutUint32(plain[28:], uint32(len(body)))
	copy(plain[32:], body)
	msgKey, data, err := m.key.ak.AesIgeEncrypt(plain)
	if err != nil {
		return fmt.Errorf("session: encrypt: %w", err)
	}
	packet := make([]byte, 0, 24+len(data))
	packet = binary.LittleEndian.AppendUint64(packet, uint64(m.key.ID))
	packet = append(packet, msgKey...)
	packet = append(packet, data...)
	return m.c.Send(packet)
}

// quickAckToken is the quick ACK of a message
// (https://core.telegram.org/mtproto/mtproto-transports#quick-ack): the
// first 32 bits of SHA256(auth_key[88:120] + the plaintext) with the high
// bit set, as the gateway computes it.
func quickAckToken(authKey, plain []byte) uint32 {
	h := sha256.New()
	h.Write(authKey[88:120])
	h.Write(plain)
	var sum [32]byte
	h.Sum(sum[:0])
	return binary.LittleEndian.Uint32(sum[:4]) | 0x80000000
}

// gunzip unpacks gzip_packed, at most maxGzipped bytes.
func gunzip(body []byte) ([]byte, error) {
	d := mtproto.NewDecodeBuf(body[4:])
	packed := d.StringBytes()
	if err := d.GetError(); err != nil {
		return nil, err
	}
	r, err := gzip.NewReader(bytes.NewReader(packed))
	if err != nil {
		return nil, err
	}
	b, err := io.ReadAll(io.LimitReader(r, maxGzipped+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxGzipped {
		return nil, errors.New("session: gzip_packed too large")
	}
	return b, nil
}
