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

package gnet

import "golang.org/x/sys/unix"

// socketQueued returns the bytes in the send queue of socket fd that the
// peer has not acknowledged (SIOCOUTQ, tcp(7)). A socket that cannot be
// asked is closing: nothing more leaves it, so 0.
func socketQueued(fd int) int {
	n, err := unix.IoctlGetInt(fd, unix.SIOCOUTQ)
	if err != nil {
		return 0
	}
	return n
}
