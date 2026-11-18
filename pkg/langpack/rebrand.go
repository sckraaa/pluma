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

package langpack

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The replacement of "Telegram" with "Pluma" in the values of the strings
// follows the client patches (ADR 0025): whole words only, never keys,
// addresses or handles.
//
//   - The word rule of the Android patch (pluma-android ci/patch.py,
//     TELEGRAM_WORD): (?<![A-Za-z0-9@/.])(Telegram|TELEGRAM)(?![A-Za-z0-9]|\.\w)
//     gives "Pluma" and "PLUMA"; "t.me/TelegramTips", "@Telegram",
//     "Telegram.org" and "Telegramm" stay.
//   - "Telegram Desktop" is "Pluma", as in the Desktop patch
//     (pluma-desktop ci/patch.py, name_telegram).
//   - The name in other scripts and in compounds, per language and longest
//     first (Android STRING_LOCAL_NAMES; Persian from the iOS patch,
//     INFOPLIST_NAMES). Korean particles follow the last sound: "Pluma" ends
//     in a vowel.
//   - The links the client patches changed in their built-in English lead to
//     Pluma's site, web client and link host (linkRules), the exceptions to
//     keeping telegram.org and t.me.
//   - Keys whose text names Telegram's API itself keep it (Desktop
//     LANG_KEEP: lng_about_text1_api).
//
//   - Inflected forms ("Telegramu" in Czech) take the same form of Pluma
//     (inflect.go); an ending a language does not list stays, and Leftovers
//     reports it.

const brandName = "Pluma"

// keepKeys are the keys whose values stay as Telegram wrote them.
var keepKeys = map[string]bool{
	"lng_about_text1_api": true, // "Telegram API", the protocol Pluma speaks
}

// isKept reports whether the value of key stays as Telegram wrote it.
func isKept(key string) bool { return keepKeys[key] }

// linkRules rewrite the links of the strings whose links the client patches
// changed in the apps' built-in English, by pack and key, as the patch of
// that client does:
//
//   - android: the QR login's links to the desktop and web clients (Android
//     CLIENT_LINKS) and the public link of an empty group (LINK_STRINGS);
//   - ios: the same QR login text (CLIENT_TEXTS) and the texts that show a
//     username's, a phone number's or a proxy's link (STRINGS_LINKS);
//   - webk: the public links of a group and of the Premium limits
//     (LINK_BUILDERS of lang.ts), and the two texts whose links to
//     Telegram's site the patch took away (TELEGRAM_SITE_EDITS): the links
//     to Telegram's apps keep their words, the "Learn More" link with no
//     address goes.
//
// tdesktop changes no link of its strings.
var linkRules = map[string]map[string]func(string) string{
	"android": {
		"AuthAnotherClientDownloadClientUrl": clientLinks,
		"AuthAnotherWebClientUrl":            clientLinks,
		"AuthAnotherClientInfo1":             clientLinks,
		"AuthAnotherClientInfo5":             clientLinks,
		"GroupDescription3":                  replaceLinkHost,
	},
	"ios": {
		"AuthSessions.AddDevice.ScanInstallInfo":      clientLinks,
		"Username.LinkHint":                           replaceLinkHost,
		"Channel.Username.LinkHint":                   replaceLinkHost,
		"EmptyGroupInfo.Line3":                        replaceLinkHost,
		"PrivacyPhoneNumberSettings.CustomPublicLink": replaceLinkHost,
		"Business.Links.SimpleLinkInfoUsernamePhone":  replaceLinkHost,
		"Business.Links.SimpleLinkInfoUsername":       replaceLinkHost,
		"Business.Links.SimpleLinkInfoPhone":          replaceLinkHost,
		"SocksProxySetup.QrCode.TMeLink":              replaceLinkHost,
	},
	"webk": {
		"PublicLinksLimitSubtitle":                          replaceLinkHost,
		"GroupDescription3":                                 replaceLinkHost,
		"ChatList.Main.EmptyPlaceholder.SubtitleNoContacts": unlinkTelegramSite,
		"ChatAutomation.HeroDescription":                    dropEmptyLink,
	},
}

// clientSite is an address of Telegram's desktop or web client, in any
// case ("Desktop.telegram.org" in the Kazakh pack).
var clientSite = regexp.MustCompile(`(?i)(https://)?(getdesktop\.|desktop\.|web\.)telegram\.org(/)?`)

// clientLinks replaces the addresses of Telegram's desktop and web clients
// with Pluma's site and web client (Android CLIENT_LINKS): a link to the
// site ends with a slash.
func clientLinks(value string) string {
	return clientSite.ReplaceAllStringFunc(value, func(m string) string {
		p := clientSite.FindStringSubmatch(m)
		host := "pluma.chat"
		if strings.EqualFold(p[2], "web.") {
			host = "web.pluma.chat"
		}
		slash := p[3]
		if p[1] != "" && host == "pluma.chat" {
			slash = "/"
		}
		return p[1] + host + slash
	})
}

