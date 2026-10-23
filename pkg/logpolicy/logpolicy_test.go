package logpolicy

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/interface/session/session"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"

	"github.com/zeromicro/go-zero/core/logx"
	zsqlx "github.com/zeromicro/go-zero/core/stores/sqlx"
	"google.golang.org/grpc"
)

// TestRPCMethodsCoverServices checks that the methods found in the proto
// registry are the ones gRPC serves: requests to these services carry message
// text and auth keys.
func TestRPCMethodsCoverServices(t *testing.T) {
	known := map[string]bool{}
	for _, m := range rpcMethods() {
		known[m] = true
	}
	for _, sd := range []grpc.ServiceDesc{
		mtproto.RPCMessages_ServiceDesc,
		mtproto.RPCAuthorization_ServiceDesc,
		session.RPCSession_ServiceDesc,
		authsession.RPCAuthsession_ServiceDesc,
		msg.RPCMsg_ServiceDesc,
	} {
		if len(sd.Methods) == 0 {
			t.Fatalf("%s: no methods", sd.ServiceName)
		}
		for _, m := range sd.Methods {
			if name := "/" + sd.ServiceName + "/" + m.MethodName; !known[name] {
				t.Errorf("%s is not in the proto registry, its content would be logged", name)
			}
		}
	}
}

// TestSQLStatementsNotLogged runs a statement through go-zero's sqlx, which
// logs statements with their arguments inlined unless Apply turned it off.
func TestSQLStatementsNotLogged(t *testing.T) {
	var buf bytes.Buffer
	logx.SetWriter(logx.NewWriter(&buf))
	t.Cleanup(func() { logx.SetWriter(logx.NewWriter(io.Discard)) })

	db, err := sql.Open(fakeDriverName, "")
	if err != nil {
		t.Fatal(err)
	}
	conn := zsqlx.NewSqlConnFromDB(db)

	const secret = "message text 7c4a8d09"
	exec := func() {
		if _, err := conn.ExecCtx(context.Background(), "insert into messages (message) values (?)", secret); err != nil {
			t.Fatal(err)
		}
	}

	exec()
	if !strings.Contains(buf.String(), secret) {
		// go-zero changed its default: this test no longer shows anything.
		t.Fatalf("sqlx did not log the statement before Apply; log: %q", buf.String())
	}

	buf.Reset()
	Apply()
	exec()
	if strings.Contains(buf.String(), secret) {
		t.Fatalf("statement arguments logged after Apply: %q", buf.String())
	}
}

const fakeDriverName = "logpolicy-fake"

func init() { sql.Register(fakeDriverName, fakeDriver{}) }

// fakeDriver accepts every statement and returns no rows.
type (
	fakeDriver struct{}
	fakeConn   struct{}
	fakeStmt   struct{}
	fakeRows   struct{}
)

func (fakeDriver) Open(string) (driver.Conn, error)  { return fakeConn{}, nil }
func (fakeConn) Prepare(string) (driver.Stmt, error) { return fakeStmt{}, nil }
func (fakeConn) Close() error                        { return nil }
func (fakeConn) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }
func (fakeStmt) Close() error                        { return nil }
func (fakeStmt) NumInput() int                       { return -1 }
func (fakeStmt) Exec([]driver.Value) (driver.Result, error) {
	return driver.RowsAffected(1), nil
}
func (fakeStmt) Query([]driver.Value) (driver.Rows, error) { return fakeRows{}, nil }
func (fakeRows) Columns() []string                         { return nil }
func (fakeRows) Close() error                              { return nil }
func (fakeRows) Next([]driver.Value) error                 { return io.EOF }
