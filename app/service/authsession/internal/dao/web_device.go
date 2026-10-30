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

package dao

import "strings"

// The device of a web app's session (task 019).
//
// Both web apps send the browser's user agent as device_model: Web K
// navigator.userAgent, unless the user named the device in its settings
// (tweb 346844bf src/lib/appManagers/networkerFactory.ts:58,
// src/environment/userAgent.ts:3), Web A navigator.userAgent (telegram-tt
// 6002824b src/api/gramjs/methods/client.ts:120). Their lang_pack is "webk"
// (tweb src/config/app.ts:25) and "weba" (telegram-tt src/config.ts:380).
// Telegram showed a session of Web A 12.0.48 in Safari 26 on macOS as
// "Safari 26", in its list of devices and in the New login message
// ("Telegram Web, 12.0.48 A, Safari 26, macOS", 2026-10-29,
// docs/tasks/019-service-messages.md): the browser and its major version,
// not the user agent. That is one observation, of one browser.
//
// As system_version Web A sends the system it derives from
// navigator.platform, "macOS" (telegram-tt
// src/util/browser/windowEnvironment.ts:13-34,
// src/global/actions/api/initial.ts:99, client.ts:121), Web K
// navigator.platform itself (networkerFactory.ts:59).

// deviceModel is the device_model of a session as account.getAuthorizations
// shows it, and from there updateNewAuthorization and the New login
// message: for a web app the browser, named by webBrowser, when
// device_model is a user agent it knows; anything else as initConnection
// said it. What initConnection said stays stored as it came.
func deviceModel(langPack, model string) string {
	switch langPack {
	case "weba", "webk":
		if browser, ok := webBrowser(model); ok {
			return browser
		}
	}
	return model
}

// systemVersion is the system_version of a session as
// account.getAuthorizations shows it: Web K's navigator.platform named by
// webSystem; anything else as initConnection said it. Web A names its
// system itself.
func systemVersion(langPack, system, model string) string {
	if langPack == "webk" {
		return webSystem(system, model)
	}
	return system
}

// webBrowsers are the browsers webBrowser knows by a product token of their
// user agents ("Edg/143.0.0.0"), in the order it looks for them: Edge and
// Opera send Chrome's token too, and all of them Safari's (MDN, User-Agent
// header reference, sections "Chrome UA string" to "Microsoft Edge UA
// string"; Firefox user agent string reference, "Firefox for iOS"; Browser
// detection using the user agent, "Browser name and version"; read
// 2026-10-30). Only Safari's name was seen in Telegram; these follow its
// pattern and are not verified.
//
// The names are the words the official clients choose a browser's icon by
// in device_model. Android looks for "safari", "edge", "chrome", "opera",
// "firefox", "vivaldi", in that order (DrKLO/Telegram aac1efcb
// ui/Cells/SessionCell.java:324-350), so in a whole user agent it found
// Safari for every Chromium browser; iOS for "chrome", "brave", "vivaldi",
// "safari", "firefox", "opera" (Telegram-iOS 6ad963e5
// ItemListRecentSessionItem.swift:126-143).
// Desktop knows Edge only by its token, "edg/", and has no Opera icon
// (tdesktop a1e89e1f settings/sections/settings_active_sessions.cpp:179-191).
// So Edge on Desktop and iOS, and Opera on Desktop, now get the icon of the
// system (settings_active_sessions.cpp:227,
// ItemListRecentSessionItem.swift:153-167), where the whole user agent gave
// them Edge's or Chrome's.
var webBrowsers = []struct {
	name     string
	products []string
}{
	{"Edge", []string{"Edg", "EdgA", "EdgiOS"}},
	{"Opera", []string{"OPR", "OPT"}},
	{"Firefox", []string{"Firefox", "FxiOS"}},
	{"Chrome", []string{"Chrome", "CriOS"}},
}

// webBrowser names the browser of a user agent with its major version, as
// Telegram named Safari 26: "Safari 26", "Chrome 141". ok is false for a
// string that is no browser's user agent, such as a name the user gave the
// device in Web K, and for a browser it does not know.
func webBrowser(userAgent string) (name string, ok bool) {
	products, ok := userAgentProducts(userAgent)
	if !ok {
		return "", false
	}
	for _, b := range webBrowsers {
		for _, p := range b.products {
			if version, found := products[p]; found {
				return withMajor(b.name, version)
			}
		}
	}
	// Safari tells its release in Version/ and the build of its engine in
	// Safari/ (MDN, "Safari UA string"). Android's own WebKit browser sent
	// both as well ("Android 4.0.3 ... Version/4.0 Mobile Safari/534.30",
	// MDN browser detection guide, "Mobile, Tablet or Desktop"), and is not
	// Safari.
	if _, found := products["Safari"]; found && !strings.Contains(userAgent, "Android") {
		if version, found := products["Version"]; found {
			return withMajor("Safari", version)
		}
	}
	return "", false
}

// userAgentProducts returns the products of a browser's user agent, the
// name of each to its version ("Chrome" to "141.0.0.0"), the first one of a
// name. ok is false for a string that does not start as almost every
// browser's user agent does, with "Mozilla/5.0" (MDN, User-Agent header
// reference, "Firefox UA string").
func userAgentProducts(userAgent string) (products map[string]string, ok bool) {
	if !strings.HasPrefix(userAgent, "Mozilla/5.0 ") {
		return nil, false
	}
	products = make(map[string]string)
	for _, field := range strings.Fields(userAgent) {
		if name, version, found := strings.Cut(field, "/"); found {
			if _, seen := products[name]; !seen {
				products[name] = version
			}
		}
	}
	return products, true
}

// withMajor is name and the major number of version: "Safari" and "26.0.1"
// give "Safari 26". ok is false for a version that does not start with a
// number.
func withMajor(name, version string) (string, bool) {
	n := 0
	for n < len(version) && version[n] >= '0' && version[n] <= '9' {
		n++
	}
	if n == 0 {
		return "", false
	}
	return name + " " + version[:n], true
}

// webSystem names the system of a Web K session by its navigator.platform
// as Web A names it by the same value (telegram-tt 6002824b
// src/util/browser/windowEnvironment.ts:13-34), so that a session of either
// app reads "macOS" in the list of devices and in the New login message.
// Web A tells an iPad that says "MacIntel" by navigator.maxTouchPoints
// (19-20), which the server does not get: such an iPad reads macOS (tweb
// 346844bf src/environment/userAgent.ts:15-16 knows the same of iPads).
// Web A finds Android in the user agent before Linux in navigator.platform
// (29-31); so does webSystem, when device_model is a user agent. A value
// Web A does not know stays as it is.
func webSystem(platform, userAgent string) string {
	switch platform {
	case "iPhone", "iPad", "iPod":
		return "iOS"
	case "Macintosh", "MacIntel", "MacPPC", "Mac68K":
		return "macOS"
	case "Win32", "Win64", "Windows", "WinCE":
		return "Windows"
	}
	if _, ok := userAgentProducts(userAgent); ok && strings.Contains(userAgent, "Android") {
		return "Android"
	}
	if strings.Contains(platform, "Linux") {
		return "Linux"
	}
	return platform
}
