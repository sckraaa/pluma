// Copyright 2024 Teamgram Authors
//  All rights reserved.
//
// Author: Benqi (wubenqi@gmail.com)
//

package ws

import (
	"bytes"
	"errors"
	"io"

	"github.com/gobwas/httphead"
	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/panjf2000/gnet/v2"
	"github.com/zeromicro/go-zero/core/logx"
)

// Limits on what a WebSocket connection may make the gateway buffer.
const (
	// MaxMessageSize bounds a WebSocket message (all its frames): one
	// MTProto transport packet (at most 16 MiB) and the 64-byte
	// obfuscation header in front of the first one.
	MaxMessageSize = 16<<20 + 1024
	// maxUpgradeRequestSize bounds the HTTP upgrade request.
	maxUpgradeRequestSize = 64 << 10
	// maxControlPayload is the RFC 6455 limit for control frames.
	maxControlPayload = 125
	// maxBuffered bounds the raw bytes read from the socket and not yet
	// framed: one frame being received and what arrives behind it.
	maxBuffered = 2*MaxMessageSize + 2*ws.MaxHeaderSize
)

var (
	ErrUpgradeRequestTooLarge = errors.New("ws: upgrade request too large")
	ErrMessageTooLarge        = errors.New("ws: message too large")
	ErrControlFrame           = errors.New("ws: invalid control frame")
	ErrBufferFull             = errors.New("ws: too much data buffered")
)

type WsCodec struct {
	upgraded bool         // 链接是否升级
	Buf      bytes.Buffer // 从实际socket中读取到的数据缓存
	wsMsgBuf wsMessageBuf // ws 消息缓存
	Conn     WsConn
}

type wsMessageBuf struct {
	curHeader *ws.Header
	cachedBuf bytes.Buffer
}

type readWrite struct {
	io.Reader
	io.Writer
}

func (w *WsCodec) Upgrade(c gnet.Conn) (ok bool, action gnet.Action) {
	if w.upgraded {
		ok = true
		return
	}
	buf := &w.Buf
	tmpReader := bytes.NewReader(buf.Bytes())
	oldLen := tmpReader.Len()
	logx.Debugf("do Upgrade")

	upgrader := ws.Upgrader{
		Protocol: func(i []byte) bool {
			if bytes.Equal(i, []byte("binary")) {
				return true
			}

			return false
		},
		//ProtocolCustom: func(i []byte) (string, bool) {
		//	return "", true
		//},
		Extension: func(option httphead.Option) bool {
			return true
		},
		ExtensionCustom: func(i []byte, options []httphead.Option) ([]httphead.Option, bool) {
			return nil, true
		},
		//Negotiate: func(option httphead.Option) (httphead.Option, error) {
		//	return
		//},
		OnRequest: func(uri []byte) error {
			// TODO: check
			return nil
		},
		OnHost: func(host []byte) error {
			return nil
		},
		OnHeader: func(key, value []byte) error {
			return nil
		},
		//OnBeforeUpgrade: func() (header ws.HandshakeHeader, err error) {
		//	//
		//},
	}

	hs, err := upgrader.Upgrade(readWrite{tmpReader, c})
	skipN := oldLen - tmpReader.Len()
	if err != nil {
		if err == io.EOF || errors.Is(err, io.ErrUnexpectedEOF) { //数据不完整，不跳过 buf 中的 skipN 字节（此时 buf 中存放的仅是部分 "handshake data" bytes），下次再尝试读取
			if buf.Len() > maxUpgradeRequestSize {
				logx.Errorf("conn[%v] upgrade request: %v", c.RemoteAddr().String(), ErrUpgradeRequestTooLarge)
				action = gnet.Close
			}
			return
		}
		buf.Next(skipN)
		// Not the request itself: its headers may carry cookies or tokens.
		logx.Errorf("conn[%v] websocket upgrade request of %d bytes: %v", c.RemoteAddr().String(), skipN, err)
		action = gnet.Close
		return
	}
	buf.Next(skipN)
	logx.Debugf("conn[%v] upgrade websocket protocol, subprotocol: %q", c.RemoteAddr().String(), hs.Protocol)

	ok = true
	w.upgraded = true
	return
}

