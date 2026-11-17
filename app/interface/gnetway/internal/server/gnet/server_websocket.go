// Copyright 2024 Teamgram Authors
//  All rights reserved.
//
// Author: Benqi (wubenqi@gmail.com)
//

package gnet

import (
	"errors"
	"net/http"
	"net/netip"
	"strings"

	"github.com/teamgram/teamgram-server/app/interface/gnetway/internal/config"
	wscodec "github.com/teamgram/teamgram-server/app/interface/gnetway/internal/server/gnet/ws"
	"github.com/teamgram/teamgram-server/pkg/mtserver/codec"

	wsproto "github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
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
		// A close frame of the client (or of the proxy in front): it
		// closed the connection, not the gateway.
		var closed wsutil.ClosedError
		if errors.As(err, &closed) {
			ctx.closeReason = closeReasonPeer
		}
		return gnet.Close
	}
	if messages == nil {
		return
	}
	// Only data messages keep an authenticated connection open: the pings
	// of the Edge entry and the client's pongs and pings do not.
	if ctx.authenticated {
		s.extendDeadline(c, ctx)
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

// Headers the origin proxy of an edge sets on the upgrade request to the
// Edge entry (ADR 0072 §3, brief 109 "Сервер"); it overwrites what the
// client sent under these names.
const (
	// headerClientAddr: the client's address as a list, as X-Forwarded-For
	// (Gcore) or a single address (the Russian node's PROXY header).
	headerClientAddr = "X-Pluma-Client-Addr"
	// headerEdgeVia: which edge the connection came through, for metrics.
	headerEdgeVia = "X-Pluma-Edge-Via"
)

// edgeUpgrade reads the upgrade request of an Edge connection
// (wscodec.HeaderReader): the client's address comes from
// X-Pluma-Client-Addr, and only then are the limits of that address
// counted, as for a PROXY header (OnTraffic). A request without an address
// is refused before any limit is counted.
type edgeUpgrade struct {
	s   *Server
	c   gnet.Conn
	ctx *connContext
	// clientAddr: the values of all X-Pluma-Client-Addr lines joined by
	// commas, as one list (RFC 9110 §5.3); via: the last X-Pluma-Edge-Via.
	clientAddr []byte
	via        []byte
}

func (e *edgeUpgrade) Reset() {
	e.clientAddr = e.clientAddr[:0]
	e.via = e.via[:0]
}

func (e *edgeUpgrade) Header(key, value []byte) {
	switch string(key) {
	case headerClientAddr:
		if len(e.clientAddr) > 0 {
			e.clientAddr = append(e.clientAddr, ',')
		}
		e.clientAddr = append(e.clientAddr, value...)
	case headerEdgeVia:
		e.via = append(e.via[:0], value...)
	}
}

func (e *edgeUpgrade) Check() error {
	ctx := e.ctx
	ctx.via = e.s.edgeVia(string(e.via))
	ip, ok := edgeClientAddr(string(e.clientAddr))
	if !ok {
		ctx.closeReason = closeReasonNoAddr
		metricEdgeNoClientAddr.Inc(ctx.via)
		logx.Errorf("conn(%d) edge: the upgrade request has no client address", e.c.ConnId())
		return wsproto.RejectConnectionError(wsproto.RejectionStatus(http.StatusBadRequest), wsproto.RejectionReason("no client address"))
	}
	ctx.setClientIp(ip)
	if !e.s.countConn(e.c, ctx) {
		return wsproto.RejectConnectionError(wsproto.RejectionStatus(http.StatusServiceUnavailable), wsproto.RejectionReason("too many connections"))
	}
	entryOpened(ctx)
	e.s.schedulePing(e.c, ctx)
	return nil
}

// edgeClientAddr returns the client's address from the value of
// X-Pluma-Client-Addr: the last element of the comma-separated list that is
// an IP address (a port after it is dropped). The last one was appended by
// the proxy that saw the client's connection; the elements before it come
// from the client and may be forged.
func edgeClientAddr(list string) (string, bool) {
	for rest := list; rest != ""; {
		elem := rest
		if i := strings.LastIndexByte(rest, ','); i >= 0 {
			elem, rest = rest[i+1:], rest[:i]
		} else {
			rest = ""
		}
		if ip, ok := parseClientAddr(strings.TrimSpace(elem)); ok {
			return ip, true
		}
	}
	return "", false
}

// parseClientAddr parses an IPv4 or IPv6 address, alone or with a port
// ("203.0.113.5:443", "[2001:db8::1]:443"), into its canonical form. A
// zone, an unspecified or a multicast address is no client's.
func parseClientAddr(s string) (string, bool) {
	addr, err := netip.ParseAddr(s)
	if err != nil {
		ap, err := netip.ParseAddrPort(s)
		if err != nil {
			return "", false
		}
		addr = ap.Addr()
	}
	addr = addr.Unmap()
	if addr.Zone() != "" || addr.IsUnspecified() || addr.IsMulticast() {
		return "", false
	}
	return addr.String(), true
}

// edgeVia returns the metric label of an X-Pluma-Edge-Via value.
func (s *Server) edgeVia(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if s.edgeVias[v] {
		return v
	}
	return config.EdgeViaOther
}

// schedulePing puts an upgraded Edge connection in the ping wheel.
func (s *Server) schedulePing(c gnet.Conn, ctx *connContext) {
	ctx.nextPing = s.CachedNow() + s.pingInterval
	s.pingWheel.Add(c.ConnId(), ctx.nextPing)
}

// expirePings pings the Edge connections due at now (OnTick). Gcore closes
// a WebSocket after 30 s without data from the origin (ADR 0072,
// "Gcore"); a ping is such data. It does not extend the connection's idle
// deadline, nor does the client's pong (onWebsocketData).
func (s *Server) expirePings(now int64) {
	for _, connId := range s.pingWheel.ExpireThrough(now) {
		s.trigger(connId, func(c gnet.Conn) {
			ctx, _ := c.Context().(*connContext)
			if ctx == nil || ctx.nextPing == 0 {
				return
			}
			// Not due yet: put back. A ping due more than an interval
			// ahead was scheduled before the clock went back; it is sent
			// now rather than after the clock catches up.
			if now < ctx.nextPing && ctx.nextPing-now <= s.pingInterval {
				s.pingWheel.Add(connId, ctx.nextPing)
				return
			}
			if err := wsutil.WriteServerMessage(c, wsproto.OpPing, nil); err != nil {
				// The connection is going away: OnClose follows.
				logx.Errorf("conn(%d) edge ping: %v", connId, err)
				ctx.nextPing = 0
				return
			}
			metricEdgePing.Inc(ctx.via)
			ctx.nextPing = now + s.pingInterval
			s.pingWheel.Add(connId, ctx.nextPing)
		})
	}
}
