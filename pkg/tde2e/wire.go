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

package tde2e

import (
	"encoding/binary"
	"errors"
)

// A client sends a block or a broadcast in the local form and applies only
// what comes in the server form: the same bytes with the first int32 plus
// one. A block in the local form is refused with "Trying to apply local
// block, not from server" (Blockchain.cpp:720-762, called at Call.cpp:597,
// 618, 670); the reference server serves blocks this way
// (TestBlockchain.cpp:204, 210).

func isGoodMagic(magic uint32) bool {
	return magic == idBlock || magic == idBroadcastCommit || magic == idBroadcastReveal
}

// ToServer returns the server form of a block or broadcast in the local form
// (Blockchain::from_local_to_server). Unlike tde2e it refuses data that does
// not start with a block or broadcast id: the server only hands out what it
// has checked. data is not changed.
func ToServer(data []byte) ([]byte, error) {
	if len(data) < 4 {
		return nil, errors.New("tde2e: block is too short")
	}
	magic := binary.LittleEndian.Uint32(data)
	if !isGoodMagic(magic) {
		return nil, errors.New("tde2e: not a block or broadcast in the local form")
	}
	out := append([]byte{}, data...)
	binary.LittleEndian.PutUint32(out, magic+1)
	return out, nil
}

// ToLocal undoes ToServer (Blockchain::from_server_to_local): it refuses
// data already in the local form and subtracts one from the first int32
// otherwise. data is not changed.
func ToLocal(data []byte) ([]byte, error) {
	if len(data) < 4 {
		return nil, errors.New("tde2e: block is too short")
	}
	magic := binary.LittleEndian.Uint32(data)
	if isGoodMagic(magic) {
		return nil, errors.New("tde2e: local block, not from the server")
	}
	out := append([]byte{}, data...)
	binary.LittleEndian.PutUint32(out, magic-1)
	return out, nil
}

// IsFromServer reports whether data is in the server form
// (Blockchain::is_from_server, Blockchain.cpp:727-733).
func IsFromServer(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	magic := binary.LittleEndian.Uint32(data)
	return isGoodMagic(magic-1) && !isGoodMagic(magic)
}