func (w *WsCodec) ReadBufferBytes(c gnet.Conn) gnet.Action {
	data, err := c.Next(-1)
	if err != nil {
		logx.Errorf("read err! %v", err)
		return gnet.Close
	}
	if w.Buf.Len()+len(data) > maxBuffered {
		logx.Errorf("conn(%d) %v", c.ConnId(), ErrBufferFull)
		return gnet.Close
	}
	w.Buf.Write(data)
	return gnet.None
}

func (w *WsCodec) Decode(c gnet.Conn) (outs []wsutil.Message, err error) {
	logx.Debug("do Decode")
	messages, err := w.readWsMessages()
	if err != nil {
		logx.Errorf("Error reading message! %v", err)
		return nil, err
	}
	if messages == nil || len(messages) <= 0 { //没有读到完整数据 不处理
		return
	}
	for _, message := range messages {
		if message.OpCode.IsControl() {
			err = wsutil.HandleClientControlMessage(c, message)
			if err != nil {
				return
			}
			continue
		}
		if message.OpCode == ws.OpText || message.OpCode == ws.OpBinary {
			outs = append(outs, message)
		}
	}
	return
}

func (w *WsCodec) readWsMessages() (messages []wsutil.Message, err error) {
	msgBuf := &w.wsMsgBuf
	in := &w.Buf
	for {
		// 从 in 中读出 header，并将 header bytes 写入 msgBuf.cachedBuf
		if msgBuf.curHeader == nil {
			if in.Len() < ws.MinHeaderSize { //头长度至少是2
				return
			}
			var head ws.Header
			if in.Len() >= ws.MaxHeaderSize {
				head, err = ws.ReadHeader(in)
				if err != nil {
					return messages, err
				}
			} else { //有可能不完整，构建新的 reader 读取 head，读取成功才实际对 in 进行读操作
				tmpReader := bytes.NewReader(in.Bytes())
				oldLen := tmpReader.Len()
				head, err = ws.ReadHeader(tmpReader)
				skipN := oldLen - tmpReader.Len()
				if err != nil {
					if err == io.EOF || errors.Is(err, io.ErrUnexpectedEOF) { //数据不完整
						return messages, nil
					}
					in.Next(skipN)
					return nil, err
				}
				in.Next(skipN)
			}

			// Refuse what cannot be valid before buffering its payload.
			if head.OpCode.IsControl() {
				if head.Length > maxControlPayload || !head.Fin {
					return nil, ErrControlFrame
				}
			} else if head.Length > MaxMessageSize || int64(msgBuf.cachedBuf.Len())+head.Length > MaxMessageSize+ws.MaxHeaderSize*16 {
				return nil, ErrMessageTooLarge
			}

			msgBuf.curHeader = &head
			err = ws.WriteHeader(&msgBuf.cachedBuf, head)
			if err != nil {
				return nil, err
			}
		}
		dataLen := (int)(msgBuf.curHeader.Length)
		// 从 in 中读出 data，并将 data bytes 写入 msgBuf.cachedBuf
		if dataLen > 0 {
			if in.Len() < dataLen { //数据不完整
				logx.Debug(in.Len(), dataLen)
				logx.Infof("incomplete data")
				return
			}

			_, err = io.CopyN(&msgBuf.cachedBuf, in, int64(dataLen))
			if err != nil {
				return
			}
		}
		if msgBuf.curHeader.Fin { //当前 header 已经是一个完整消息
			messages, err = wsutil.ReadClientMessage(&msgBuf.cachedBuf, messages)
			if err != nil {
				return nil, err
			}
			msgBuf.cachedBuf.Reset()
		} else {
			logx.Infof("The data is split into multiple frames")
		}
		msgBuf.curHeader = nil
	}
}
