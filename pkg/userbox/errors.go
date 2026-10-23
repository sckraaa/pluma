package userbox

import (
	"database/sql"
	"errors"

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/grpc/status"
)

// minMTProtoCode is the smallest code of an MTProto error (303 SEE_OTHER):
// business errors are gRPC statuses with these codes, transport ones have
// the codes of gRPC (0-16).
const minMTProtoCode = 300

// MySQL errors of the data itself: the same statement fails the same way
// on every attempt.
var deterministicMySQL = map[uint16]bool{
	1048: true, // ER_BAD_NULL_ERROR
	1062: true, // ER_DUP_ENTRY
	1264: true, // ER_WARN_DATA_OUT_OF_RANGE
	1366: true, // ER_TRUNCATED_WRONG_VALUE_FOR_FIELD
	1406: true, // ER_DATA_TOO_LONG
	3140: true, // ER_INVALID_JSON_TEXT
}

// Deterministic tells whether err of a box operation comes back the same on
// every attempt (ADR 0012, p. 5): a business error (an MTProto error), a
// MySQL error of the data, a misuse of the Box (ErrMisuse,
// ErrEventNotFinished, a nested Run, an event CheckEvent refuses), or a
// state changed by a writer bypassing Run. The list is closed: anything
// else, an unknown error included, is temporary and may be tried again
// later, so a restart of MySQL never looks like a broken event.
func Deterministic(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrMisuse) || errors.Is(err, ErrInsideRun) || errors.Is(err, ErrInvalidEvent) || errors.Is(err, errStateChanged) {
		return true
	}
	if n, ok := mysqlErrorNumber(err); ok {
		return deterministicMySQL[n]
	}
	var rpcErr *mtproto.TLRpcError
	if errors.As(err, &rpcErr) {
		return true
	}
	if st, ok := status.FromError(err); ok && st.Code() >= minMTProtoCode {
		return true
	}
	return false
}

func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }
