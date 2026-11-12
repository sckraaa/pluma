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

// Package reporttree is the tree a report walks: messages.report of the
// reports module and stories.report of the stories module.
package reporttree

import (
	"strings"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/authorization/servicetext"
)

// The tree of messages.report and of stories.report. The clients show what the server sends and
// send back the option of the chosen entry (core.telegram.org/method/
// messages.report; Android ReportBottomSheet.submitOption, tdesktop
// api_report.cpp): an empty option asks for the root.
//
// Telegram's tree is not published and can only be read by filing a report,
// so this one has a single level: the reasons of the ReportReason type of
// the schema and of the older report menu of Android (AlertsCreator:6676,
// the strings ReportChatSpam...ReportChatOther). A reason leads to
// reportResultAddComment under its own option with ":c" appended, so that
// the last call, with or without a comment, ends the report and an empty
// comment is not read as "show the comment page" again.

// A reason's comment is optional except for "other", which says nothing by
// itself (Android's older menu asked for the text of "Other" too,
// AlertsCreator.createReportAlert).
var reasons = []struct {
	key  string
	text map[servicetext.Lang]string
}{
	{"spam", map[servicetext.Lang]string{servicetext.English: "Spam", servicetext.Russian: "Спам"}},
	{"fake", map[servicetext.Lang]string{servicetext.English: "Fake account", servicetext.Russian: "Поддельный аккаунт"}},
	{"violence", map[servicetext.Lang]string{servicetext.English: "Violence", servicetext.Russian: "Насилие"}},
	{"child_abuse", map[servicetext.Lang]string{servicetext.English: "Child abuse", servicetext.Russian: "Детская порнография"}},
	{"illegal_drugs", map[servicetext.Lang]string{servicetext.English: "Illegal drugs", servicetext.Russian: "Запрещённые вещества"}},
	{"personal_details", map[servicetext.Lang]string{servicetext.English: "Personal details", servicetext.Russian: "Личные данные"}},
	{"pornography", map[servicetext.Lang]string{servicetext.English: "Pornography", servicetext.Russian: "Порнография"}},
	{"copyright", map[servicetext.Lang]string{servicetext.English: "Copyright", servicetext.Russian: "Авторские права"}},
	{"other", map[servicetext.Lang]string{servicetext.English: "Other", servicetext.Russian: "Другое"}},
}

var chooseTitle = map[servicetext.Lang]string{servicetext.English: "Report", servicetext.Russian: "Пожаловаться"}

const commentSuffix = ":c"

func isReason(key string) bool {
	for _, r := range reasons {
		if r.key == key {
			return true
		}
	}
	return false
}

// CommentOptional tells whether a report of the reason may go without a
// comment.
func CommentOptional(key string) bool {
	return key != "other"
}

// ChooseOption is the root of the tree in the language of the session.
func ChooseOption(langCode string) *mtproto.ReportResult {
	lang := servicetext.LangOf(langCode)
	options := make([]*mtproto.MessageReportOption, len(reasons))
	for i, r := range reasons {
		options[i] = mtproto.MakeTLMessageReportOption(&mtproto.MessageReportOption{
			Text:   r.text[lang],
			Option: []byte(r.key),
		}).To_MessageReportOption()
	}
	return mtproto.MakeTLReportResultChooseOption(&mtproto.ReportResult{
		Title:   chooseTitle[lang],
		Options: options,
	}).To_ReportResult()
}

// StepOf reads the option of a messages.report call. reason is empty for
// the root; final tells the report is complete.
func StepOf(option []byte) (reason string, final bool, err error) {
	if len(option) == 0 {
		return "", false, nil
	}
	key := string(option)
	if rest, ok := strings.CutSuffix(key, commentSuffix); ok {
		if !isReason(rest) {
			return "", false, mtproto.ErrOptionInvalid
		}
		return rest, true, nil
	}
	if !isReason(key) {
		return "", false, mtproto.ErrOptionInvalid
	}
	return key, false, nil
}

// AddComment is the answer to a chosen reason.
func AddComment(reason string) *mtproto.ReportResult {
	return mtproto.MakeTLReportResultAddComment(&mtproto.ReportResult{
		Optional: CommentOptional(reason),
		Option:   []byte(reason + commentSuffix),
	}).To_ReportResult()
}

// ReasonOf is the key of a ReportReason of the older methods.
func ReasonOf(r *mtproto.ReportReason) string {
	switch r.GetPredicateName() {
	case mtproto.Predicate_inputReportReasonSpam:
		return "spam"
	case mtproto.Predicate_inputReportReasonViolence:
		return "violence"
	case mtproto.Predicate_inputReportReasonPornography:
		return "pornography"
	case mtproto.Predicate_inputReportReasonChildAbuse:
		return "child_abuse"
	case mtproto.Predicate_inputReportReasonCopyright:
		return "copyright"
	case mtproto.Predicate_inputReportReasonGeoIrrelevant:
		return "geo_irrelevant"
	case mtproto.Predicate_inputReportReasonFake:
		return "fake"
	case mtproto.Predicate_inputReportReasonIllegalDrugs:
		return "illegal_drugs"
	case mtproto.Predicate_inputReportReasonPersonalDetails:
		return "personal_details"
	}
	return "other"
}
