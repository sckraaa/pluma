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

package conformance

import (
	"os"
	"strings"
	"sync"
)

// device is how tglogin and the recordings describe themselves to Telegram
// in initConnection: the computer they run on, as an official app does.
// Telegram shows it in the sessions of the account. A made-up description
// (system "go", a language without a region) is one of the suspects when
// Telegram does not deliver a login code to a third-party app
// (docs/ops/testing.md).
type device struct {
	model          string // Mac14,2
	system         string // macOS 15.4
	systemLangCode string // de-DE
	langCode       string // ru
}

var hostDevice = sync.OnceValue(func() device {
	model, system := hostModel()
	tag := systemLocale()
	lang, _, _ := strings.Cut(tag, "-")
	return device{model: model, system: system, systemLangCode: tag, langCode: lang}
})

// systemLocale is the locale of the system as a language tag: ru-RU.
func systemLocale() string {
	for _, v := range []string{os.Getenv("LC_ALL"), os.Getenv("LC_MESSAGES"), os.Getenv("LANG"), platformLocale()} {
		if tag := localeTag(v); tag != "" {
			return tag
		}
	}
	return "en-US"
}

// localeTag turns a POSIX or Apple locale (ru_RU.UTF-8, en_US@rg=dezzzz)
// into a language tag (ru-RU, en-US); "" for none or the C locale.
func localeTag(locale string) string {
	locale, _, _ = strings.Cut(locale, ".")
	locale, _, _ = strings.Cut(locale, "@")
	if locale == "C" || locale == "POSIX" {
		return ""
	}
	return strings.ReplaceAll(locale, "_", "-")
}
