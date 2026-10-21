// Package logpolicy switches off the logging go-zero does on its own of RPC
// requests, SQL statements and Redis commands, which would otherwise put
// message text, auth keys, phone numbers and login data into the logs (audit
// findings #8, #9, #25; ADR 0003):
//
//   - the zrpc server logs every request as JSON at info level, and again on
//     slow calls;
//   - the zrpc client logs the request of every failed call and the request
//     and reply of every slow call;
//   - sqlx logs every statement with its arguments inlined at info level,
//     and on slow calls and errors;
//   - the Redis client logs every argument of a slow command.
//
// Apply is called by conf.Load, the first thing every service does, so no
// service starts with these logs enabled. Timing metrics stay: the zrpc stat
// and Redis duration metrics are kept, slow RPC calls are still logged with
// the method name only. sqlx records its duration metrics only while it logs
// statements, so those are lost.
package logpolicy

import (
	"math"
	"sort"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/stores/redis"
	zsqlx "github.com/zeromicro/go-zero/core/stores/sqlx"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

var once sync.Once

// Apply switches the logs off; calling it again does nothing.
func Apply() {
	once.Do(func() {
		zsqlx.DisableLog()
		redis.SetSlowThreshold(time.Duration(math.MaxInt64))
		for _, method := range rpcMethods() {
			zrpc.DontLogContentForMethod(method)
			zrpc.DontLogClientContentForMethod(method)
		}
	})
}

// rpcMethods returns the full gRPC name ("/package.Service/Method") of every
// method of every service linked into the binary. All RPC services here are
// generated from .proto files, whose descriptors register themselves in
// protoregistry.GlobalFiles when their package is initialized, before main.
func rpcMethods() []string {
	var methods []string
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		services := fd.Services()
		for i := 0; i < services.Len(); i++ {
			sd := services.Get(i)
			ms := sd.Methods()
			for j := 0; j < ms.Len(); j++ {
				methods = append(methods, "/"+string(sd.FullName())+"/"+string(ms.Get(j).Name()))
			}
		}
		return true
	})
	sort.Strings(methods)
	return methods
}
