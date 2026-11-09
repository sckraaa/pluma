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

// Package secretchat keeps the secret chats (task 069). The server relays
// ciphertext and public Diffie-Hellman values: it holds neither a key nor a
// message text, and nothing here may log the payloads.
//
// A chat belongs to one authorization of each side, not to the accounts
// (core.telegram.org/api/end-to-end): the request reaches every
// authorization of the participant, the first to accept keeps the chat and
// the others are told it is discarded. Each authorization has its own qts
// and its own queue of messages, kept until it confirms them
// (messages.receivedQueue, or the qts of updates.getDifference) and at most
// 7 days.
package secretchat

import (
	"encoding/hex"
	"errors"
	"math/big"

	"github.com/teamgram/proto/mtproto"
)

// States of a chat.
const (
	StateRequested int8 = 1
	StateActive    int8 = 2
	StateDiscarded int8 = 3
)

// Errors of the store; bff maps them to the RPC errors of the methods.
var (
	ErrChatNotFound    = errors.New("secretchat: no such chat")
	ErrWrongDevice     = errors.New("secretchat: the chat belongs to another authorization")
	ErrDeclined        = errors.New("secretchat: the chat is discarded")
	ErrAlreadyAccepted = errors.New("secretchat: the chat was already accepted")
	ErrAlreadyDeclined = errors.New("secretchat: the chat was already declined")
	ErrRandomIdUsed    = errors.New("secretchat: the random id was used for another request")
	ErrRequestFlood    = errors.New("secretchat: too many requests in the last hour")
	ErrQueueFull       = errors.New("secretchat: the queue of the recipient is full")
)

// DHVersion is the version of messages.dhConfig. It changes with the prime.
const DHVersion int32 = 1

// DHG is the generator of DHPrime.
const DHG int32 = 3

// DHPrime is the safe prime of the key exchange: the one the official
// clients accept without a primality check (goodPrime in tgnet/Handshake.cpp
// of DrKLO/Telegram) and this server's own handshake uses.
var DHPrime = mustHex("c71caeb9c6b1c9048e6c522f70f13f73980d40238e3e21c14934d037563d930f" +
	"48198a0aa7c14058229493d22530f4dbfa336f6e0ac925139543aed44cce7c37" +
	"20fd51f69458705ac68cd4fe6b6b13abdc9746512969328454f18faf8c595f64" +
	"2477fe96bb2a941d5bcd1d4ac8cc49880708fa9b378e3c4f3a9060bee67cf9a4" +
	"a4a695811051907e162753b56b0f6b410dba74d8a84b2a14b3144e0ef1284754" +
	"fd17ed950d5965b4b9dd46582db1178d169c6bc465b0d6ff9ca3928fef5b9ae4" +
	"e418fc15e83ebea0f87fa9ff5eed70050ded2849f47bf959d956850ce929851f" +
	"0d8115f635b105ee2e4e15d04b2454bf6f4fadf034b10403119cd8e3b92fcc5b")

var (
	dhPrimeInt = new(big.Int).SetBytes(DHPrime)
	dhLow      = new(big.Int).Lsh(big.NewInt(1), 2048-64)
	dhHigh     = new(big.Int).Sub(dhPrimeInt, dhLow)
)

func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

// ValidDH tells whether value is a public value the other side will accept:
// at most 2048 bits and 2^(2048-64) < value < p - 2^(2048-64)
// (core.telegram.org/api/end-to-end).
func ValidDH(value []byte) bool {
	if len(value) == 0 || len(value) > 256 {
		return false
	}
	v := new(big.Int).SetBytes(value)
	return v.Cmp(dhLow) > 0 && v.Cmp(dhHigh) < 0
}

// Chat is a row of encrypted_chats.
type Chat struct {
	Id                 int32
	AccessHash         int64
	AdminId            int64
	AdminAuthKey       int64
	ParticipantId      int64
	ParticipantAuthKey int64 // 0 until the chat is accepted
	RandomId           int32
	GA, GB             []byte
	KeyFingerprint     int64
	State              int8
	HistoryDeleted     bool
	DiscardedBy        int64 // the authorization that discarded the chat, 0 for the server
	Date               int32
	StateAt            int32
	AdminRead          int32
	AdminReadAt        int32
	ParticipantRead    int32
	ParticipantReadAt  int32
}

// Device is one authorization of a user.
type Device struct {
	UserId  int64
	AuthKey int64
}

