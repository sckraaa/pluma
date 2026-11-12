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

// Package tde2e checks the blockchain of an end-to-end encrypted group call
// the way the server sees it (ADR 0060 §2, task 094). The server orders and
// checks blocks but never holds a key: it cannot read the shared key and
// cannot write a block itself.
//
// The package is a Go port of the server side of tdlib's tde2e at commit
// e894536b, the version the official clients link:
//
//   - the TL serialization of e2e.chain.block, its changes and state proof,
//     and of the two broadcasts of sub-chain 1 (td/generate/scheme/
//     e2e_api.tl:67-81);
//   - the wire form: the server hands out every block and broadcast with its
//     first int32 plus one (ToServer, ToLocal);
//   - Chain, the rules of ServerBlockchain (TestBlockchain.cpp:161-178):
//     height and previous hash, the Ed25519 signature, the permissions of
//     group state changes, the shared key and the state proof;
//   - Verification, the commit and reveal rules of sub-chain 1, the emoji
//     verification (Call.cpp:65-257);
//   - the server policy of ADR 0060 on top of the chain: Chain.Join,
//     Chain.Remove and Verification.Accept.
//
// File references without a directory name files under tde2e/td/e2e/ of
// tdlib at that commit. A refused block or broadcast is an *Error whose Kind
// tells which RPC error the server answers with.
//
// The key-value store of the chain (Trie.cpp, BitString.cpp) is not ported:
// calls never write values, so a ChangeSetValue is refused and the key-value
// hash of every call block is EmptyKVHash.
package tde2e
