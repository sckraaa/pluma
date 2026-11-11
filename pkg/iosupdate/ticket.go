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

// Package iosupdate lets an Ad Hoc build of the iPhone app update from the
// app itself (task 085, the addition of 2026-11-11 to ADR 0035).
// help.getAppUpdate gives such a build an itms-services link, which iOS
// opens as it opens any url of help.appUpdate (the app hands it to
// UIApplication.open, AppDelegate.swift:845-859 of Telegram-iOS at
// release-13.2, 24638bc69b; the url reaches it by the patches I-099 and
// I-100 of clients/patches/ios.md); the manifest and the IPA behind it are served
// by the install service (app/ops/iosinstall) for an update ticket: a
// credential the bff mints and the install service checks with a secret
// both hold. The device tokens of the install page are not known to the
// bff.
package iosupdate

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"net/url"
	"time"
)

// MinSecret is the least length of the shared secret (openssl rand -hex 32
// gives 64 characters).
const MinSecret = 32

// Param is the query parameter of the manifest and of the IPA that carries
// the ticket; the device token of the install page is "t".
const Param = "u"

// day is the bucket of a ticket: tickets of one UTC day are the same.
const day = 24 * time.Hour

const (
	version    = 1
	bodySize   = 1 + 4 + 4 + 8
	ticketSize = bodySize + sha256.Size
)

var (
	// ErrInvalid: not a ticket of this secret.
	ErrInvalid = errors.New("iosupdate: the ticket is not valid")
	// ErrExpired: a ticket of this secret whose days have passed, or of a
	// day to come.
	ErrExpired = errors.New("iosupdate: the ticket has expired")
)

// Ticket is what a valid ticket says.
type Ticket struct {
	// Build is the build the ticket gives.
	Build int32
	// Day is the UTC day it was minted on, counted from 1970.
	Day uint32
	// Subject names the session the ticket was given to: a keyed hash of
	// its auth key, the same in every ticket of the session. It tells the
	// tickets of two sessions apart and cannot be turned back into the key.
	Subject [8]byte
}

// ValidSecret tells whether s can be the shared secret.
func ValidSecret(s string) bool { return len(s) >= MinSecret }

func mac(secret []byte, label string, data []byte) []byte {
	h := hmac.New(sha256.New, secret)
	h.Write([]byte(label))
	h.Write([]byte{0})
	h.Write(data)
	return h.Sum(nil)
}

func dayOf(t time.Time) uint32 {
	return uint32(t.Unix() / int64(day/time.Second))
}

// Mint is the ticket of build for the session of authKeyID at now. It is
// the same for the whole UTC day: the iPhone app shows the update screen
// again whenever the answer of help.getAppUpdate differs from the one it
// has (ApplicationContext.swift:589-599 of Telegram-iOS at release-13.2,
// 24638bc69b: AppUpdateInfo is Equatable, AppUpdate.swift:8, and the patch
// I-099 adds the url to its fields), so a ticket that changed on every
// request would bring the screen back after "Skip". Verify takes it on its
// day and on the next one: a screen shown just before midnight still works,
// and a ticket lives from 24 to 48 hours.
func Mint(secret []byte, build int32, authKeyID int64, now time.Time) string {
	var key [8]byte
	binary.BigEndian.PutUint64(key[:], uint64(authKeyID))
	b := make([]byte, bodySize, ticketSize)
	b[0] = version
	binary.BigEndian.PutUint32(b[1:], uint32(build))
	binary.BigEndian.PutUint32(b[5:], dayOf(now))
	copy(b[9:], mac(secret, "pluma ios update subject", key[:]))
	b = append(b, mac(secret, "pluma ios update ticket", b)...)
	return base64.RawURLEncoding.EncodeToString(b)
}

// Verify reads a ticket of secret at now: ErrInvalid for anything that
// secret did not sign, ErrExpired for a ticket older than the day before
// or of a day to come. The caller checks that the build is the one it
// serves.
func Verify(secret []byte, ticket string, now time.Time) (Ticket, error) {
	var t Ticket
	// A ticket is 66 characters; nothing longer is decoded.
	if len(ticket) != base64.RawURLEncoding.EncodedLen(ticketSize) {
		return t, ErrInvalid
	}
	b, err := base64.RawURLEncoding.DecodeString(ticket)
	if err != nil || len(b) != ticketSize || b[0] != version {
		return t, ErrInvalid
	}
	if !hmac.Equal(b[bodySize:], mac(secret, "pluma ios update ticket", b[:bodySize])) {
		return t, ErrInvalid
	}
	build := binary.BigEndian.Uint32(b[1:])
	if build == 0 || build > 1<<31-1 {
		return t, ErrInvalid
	}
	t.Build = int32(build)
	t.Day = binary.BigEndian.Uint32(b[5:])
	copy(t.Subject[:], b[9:])
	if today := dayOf(now); t.Day > today || today-t.Day > 1 {
		return t, ErrExpired
	}
	return t, nil
}

// InstallURL is the link that makes iOS install the app of the manifest
// at manifestURL (https) for ticket.
func InstallURL(manifestURL, ticket string) string {
	return "itms-services://?action=download-manifest&url=" + url.QueryEscape(manifestURL+"?"+Param+"="+ticket)
}
