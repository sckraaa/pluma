package gnet

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/panjf2000/gnet/v2"
	"github.com/zeromicro/go-zero/core/logx"
)

// gnet does not recover panics: one in an event handler or in a callback run
// on an event loop takes the whole gateway down. Everything that processes
// data of a single connection therefore runs under recoverConn, so that a
// malformed packet closes that connection and nothing else.

// logRecoveredPanic logs a recovered panic with the connection id, the type
// of the panic value and where it happened. The value itself is not logged:
// it may carry data from the packet.
func logRecoveredPanic(connId int64, where string, r any) {
	metricPanicRecovered.Inc(where)
	logx.Errorf("conn(%d) %s: recovered panic (%T) at %s", connId, where, r, panicSite())
}

// panicSite returns file:line of the function that panicked, called from a
// deferred function during the panic.
func panicSite() string {
	var pcs [32]uintptr
	n := runtime.Callers(3, pcs[:])
	frames := runtime.CallersFrames(pcs[:n])
	afterPanic := false
	for {
		f, more := frames.Next()
		if f.Function == "runtime.gopanic" {
			afterPanic = true
		} else if afterPanic && !strings.HasPrefix(f.Function, "runtime.") {
			return fmt.Sprintf("%s:%d", f.File, f.Line)
		}
		if !more {
			return "unknown"
		}
	}
}

// recoverConn, deferred by a function that processes data of connection c,
// turns a panic into closing c: *action (if not nil) becomes gnet.Close and
// c is closed otherwise.
func recoverConn(c gnet.Conn, where string, action *gnet.Action) {
	if r := recover(); r != nil {
		logRecoveredPanic(c.ConnId(), where, r)
		if action != nil {
			*action = gnet.Close
		} else {
			_ = c.Close()
		}
	}
}

// trigger runs cb on the event loop of connection connId (if it is still
// open); a panic in cb closes that connection.
func (s *Server) trigger(connId int64, cb func(c gnet.Conn)) {
	if s.triggerFn != nil {
		s.triggerFn(connId, func(c gnet.Conn) {
			defer recoverConn(c, "trigger", nil)
			cb(c)
		})
		return
	}
	s.eng.Trigger(connId, func(c gnet.Conn) {
		defer recoverConn(c, "trigger", nil)
		cb(c)
	})
}

// submit runs task in the worker pool on behalf of connection connId; a
// panic in task closes that connection.
func (s *Server) submit(connId int64, task func()) error {
	return s.pool.Submit(func() {
		defer func() {
			if r := recover(); r != nil {
				logRecoveredPanic(connId, "worker", r)
				s.trigger(connId, func(c gnet.Conn) {
					_ = c.Close()
				})
			}
		}()
		task()
	})
}
