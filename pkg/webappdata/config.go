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

package webappdata

import "strings"

// Config is the WebApp section of the bff config (task 106, ADR 0068).
type Config struct {
	// Versions is the tgWebAppVersion given to a page by the platform of
	// the client that opens it.
	Versions Versions `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// SigningKey is WEBAPP_SIGNING_KEY, the Ed25519 seed of the signature
	// of initData in hex. Empty: no mini app opens (BOT_WEBVIEW_DISABLED).
	SigningKey string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}

// Signer returns the Signer of the config: nil, without an error, when no
// key is set; an error for a key that is not 64 hex digits, which should
// stop the service.
func (c Config) Signer() (*Signer, error) {
	if strings.TrimSpace(c.SigningKey) == "" {
		return nil, nil
	}
	return NewSigner(c.SigningKey)
}

// defaultVersion is the version until the real ones are taken from Telegram
// (task 106, "Не проверено"): tweb rewrites exactly tgWebAppVersion=8.0 of
// the address it gets to 9.0 (tweb src/components/webApp.tsx:252-254), so
// Telegram gives Web K that version.
const defaultVersion = "8.0"

// Versions are the tgWebAppVersion of each platform a client names in
// requestWebView and its kin: "android" (Android ui/bots/BotWebViewSheet.java,
// req.platform), "ios" and "macos" (iOS TelegramCore BotWebView.swift:8-10),
// "tdesktop" (inline_bots/bot_attach_web_view.cpp:1273), "web" (tweb
// appAttachMenuBotsManager.ts:177), and "weba", "webk" that Telegram's own
// pages name. Unknown is for any other platform.
type Versions struct {
	Android  string `json:",default=8.0"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	IOS      string `json:",default=8.0"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	MacOS    string `json:",default=8.0"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	TDesktop string `json:",default=8.0"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	Web      string `json:",default=8.0"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	WebA     string `json:",default=8.0"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	WebK     string `json:",default=8.0"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	Unknown  string `json:",default=8.0"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}

// For returns the version of platform, as a client sends it.
func (v Versions) For(platform string) string {
	var got string
	switch strings.ToLower(platform) {
	case "android":
		got = v.Android
	case "ios":
		got = v.IOS
	case "macos":
		got = v.MacOS
	case "tdesktop":
		got = v.TDesktop
	case "web":
		got = v.Web
	case "weba":
		got = v.WebA
	case "webk":
		got = v.WebK
	default:
		got = v.Unknown
	}
	if got == "" {
		if v.Unknown != "" {
			return v.Unknown
		}
		return defaultVersion
	}
	return got
}
