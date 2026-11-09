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

// Package tdjson is the JSON interface of TDLib (td/telegram/td_json_client.h).
//
// The library is C++ and has to be built for the image, so only the code
// sender links it: with the build tag tdlib (teamgramd/deploy/codes/Dockerfile).
// Built without the tag, as everything else in the repository is, Open returns
// ErrNotLinked; the rest of the service compiles and runs its tests.
package tdjson

import (
	"errors"
	"time"
)

// ErrNotLinked is returned by Open in a binary built without the tag tdlib.
var ErrNotLinked = errors.New("tdjson: this binary is built without TDLib (-tags tdlib, teamgramd/deploy/codes/Dockerfile)")

// Library is the interface of td_json_client.h. Receive must be called from
// one goroutine at a time; Send and Execute from any.
type Library interface {
	// CreateClient returns the identifier of a new TDLib instance
	// (td_create_client_id). It starts on the first request sent to it.
	CreateClient() int
	// Send sends a request to the client (td_send).
	Send(clientID int, request string)
	// Receive returns the next response or update of any client, "" if
	// there is none within timeout (td_receive).
	Receive(timeout time.Duration) string
	// Execute runs a request that TDLib allows synchronously (td_execute).
	Execute(request string) string
}
