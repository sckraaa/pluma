//go:build tdlib

package tdjson

/*
#cgo LDFLAGS: -ltdjson
#include <stdlib.h>
#include <td/telegram/td_json_client.h>
*/
import "C"

import (
	"time"
	"unsafe"
)

type library struct{}

// Open returns the TDLib linked into the binary.
func Open() (Library, error) {
	return library{}, nil
}

func (library) CreateClient() int {
	return int(C.td_create_client_id())
}

func (library) Send(clientID int, request string) {
	r := C.CString(request)
	defer C.free(unsafe.Pointer(r))
	C.td_send(C.int(clientID), r)
}

func (library) Receive(timeout time.Duration) string {
	// The result stays valid only until the next call: copied at once.
	return C.GoString(C.td_receive(C.double(timeout.Seconds())))
}

func (library) Execute(request string) string {
	r := C.CString(request)
	defer C.free(unsafe.Pointer(r))
	return C.GoString(C.td_execute(r))
}
