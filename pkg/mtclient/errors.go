package mtclient

import (
	"errors"
	"fmt"
	"strings"
)

// ErrClosed is returned for calls on a closed client.
var ErrClosed = errors.New("mtclient: client closed")

// RPCError is an rpc_error the server answered a request with.
type RPCError struct {
	Code    int32
	Message string
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

// IsRPCError reports whether err is an rpc_error whose message is message or,
// if message ends with '_', starts with it (e.g. "FLOOD_WAIT_").
func IsRPCError(err error, message string) bool {
	var e *RPCError
	if !errors.As(err, &e) {
		return false
	}
	if strings.HasSuffix(message, "_") {
		return strings.HasPrefix(e.Message, message)
	}
	return e.Message == message
}

// BadMsgError is a bad_msg_notification the client could not recover from
// by resending (https://core.telegram.org/mtproto/service_messages_about_messages).
type BadMsgError struct {
	Code int32
}

func (e *BadMsgError) Error() string {
	return fmt.Sprintf("mtclient: bad_msg_notification %d", e.Code)
}