// linkHost is the host of Pluma's links to users, chats and proxies.
const linkHost = "pluma.chat"

// replaceLinkHost replaces the host t.me, as a whole host, with linkHost:
// the rule of the iOS patch, (?<![0-9A-Za-z_.])t\.me(?![0-9A-Za-z_]).
func replaceLinkHost(value string) string {
	var b strings.Builder
	pos := 0
	for i := 0; ; {
		j := strings.Index(value[i:], "t.me")
		if j < 0 {
			break
		}
		j += i
		end := j + len("t.me")
		i = j + 1
		if j > 0 && (isASCIIAlnum(value[j-1]) || value[j-1] == '_' || value[j-1] == '.') {
			continue
		}
		if end < len(value) && (isASCIIAlnum(value[end]) || value[end] == '_') {
			continue
		}
		b.WriteString(value[pos:j])
		b.WriteString(linkHost)
		pos, i = end, end
	}
	if pos == 0 {
		return value
	}
	b.WriteString(value[pos:])
	return b.String()
}

// telegramSiteAddress is an address of telegram.org in parentheses, the
// target of a Markdown link, with the spaces before it.
var telegramSiteAddress = regexp.MustCompile(`\s*\(https?://(?:[\w-]+\.)*telegram\.org(?:/[^)\s]*)?\)`)

// unlinkTelegramSite keeps the words of the links to telegram.org and drops
// the links: their addresses and the brackets, which some translations do
// not pair ("[Android] (https://telegram.org/android)" in Croatian, an
// unclosed one in French).
func unlinkTelegramSite(value string) string {
	value = telegramSiteAddress.ReplaceAllString(value, "")
	return strings.NewReplacer("[", "", "]", "").Replace(value)
}

// emptyLink is a Markdown link the client fills with an element of its own
// ("[Learn More >]()"), with the spaces before it.
var emptyLink = regexp.MustCompile(`\s*\[[^\]]*\]\(\)`)

// dropEmptyLink removes the links the client would fill.
func dropEmptyLink(value string) string {
	return emptyLink.ReplaceAllString(value, "")
}

// sentenceRewrites are sentences that would turn false with the name
// changed (Android STRING_REWRITES).
var sentenceRewrites = strings.NewReplacer(
	"e.g., all videos that were sent from Android using Telegram 4.7 and earlier versions.",
	"e.g., videos that were sent from very old versions of the app.",
)

// localNames are the name in other scripts and in compounds by language
// code, longest first.
var localNames = map[string]*strings.Replacer{
	"ar": strings.NewReplacer(
		"لتيليجرام", "لـPluma",
		"تيليجرام", "Pluma",
		"تليجرام", "Pluma",
	),
	"fa": strings.NewReplacer(
		"تلگرام", "Pluma",
	),
	"ko": strings.NewReplacer(
		"텔레그램이라는", "Pluma라는",
		"텔레그램이", "Pluma가",
		"텔레그램으로", "Pluma로",
		"텔레그램은", "Pluma는",
		"텔레그램을", "Pluma를",
		"텔레그램", "Pluma",
	),
	"nl": strings.NewReplacer(
		"Telegramberichten", "Pluma-berichten",
	),
	// The name in Cyrillic, rare in these packs, inflected as in inflect.go.
	"sr": strings.NewReplacer(
		"Телеграмом", "Plumom",
		"Телеграма", "Plume",
		"Телеграму", "Plumi",
		"Телеграм", "Pluma",
	),
	"kk": strings.NewReplacer(
		"Телеграмды", "Pluma-ны",
		"Телеграм", "Pluma",
	),
	// Search keywords of settings ("Телеграм не Фейсбук", "Значок
	// телеграму"), where the Ukrainian packs leave the Latin name
	// uninflected.
	"uk": strings.NewReplacer(
		"Телеграму", "Pluma",
		"телеграму", "Pluma",
		"Телеграм", "Pluma",
	),
}

// Rebrand returns s of pack with "Telegram" replaced in its texts for
// language lang. Keys never change.
func Rebrand(pack, lang string, s String) String {
	r := s
	r.Value = RebrandValue(pack, lang, s.Key, s.Value)
	for i, f := range r.forms() {
		if v := *s.forms()[i]; v != nil {
			nv := RebrandValue(pack, lang, s.Key, *v)
			*f = &nv
		}
	}
	return r
}

// RebrandValue returns the text value of key of pack in language lang with
// "Telegram" replaced and its links rewritten (linkRules).
func RebrandValue(pack, lang, key, value string) string {
	if isKept(key) {
		return value
	}
	if rule := linkRules[pack][key]; rule != nil {
		value = rule(value)
	}
	value = sentenceRewrites.Replace(value)
	if r := localNames[primaryLang(lang)]; r != nil {
		value = r.Replace(value)
	}
	return replaceWord(inflect(lang, value))
}

// primaryLang returns the language of a code without its region or variant
// ("pt-br" -> "pt").
func primaryLang(code string) string {
	if i := strings.IndexByte(code, '-'); i >= 0 {
		return code[:i]
	}
	return code
}