// View returns the chat as the authorization d sees it, encryptedChatEmpty
// if it is not d's. The participant's other authorizations see an accepted
// chat as discarded.
func (c *Chat) View(d Device) *mtproto.EncryptedChat {
	discarded := func(deleted bool) *mtproto.EncryptedChat {
		return mtproto.MakeTLEncryptedChatDiscarded(&mtproto.EncryptedChat{Id: c.Id, HistoryDeleted: deleted}).To_EncryptedChat()
	}
	admin := c.AdminId == d.UserId && c.AdminAuthKey == d.AuthKey
	participant := c.ParticipantId == d.UserId
	if !admin && !participant {
		return mtproto.MakeTLEncryptedChatEmpty(&mtproto.EncryptedChat{Id: c.Id}).To_EncryptedChat()
	}
	switch c.State {
	case StateRequested:
		if admin {
			return mtproto.MakeTLEncryptedChatWaiting(&mtproto.EncryptedChat{
				Id: c.Id, AccessHash: c.AccessHash, Date: c.Date, AdminId: c.AdminId, ParticipantId: c.ParticipantId,
			}).To_EncryptedChat()
		}
		return mtproto.MakeTLEncryptedChatRequested(&mtproto.EncryptedChat{
			Id: c.Id, AccessHash: c.AccessHash, Date: c.Date, AdminId: c.AdminId, ParticipantId: c.ParticipantId, GA: c.GA,
		}).To_EncryptedChat()
	case StateActive:
		switch {
		case admin:
			return c.active(c.GB)
		case c.ParticipantAuthKey == d.AuthKey:
			return c.active(c.GA)
		}
		return discarded(false)
	}
	// The side that discarded has already dropped its copy; the
	// participant's other authorizations never had the history.
	own := admin || c.ParticipantAuthKey == d.AuthKey
	return discarded(c.HistoryDeleted && own && c.DiscardedBy != d.AuthKey)
}

// active is encryptedChat with the value of the other side: "B = g ^ b mod
// p, if the currently authorized user is the chat's creator, or A = g ^ a
// mod p otherwise" (core.telegram.org/constructor/encryptedChat).
func (c *Chat) active(gAOrB []byte) *mtproto.EncryptedChat {
	return mtproto.MakeTLEncryptedChat(&mtproto.EncryptedChat{
		Id: c.Id, AccessHash: c.AccessHash, Date: c.Date, AdminId: c.AdminId, ParticipantId: c.ParticipantId,
		GAOrB: gAOrB, KeyFingerprint: c.KeyFingerprint,
	}).To_EncryptedChat()
}

// UpdateEncryption is updateEncryption with the view of d.
func (c *Chat) UpdateEncryption(d Device, date int32) *mtproto.Update {
	return mtproto.MakeTLUpdateEncryption(&mtproto.Update{Chat: c.View(d), Date_INT32: date}).To_Update()
}

// File is a row of encrypted_files.
type File struct {
	Id             int64
	AccessHash     int64
	Size           int64
	DcId           int32
	KeyFingerprint int32
}

// Encrypted is f as an encryptedFile, encryptedFileEmpty for nil.
func (f *File) Encrypted() *mtproto.EncryptedFile {
	if f == nil {
		return mtproto.MakeTLEncryptedFileEmpty(nil).To_EncryptedFile()
	}
	return mtproto.MakeTLEncryptedFile(&mtproto.EncryptedFile{
		Id:             f.Id,
		AccessHash:     f.AccessHash,
		Size2_INT64:    f.Size,
		DcId:           f.DcId,
		KeyFingerprint: f.KeyFingerprint,
	}).To_EncryptedFile()
}

// QueuedMessage is one row of an authorization's queue.
type QueuedMessage struct {
	Qts      int32
	ChatId   int32
	RandomId int64
	Date     int32
	Service  bool
	Data     []byte
	File     *File
}

// Message is the row as encryptedMessage or encryptedMessageService.
func (m *QueuedMessage) Message() *mtproto.EncryptedMessage {
	if m.Service {
		return mtproto.MakeTLEncryptedMessageService(&mtproto.EncryptedMessage{
			RandomId: m.RandomId, ChatId: m.ChatId, Date: m.Date, Bytes: m.Data,
		}).To_EncryptedMessage()
	}
	return mtproto.MakeTLEncryptedMessage(&mtproto.EncryptedMessage{
		RandomId: m.RandomId, ChatId: m.ChatId, Date: m.Date, Bytes: m.Data, File: m.File.Encrypted(),
	}).To_EncryptedMessage()
}

// Update is updateNewEncryptedMessage of the row.
func (m *QueuedMessage) Update() *mtproto.Update {
	return mtproto.MakeTLUpdateNewEncryptedMessage(&mtproto.Update{
		Message_ENCRYPTEDMESSAGE: m.Message(), Qts: m.Qts,
	}).To_Update()
}
