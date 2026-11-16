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
	"github.com/teamgram/teamgram-server/pkg/botstore"
	"github.com/teamgram/teamgram-server/pkg/botwidgets"
)

// MessagesGetWidgetPublicData
// messages.getWidgetPublicData#6fc53181 peer:InputPeer msg_id:int hash:long = messages.WidgetPublicData;
//
// A client polls the public state of a shared widget it shows, by one
// message that carries it, with the hash of the state it has (Android
// utils/twidget/WidgetMedia.java poll). The answer
// (jni/twidget/widget-sandbox/docs/tgw-format.md, "A shared widget's shared
// state"): widgetPublicDataNotModified with the period when the message
// has no object ("a static widget always answers so") or the hash
// matches, widgetPublicData with the object's state, hash and period
// otherwise. The period is the server's, 30 s for a static widget.
func (c *BotsCore) MessagesGetWidgetPublicData(in *mtproto.TLMessagesGetWidgetPublicData) (*mtproto.Messages_WidgetPublicData, error) {
	p, err := c.pressedMessage(c.chatPeer(in.Peer), in.MsgId)
	if err != nil {
		c.Logger.Errorf("messages.getWidgetPublicData - error: %v", err)
		return nil, err
	}
	media := p.message.GetMedia()
	if !botwidgets.Is(media) {
		return nil, mtproto.ErrMessageIdInvalid
	}
	notModified := func(period int32) *mtproto.Messages_WidgetPublicData {
		return mtproto.MakeTLMessagesWidgetPublicDataNotModified(&mtproto.Messages_WidgetPublicData{Period: period}).To_Messages_WidgetPublicData()
	}
	if media.GetWidgetId() == nil {
		return notModified(botstore.DefaultWidgetPeriod), nil
	}
	w, err := c.svcCtx.Bots.WidgetOf(c.ctx, media.GetWidgetId().GetValue())
	if err != nil {
		c.Logger.Errorf("messages.getWidgetPublicData - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	switch {
	case w == nil:
		return notModified(botstore.DefaultWidgetPeriod), nil
	case w.PublicHash == in.Hash:
		return notModified(w.Period), nil
	}
	return mtproto.MakeTLMessagesWidgetPublicData(&mtproto.Messages_WidgetPublicData{
		Hash:   w.PublicHash,
		Data:   w.PublicData,
		Period: w.Period,
	}).To_Messages_WidgetPublicData(), nil
}
