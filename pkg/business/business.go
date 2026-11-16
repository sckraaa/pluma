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

// Package business keeps what an account sets up in Telegram Business
// (task 105, ADR 0067), open to every account (ADR 0041): the work hours,
// the location and the intro of its profile, the automatic replies, the
// chat links and the connected bot (migrate-20261119-business.sql). The
// values are kept normalized, as the methods answer them: sorted and merged
// intervals of the work hours, ids of users of the recipients.
//
// The bff modules business, users and dialogs, the service msg and the
// service links read it; there is no cache in Redis, every read is by
// primary key.
package business

// The limits the clients take from help.getAppConfig
// (teamgramd/bin/app_config.json), and the server applies the same numbers:
// a test keeps them equal. Telegram publishes none of the values; these are
// the clients' defaults.
const (
	// QuickRepliesLimit is quick_replies_limit, the most shortcuts of an
	// account, the automatic replies "hello" and "away" among them
	// (tdesktop settings/business/settings_recipients_helper.cpp:397;
	// Android MessagesController.java:4251 reads the key, default 10).
	QuickRepliesLimit = 100
	// QuickReplyMessagesLimit is quick_reply_messages_limit, the most
	// messages of a shortcut (tdesktop settings_recipients_helper.cpp:409,
	// Android AppGlobalConfig.java:105).
	QuickReplyMessagesLimit = 20
	// ChatLinksLimit is business_chat_links_limit (Android
	// MessagesController.java:4405, tdesktop
	// settings/business/settings_chat_links.cpp:734).
	ChatLinksLimit = 100
	// IntroTitleLengthLimit is intro_title_length_limit, in UTF-16 code
	// units as the clients count it (tdesktop
	// settings/business/settings_chat_intro.cpp:569; Android
	// MessagesController.java:4383).
	IntroTitleLengthLimit = 32
	// IntroDescriptionLengthLimit is intro_description_length_limit, the
	// smaller of the defaults of tdesktop (70,
	// settings_chat_intro.cpp:574) and Android (72,
	// MessagesController.java:4394).
	IntroDescriptionLengthLimit = 70
)

// The limits of the server alone: the clients do not take them from the
// app config.
const (
	// AddressLengthMax is the longest address of a business location, in
	// characters: "max 96 UTF-8 characters" (core.telegram.org/api/business).
	AddressLengthMax = 96
	// ChatLinkMessageLengthMax is the longest text of a chat link: it
	// becomes the text of a message (message_length_max).
	ChatLinkMessageLengthMax = 4096
	// ChatLinkTitleLengthMax is the longest title of a chat link. Telegram
	// publishes no value (not verified); a title is a short name in the
	// list of links, as long as the title of an intro.
	ChatLinkTitleLengthMax = 32
	// RecipientsUsersMax is the most users a list of recipients names.
	// Telegram publishes no value (not verified); the clients pick users
	// from the chats one by one.
	RecipientsUsersMax = 100
)
