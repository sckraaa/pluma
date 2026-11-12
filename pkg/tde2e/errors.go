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

import "errors"

// Kind is the answer the server gives to a refused block or broadcast
// (ADR 0060 §2, «Ошибки»).
type Kind uint8

const (
	// KindInvalid is BLOCK_INVALID: the bytes, the signature, the state or
	// the server policy.
	KindInvalid Kind = iota
	// KindStale is CONF_WRITE_CHAIN_INVALID: the block is not on top of the
	// current chain. The client reads the last block again and rewrites it.
	KindStale
	// KindForbidden is GROUPCALL_FORBIDDEN: the writer is not in the group
	// state of the chain.
	KindForbidden
	// KindTooMany is GROUPCALL_PARTICIPANTS_TOO_MUCH: the group state would
	// exceed MaxParticipants.
	KindTooMany
)

func (k Kind) String() string {
	switch k {
	case KindInvalid:
		return "invalid"
	case KindStale:
		return "stale"
	case KindForbidden:
		return "forbidden"
	case KindTooMany:
		return "too many"
	}
	return "unknown"
}

// Code is the tde2e error code of a refusal (e2e_errors.h:12-43), or
// CodePolicy for a rule of this server that tde2e does not have.
type Code int

const (
	// CodeUnknown is a tde2e error without a code of its own, such as a
	// key-value hash mismatch (td::Status::Error without a code becomes
	// UNKNOWN_ERROR, utils.h:29-35).
	CodeUnknown                   Code = 100
	CodeInvalidBlock              Code = 200
	CodeNoChanges                 Code = 201
	CodeInvalidSignature          Code = 202
	CodeHashMismatch              Code = 203
	CodeHeightMismatch            Code = 204
	CodeStateProofGroup           Code = 205
	CodeStateProofSecret          Code = 206
	CodeNoPermissions             Code = 207
	CodeInvalidGroupState         Code = 208
	CodeInvalidSharedSecret       Code = 209
	CodeBroadcastInFuture         Code = 500
	CodeBroadcastNotInCommit      Code = 501
	CodeBroadcastNotInReveal      Code = 502
	CodeBroadcastUnknownUserID    Code = 503
	CodeBroadcastAlreadyApplied   Code = 504
	CodeBroadcastInvalidReveal    Code = 505
	CodeBroadcastInvalidBlockHash Code = 506
	// CodePolicy is a refusal by the server policy of ADR 0060.
	CodePolicy Code = 1000
)

// codeNames are the names of e2e_errors.h:45-106.
var codeNames = map[Code]string{
	CodeUnknown:                   "UNKNOWN_ERROR",
	CodeInvalidBlock:              "INVALID_BLOCK",
	CodeNoChanges:                 "INVALID_BLOCK__NO_CHANGES",
	CodeInvalidSignature:          "INVALID_BLOCK__INVALID_SIGNATURE",
	CodeHashMismatch:              "INVALID_BLOCK__HASH_MISMATCH",
	CodeHeightMismatch:            "INVALID_BLOCK__HEIGHT_MISMATCH",
	CodeStateProofGroup:           "INVALID_BLOCK__INVALID_STATE_PROOF__GROUP",
	CodeStateProofSecret:          "INVALID_BLOCK__INVALID_STATE_PROOF__SECRET",
	CodeNoPermissions:             "INVALID_BLOCK__NO_PERMISSIONS",
	CodeInvalidGroupState:         "INVALID_BLOCK__INVALID_GROUP_STATE",
	CodeInvalidSharedSecret:       "INVALID_BLOCK__INVALID_SHARED_SECRET",
	CodeBroadcastInFuture:         "INVALID_BROADCAST__IN_FUTURE",
	CodeBroadcastNotInCommit:      "INVALID_BROADCAST__NOT_IN_COMMIT",
	CodeBroadcastNotInReveal:      "INVALID_BROADCAST__NOT_IN_REVEAL",
	CodeBroadcastUnknownUserID:    "INVALID_BROADCAST__UNKNOWN_USER_ID",
	CodeBroadcastAlreadyApplied:   "INVALID_BROADCAST__ALREADY_APPLIED",
	CodeBroadcastInvalidReveal:    "INVALID_BROADCAST__INVALID_REVEAL",
	CodeBroadcastInvalidBlockHash: "INVALID_BROADCAST__INVALID_BLOCK_HASH",
	CodePolicy:                    "SERVER_POLICY",
}

func (c Code) String() string {
	if s, ok := codeNames[c]; ok {
		return s
	}
	return "UNKNOWN_ERROR"
}

// Error is a refused block or broadcast. Msg never holds key material.
type Error struct {
	Kind Kind
	Code Code
	Msg  string
}

func (e *Error) Error() string {
	if e.Msg == "" {
		return "tde2e: " + e.Code.String()
	}
	return "tde2e: " + e.Code.String() + ": " + e.Msg
}

// KindOf returns the kind of a refusal; an error that is not an *Error is
// KindInvalid.
func KindOf(err error) Kind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return KindInvalid
}

// fail is a refusal of tde2e's rules: the server answers BLOCK_INVALID.
func fail(code Code, msg string) *Error {
	return &Error{Kind: KindInvalid, Code: code, Msg: msg}
}

// policy is a refusal of the server policy of ADR 0060.
func policy(msg string) *Error {
	return &Error{Kind: KindInvalid, Code: CodePolicy, Msg: msg}
}
