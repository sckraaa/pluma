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

package core

import (
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/media/media"
)

// MessagesReportMusicListen
// messages.reportMusicListen#ddbcd819 id:InputDocument listened_duration:int = Bool;
//
// Keeps that the user listened to a music file for listened_duration
// seconds (task 098, part D), for the statistics (task 103). All four
// clients send the file played with the seconds listened and ignore the
// answer: Android after 3 s (messenger/MediaController.java:7608-7618),
// Desktop (media/player/media_player_listen_tracker.cpp:70-82; it only
// retries on FILE_REFERENCE_*, which is not checked here), iOS
// (TelegramEngine/Messages/TelegramEngineMessages.swift:2023-2036), Web K
// (lib/appManagers/appMessagesManager.ts:15748-15753). The
// document must be one the server has, with its access_hash; the listens
// of a user and a document in one minute make one row
// (readmetrics.Store.AddListen).
func (c *ReportsCore) MessagesReportMusicListen(in *mtproto.TLMessagesReportMusicListen) (*mtproto.Bool, error) {
	if c.MD.GetIsBot() {
		return nil, mtproto.ErrBotMethodInvalid
	}
	id := in.GetId()
	if id.GetPredicateName() != mtproto.Predicate_inputDocument || id.GetId() == 0 {
		return nil, mtproto.ErrDocumentInvalid
	}
	if in.GetListenedDuration() < 0 {
		return nil, mtproto.ErrInputRequestInvalid
	}
	doc, err := c.svcCtx.Dao.Media.MediaGetDocument(c.ctx, &media.TLMediaGetDocument{Id: id.GetId()})
	if err != nil {
		c.Logger.Errorf("messages.reportMusicListen - document %d: %v", id.GetId(), err)
		return nil, err
	}
	if doc.GetPredicateName() != mtproto.Predicate_document || doc.GetAccessHash() != id.GetAccessHash() {
		return nil, mtproto.ErrDocumentInvalid
	}
	if err = c.svcCtx.Dao.ReadMetrics.AddListen(c.ctx, c.MD.UserId, doc.GetId(), in.GetListenedDuration()); err != nil {
		c.Logger.Errorf("messages.reportMusicListen - user %d: %v", c.MD.UserId, err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
