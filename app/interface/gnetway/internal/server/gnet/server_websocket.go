// Copyright 2024 Teamgram Authors
//  All rights reserved.
//
// Author: Benqi (wubenqi@gmail.com)
//

package gnet

import (
	"errors"

	"github.com/teamgram/teamgram-server/app/interface/gnetway/internal/server/gnet/codec"
	wscodec "github.com/teamgram/teamgram-server/app/interface/gnetway/internal/server/gnet/ws"

	"github.com/panjf2000/gnet/v2"
	"github.com/zeromicro/go-zero/core/logx"
)

func (s *Server) onWebsocketData(ctx *connContext, c gnet.Conn) (action gnet.Action) {
	ws := ctx.wsCodec
	if ws.ReadBufferBytes(c) == gnet.Close {
		return gnet.Close
	}
	ok, action := ws.Upgrade(c)
	if !ok {
		return
	}

	if ws.Buf.Len() <= 0 {
		return gnet.None
	}
	messages, err := ws.Decode(c)
	if err != nil {
		return gnet.Close
	}
	if messages == nil {
		return
	}
	for _, message := range messages {
		ws.Conn.Buffer = message.Payload
		if s.onWebsocketMessage(ctx, c) == gnet.Close {
			return gnet.Close
		}
		if ws.Conn.InboundBuffered() > wscodec.MaxMessageSize {
			logx.Errorf("conn(%d) websocket: %v", c.ConnId(), wscodec.ErrBufferFull)
			return gnet.Close
		}
	}
	return gnet.None
}

// onWebsocketMessage decodes the transport frames of one WebSocket message
// (in ws.Conn.Buffer, after what earlier messages left in InboundBuffer). All
// complete frames are processed; the start of a frame that continues in the
// next message is kept for it. What is kept is at most one frame, which the
// transport codecs bound.
func (s *Server) onWebsocketMessage(ctx *connContext, c gnet.Conn) gnet.Action {
	ws := ctx.wsCodec
	defer func() {
		if len(ws.Conn.Buffer) > 0 {
			_, _ = ws.Conn.InboundBuffer.Write(ws.Conn.Buffer)
			ws.Conn.Buffer = ws.Conn.Buffer[:0]
		}
	}()

	if ctx.codec == nil {
		var err error
		ctx.codec, err = codec.CreateCodec(&ws.Conn)
		if err != nil {
			metricCodecDecodeError.Inc("websocket", classifyCodecError(err))
			if errors.Is(err, codec.ErrUnexpectedEOF) {
				return gnet.None
			}
			logx.Errorf("conn(%s) create codec error: %v", c, err)
			return gnet.Close
		}
	}

	for {
		needAck, frame, err := ctx.codec.Decode(&ws.Conn)
		if err != nil {
			metricCodecDecodeError.Inc("websocket", classifyCodecError(err))
			if errors.Is(err, codec.ErrUnexpectedEOF) {
				return gnet.None
			}
			logx.Errorf("conn(%s) frame is error: %v", c, err)
			return gnet.Close
		} else if frame == nil {
			return gnet.None
		}

		if s.onMTPRawMessage(ctx, c, needAck, frame) == gnet.Close {
			return gnet.Close
		}
	}
}