// replaceWord applies the word rule, with "Telegram Desktop" as one name.
func replaceWord(value string) string {
	var b strings.Builder
	pos := 0
	for i := 0; i < len(value); {
		j := nextName(value, i)
		if j < 0 {
			break
		}
		word := value[j : j+len("Telegram")]
		end := j + len(word)
		if !wordBoundaryBefore(value, j) || !wordBoundaryAfter(value, end) {
			i = j + 1
			continue
		}
		repl := brandName
		if word == "TELEGRAM" {
			repl = strings.ToUpper(brandName)
		} else if rest := value[end:]; strings.HasPrefix(rest, " Desktop") && wordBoundaryAfter(value, end+len(" Desktop")) {
			end += len(" Desktop")
		}
		b.WriteString(value[pos:j])
		b.WriteString(repl)
		pos, i = end, end
	}
	if pos == 0 {
		return value
	}
	b.WriteString(value[pos:])
	return b.String()
}

// nextName returns the index of the next "Telegram" or "TELEGRAM" at or
// after i, or -1.
func nextName(value string, i int) int {
	a := strings.Index(value[i:], "Telegram")
	c := strings.Index(value[i:], "TELEGRAM")
	switch {
	case a < 0 && c < 0:
		return -1
	case a < 0 || (c >= 0 && c < a):
		return i + c
	default:
		return i + a
	}
}

// wordBoundaryBefore: (?<![A-Za-z0-9@/.]) at i, or a format argument
// right before it ("%3$sTelegram Premium" in the Norwegian pack).
func wordBoundaryBefore(value string, i int) bool {
	if i == 0 || formatArgument.MatchString(value[:i]) {
		return true
	}
	c := value[i-1]
	return !isASCIIAlnum(c) && c != '@' && c != '/' && c != '.'
}

var formatArgument = regexp.MustCompile(`%(?:\d+\$)?[sd@]$`)

// wordBoundaryAfter: (?![A-Za-z0-9]|\.\w) at i, \w being a Unicode word
// character as in Python's re. A Latin or Cyrillic letter continues the
// word too: the Android patch knows English only, the packs have endings
// such as Slovak "Telegramách", which would turn into "Plumaách" (the
// endings of inflect.go are replaced before). Letters of other scripts end
// it (Chinese "Telegram上的").
func wordBoundaryAfter(value string, i int) bool {
	if i >= len(value) {
		return true
	}
	c := value[i]
	if isASCIIAlnum(c) {
		return false
	}
	if r, _ := utf8.DecodeRuneInString(value[i:]); unicode.In(r, unicode.Latin, unicode.Cyrillic) {
		return false
	}
	if c == '.' && i+1 < len(value) {
		r, _ := utf8.DecodeRuneInString(value[i+1:])
		if isWordRune(r) {
			return false
		}
	}
	return true
}

func isASCIIAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.Mn, r)
}

// leftoverName finds the name of Telegram in the scripts of its packs.
var leftoverName = regexp.MustCompile(`(?i)telegram|телеграм|텔레그램|تيليجرام|تليجرام|تلگرام`)

// Leftovers returns the words that still name Telegram in value (such as
// "Telegramu"), outside addresses and handles: what the rules leave and an
// administrator should look at.
func Leftovers(value string) []string {
	var words []string
	for _, m := range leftoverName.FindAllStringIndex(value, -1) {
		if inAddress(value, m[0], m[1]) {
			continue
		}
		start, end := m[0], m[1]
		for start > 0 {
			r, n := utf8.DecodeLastRuneInString(value[:start])
			if !isWordRune(r) {
				break
			}
			start -= n
		}
		for end < len(value) {
			r, n := utf8.DecodeRuneInString(value[end:])
			if !isWordRune(r) {
				break
			}
			end += n
		}
		if w := value[start:end]; !slices.Contains(words, w) {
			words = append(words, w)
		}
	}
	return words
}

// inAddress reports whether value[i:j] is part of an address or a handle:
// its token (between spaces and brackets) has a scheme, a '/', an '@' or a
// '.' right before it, or the name is followed by ".word" (telegram.org).
func inAddress(value string, i, j int) bool {
	start := i
	for start > 0 {
		r, n := utf8.DecodeLastRuneInString(value[:start])
		if unicode.IsSpace(r) || strings.ContainsRune(`"'()[]<>*`, r) {
			break
		}
		start -= n
	}
	end := j
	for end < len(value) {
		r, n := utf8.DecodeRuneInString(value[end:])
		if unicode.IsSpace(r) || strings.ContainsRune(`"'()[]<>*`, r) {
			break
		}
		end += n
	}
	token := value[start:end]
	if strings.Contains(token, "://") || strings.HasPrefix(token, "@") {
		return true
	}
	if i > start && strings.ContainsRune("/@.", rune(value[i-1])) {
		return true
	}
	return !wordBoundaryAfter(value, j) && value[j] == '.'
}
