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

package urlauth

import "strings"

// browserTokens are the products of a User-Agent by which browsers name
// themselves, the most specific first: Edge, Opera, Yandex and Samsung
// Internet also carry "Chrome/", and Chrome carries "Safari/".
var browserTokens = []struct{ token, name string }{
	{"EdgA/", "Edge"},
	{"EdgiOS/", "Edge"},
	{"Edg/", "Edge"},
	{"OPR/", "Opera"},
	{"YaBrowser/", "Yandex Browser"},
	{"SamsungBrowser/", "Samsung Internet"},
	{"FxiOS/", "Firefox"},
	{"Firefox/", "Firefox"},
	{"CriOS/", "Chrome"},
	{"Chromium/", "Chromium"},
	{"Chrome/", "Chrome"},
}

// platformTokens name the system of a User-Agent; iPhone and iPad go
// before "Mac OS X", which they carry too, and Android before Linux.
var platformTokens = []struct{ token, name string }{
	{"iPhone", "iOS"},
	{"iPad", "iPadOS"},
	{"Android", "Android"},
	{"CrOS", "ChromeOS"},
	{"Windows", "Windows"},
	{"Macintosh", "macOS"},
	{"Mac OS X", "macOS"},
	{"Linux", "Linux"},
}

// UserAgent splits the User-Agent of a site's visitor into the browser with
// its major version ("Firefox 121") and the platform ("Windows"), as an app
// shows them in the login request (urlAuthResultRequest.browser and
// platform) and in the list of sites logged in on (webAuthorization). How
// Telegram itself words them is not known; a part it cannot tell is "".
func UserAgent(ua string) (browser, platform string) {
	for _, p := range platformTokens {
		if strings.Contains(ua, p.token) {
			platform = p.name
			break
		}
	}
	for _, b := range browserTokens {
		if v, ok := productVersion(ua, b.token); ok {
			return joinVersion(b.name, v), platform
		}
	}
	// Safari names its version in Version/ and itself only in Safari/.
	if strings.Contains(ua, "Safari/") {
		v, _ := productVersion(ua, "Version/")
		return joinVersion("Safari", v), platform
	}
	return "", platform
}

// productVersion is the major version after token in ua.
func productVersion(ua, token string) (string, bool) {
	i := strings.Index(ua, token)
	if i < 0 {
		return "", false
	}
	rest := ua[i+len(token):]
	n := 0
	for n < len(rest) && n < 8 && rest[n] >= '0' && rest[n] <= '9' {
		n++
	}
	return rest[:n], true
}

func joinVersion(name, version string) string {
	if version == "" {
		return name
	}
	return name + " " + version
}
