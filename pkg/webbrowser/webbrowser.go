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

// Package webbrowser keeps the in-app browser settings of an account
// (account.webBrowserSettings, task 098): whether links open in the
// external browser, whether the in-app one shows a close button, and two
// lists of domains that are exceptions to the first flag.
//
// Android and iOS read the settings on start and every hour with the hash
// of their copy (Android MessagesController.java:25392-25419, iOS
// TelegramCore/Sources/State/ManagedWebBrowserSettingsUpdates.swift:7-15),
// so the hash is made of the content alone: the same settings have the
// same hash, after a restart too.
package webbrowser

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/idna"
)

const (
	// MaxExceptions is the size of a list. Android checks it before it adds
	// one: 100, 5 on the test backend (MessagesController.java:25378-25383).
	MaxExceptions = 100
	// MaxDomainLength is the longest host name (RFC 1035), in its ASCII
	// form.
	MaxDomainLength = 253
	// MaxURLLength is the longest address an exception keeps, in
	// characters (the column user_web_browser_exceptions.url).
	MaxURLLength = 1024
)

var (
	// ErrURLInvalid is an address with no host, or a host too long.
	ErrURLInvalid = errors.New("webbrowser: invalid url")
	// ErrLimit is an exception that would make its list longer than
	// MaxExceptions.
	ErrLimit = errors.New("webbrowser: too many exceptions")
)

// uriParts is the expression of RFC 3986, appendix B: 2 the scheme, 3 the
// authority with "//", 4 the authority.
var uriParts = regexp.MustCompile(`^(([^:/?#]+):)?(//([^/?#]*))?([^?#]*)(\?([^#]*))?(#(.*))?$`)

// Exception is a domain in one of the lists and the address it was made
// from.
type Exception struct {
	Domain string
	URL    string
}

// Settings are the settings of an account. The lists are in the order the
// exceptions were made, the oldest first, as clients append them
// (Android MessagesController.java:25672-25700, iOS
// AccountWebBrowserSettings.swift:86-94).
type Settings struct {
	OpenExternal       bool
	DisplayCloseButton bool
	External           []Exception
	InApp              []Exception
}

// Defaults are the settings of an account that never changed them: the
// in-app browser with its close button. iOS assumes the in-app browser
// without the server's answer (AccountWebBrowserSettings.swift:33-35);
// Android without settings uses Custom Tabs, which have the close button
// (MessagesController.java:25361-25362).
func Defaults() Settings {
	return Settings{OpenExternal: false, DisplayCloseButton: true}
}

// Store keeps the settings. A write of the exceptions of one account is
// serialized with the others of that account.
type Store interface {
	// Get returns the settings of userId; Defaults for an account with
	// none.
	Get(ctx context.Context, userId int64) (Settings, error)
	// SetFlags sets the two flags of userId; the lists stay.
	SetFlags(ctx context.Context, userId int64, openExternal, displayCloseButton bool) error
	// PutException puts e at the end of the external list (external) or of
	// the in-app one, out of the other: a domain is in one list. The
	// address of a domain already there is replaced. ErrLimit, and nothing
	// changes, if the list would be longer than MaxExceptions.
	PutException(ctx context.Context, userId int64, e Exception, external bool) error
	// DeleteException removes domain from the list *external, or from
	// either for a nil one; deleted is false if it was in none of them.
	DeleteException(ctx context.Context, userId int64, domain string, external *bool) (deleted bool, err error)
	// DeleteAllExceptions empties both lists and returns what they had;
	// the flags stay.
	DeleteAllExceptions(ctx context.Context, userId int64) (removed Settings, err error)
}

// Hash is the hash of s that account.getWebBrowserSettings compares with
// the client's: the first 8 bytes of SHA-256 of the flags and of the
// lists in order (length-prefixed domains and addresses), positive. 0,
// which clients send without a copy, is never a hash, so they always get
// the settings.
func Hash(s Settings) int64 {
	h := sha256.New()
	var flags byte
	if s.OpenExternal {
		flags |= 1
	}
	if s.DisplayCloseButton {
		flags |= 2
	}
	h.Write([]byte{flags})
	var n [8]byte
	writeString := func(v string) {
		binary.BigEndian.PutUint64(n[:], uint64(len(v)))
		h.Write(n[:])
		h.Write([]byte(v))
	}
	for _, list := range [][]Exception{s.External, s.InApp} {
		binary.BigEndian.PutUint64(n[:], uint64(len(list)))
		h.Write(n[:])
		for _, e := range list {
			writeString(e.Domain)
			writeString(e.URL)
		}
	}
	v := int64(binary.BigEndian.Uint64(h.Sum(nil)[:8]) & (1<<63 - 1))
	if v == 0 {
		v = 1
	}
	return v
}

// NormalizeURL returns the exception of an address a client sends: an
// address with or without a scheme, or a bare domain, which Android sends
// (ui/ArticleViewer.java:4864-4870); an address without a scheme is read
// as "//" and the address, as Android reads it when it opens a link
// (MessagesController.java:25601, 25611).
//
// The domain is the host in lower case, without the user, the port and a
// final dot. A host in Unicode stays in Unicode: Android matches the
// exceptions with the host as the link has it, in lower case
// (AndroidUtilities.java:6240-6257, MessagesController.java:25622-25631),
// so an exception it makes for a Unicode host only works if it comes back
// as it was sent. Its ASCII (punycode) form is what is checked against
// MaxDomainLength.
//
// The address is kept as sent, without the spaces around. One longer than
// MaxURLLength is kept as its domain, the form Android sends itself: iOS
// sends the address of the link it opens (TelegramUI/Sources/Chat/
// ChatControllerOpenLinkContextMenu.swift:195) and repeats the request
// after any error without end (TelegramCore/Sources/Network/
// Network.swift:1513-1525), and both clients match an exception by its
// domain (Android tgnet/tl/TL_account.java:4401-4409, iOS
// AccountWebBrowserSettings.swift:66-72), so a long link must not be
// refused.
func NormalizeURL(raw string) (Exception, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || !utf8.ValidString(raw) {
		return Exception{}, ErrURLInvalid
	}
	// The authority as RFC 3986, appendix B, splits it (the expression
	// Android uses, AndroidUtilities.java:6229-6257); none: read again
	// after "//".
	m := uriParts.FindStringSubmatch(raw)
	if m == nil || m[3] == "" {
		if m = uriParts.FindStringSubmatch("//" + raw); m == nil {
			return Exception{}, ErrURLInvalid
		}
	}
	u, err := url.Parse("//" + m[4])
	if err != nil {
		return Exception{}, ErrURLInvalid
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "" {
		return Exception{}, ErrURLInvalid
	}
	ascii, err := idna.Punycode.ToASCII(host)
	if err != nil || len(ascii) > MaxDomainLength {
		return Exception{}, ErrURLInvalid
	}
	if utf8.RuneCountInString(raw) > MaxURLLength {
		raw = host
	}
	return Exception{Domain: host, URL: raw}, nil
}
