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
	_ "embed"
	"fmt"
	"strings"
)

// The texts of the statistics of channels, groups, posts, stories and polls
// (task 103, ADR 0066): the names of the series of the graphs (the "names"
// of statsGraph's JSON), the names of the languages of the graph of
// languages and the texts of statsGraphError, in the language of the
// viewer's session. They are in every language of own.json (the test holds
// them to it). None has a "-": tdesktop shows a series name with it
// replaced by a dash.
//
// stats.json has mail.json's shape: {key: {lang_code: value}}.
//
//go:embed stats.json
var statsJSON []byte

var statsTexts = mustLoadTexts("stats.json", statsJSON)

// The keys of stats.json the code names; the names of languages are
// StatsLanguageKey's.
const (
	// The followers of a channel (growth, views by source) and the members
	// of a supergroup (growth).
	StatsSubscribers       = "PlumaStatsSubscribers"
	StatsSourceSubscribers = "PlumaStatsSourceSubscribers"
	StatsMembers           = "PlumaStatsMembers"

	// Joins and leaves; mutes and unmutes.
	StatsJoined  = "PlumaStatsJoined"
	StatsLeft    = "PlumaStatsLeft"
	StatsMuted   = "PlumaStatsMuted"
	StatsUnmuted = "PlumaStatsUnmuted"

	// The two series of a channel's top hours.
	StatsThisPeriod     = "PlumaStatsThisPeriod"
	StatsPreviousPeriod = "PlumaStatsPreviousPeriod"

	// Interactions of posts and stories.
	StatsViews  = "PlumaStatsViews"
	StatsShares = "PlumaStatsShares"

	// Where the views of a channel's posts come from.
	StatsSourceChannels = "PlumaStatsSourceChannels"
	StatsSourceGroups   = "PlumaStatsSourceGroups"
	StatsSourcePrivate  = "PlumaStatsSourcePrivate"
	// StatsSourceOther is the other source of views and of joins.
	StatsSourceOther = "PlumaStatsSourceOther"

	// How the new followers or members joined.
	StatsJoinSourceLink    = "PlumaStatsJoinSourceLink"
	StatsJoinSourceFolder  = "PlumaStatsJoinSourceFolder"
	StatsJoinSourceRequest = "PlumaStatsJoinSourceRequest"
	StatsJoinSourcePublic  = "PlumaStatsJoinSourcePublic"
	StatsJoinSourceAdded   = "PlumaStatsJoinSourceAdded"

	// StatsOtherLanguages is the series of the languages past the top ones.
	StatsOtherLanguages = "PlumaStatsOther"

	// Reactions by emotion.
	StatsReactionsPositive = "PlumaStatsReactionsPositive"
	StatsReactionsNegative = "PlumaStatsReactionsNegative"
	StatsReactionsOther    = "PlumaStatsReactionsOther"

	// The messages of a supergroup by type.
	StatsMessagesText     = "PlumaStatsMessagesText"
	StatsMessagesPhotos   = "PlumaStatsMessagesPhotos"
	StatsMessagesVideos   = "PlumaStatsMessagesVideos"
	StatsMessagesVoice    = "PlumaStatsMessagesVoice"
	StatsMessagesRound    = "PlumaStatsMessagesRound"
	StatsMessagesStickers = "PlumaStatsMessagesStickers"
	StatsMessagesGifs     = "PlumaStatsMessagesGifs"
	StatsMessagesFiles    = "PlumaStatsMessagesFiles"
	StatsMessagesMusic    = "PlumaStatsMessagesMusic"
	StatsMessagesOther    = "PlumaStatsMessagesOther"

	// A supergroup's actions and top hours.
	StatsMessages = "PlumaStatsMessages"
	StatsViewers  = "PlumaStatsViewers"

	// The texts of statsGraphError: too little data to draw, a poll with no
	// votes, the graph of Instant View, which Pluma does not have.
	StatsNotEnoughData = "PlumaStatsNotEnoughData"
	StatsNoVotes       = "PlumaStatsNoVotes"
	StatsNoInstantView = "PlumaStatsNoInstantView"
)

// StatsWeekdays are the keys of the days of the week, Monday first, the
// order of a supergroup's graph of weekdays.
var StatsWeekdays = [7]string{
	"PlumaStatsWeekday1", "PlumaStatsWeekday2", "PlumaStatsWeekday3", "PlumaStatsWeekday4",
	"PlumaStatsWeekday5", "PlumaStatsWeekday6", "PlumaStatsWeekday7",
}

// statsLanguagePrefix starts the key of the name of a language: the prefix
// and the base language code ("PlumaStatsLang_pt" for "pt-br").
const statsLanguagePrefix = "PlumaStatsLang_"

// StatsText returns the text of key in lang, with MailText's fallbacks. It
// panics on a key stats.json does not have: the keys are constants of the
// code (the constants above, StatsWeekdays and the keys StatsLanguageKey
// reports as known).
func StatsText(key, lang string) string {
	texts, ok := statsTexts[key]
	if !ok {
		panic(fmt.Sprintf("langpack: no stats text %q", key))
	}
	return pick(texts, lang)
}

// StatsLanguageKey returns the key of the name of the language of a
// lang_code of initConnection (any case, "_" or "-"): that of its base
// language, so "pt-br" counts as "pt" and "zh-hans" and "zh-hant" as "zh".
// ok is false for a language stats.json does not name (and for an empty
// code); the graph of languages counts it among the others.
func StatsLanguageKey(langCode string) (key string, ok bool) {
	base, _, _ := strings.Cut(CanonicalLangCode(langCode), "-")
	if base == "" {
		return "", false
	}
	key = statsLanguagePrefix + base
	if _, ok := statsTexts[key]; !ok {
		return "", false
	}
	return key, true
}
