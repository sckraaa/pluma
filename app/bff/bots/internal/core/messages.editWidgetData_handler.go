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
	"github.com/teamgram/teamgram-server/pkg/botwidgets"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// MessagesEditWidgetData
// messages.editWidgetData#eac343c0 flags:# widget_id:long public_data:string cache_time:flags.0?int = Bool;
//
// The bot writes the public state of its shared widget by its id, which
// "works always, even when every carrier is deleted or unknown to the
// bot"; cache_time is "the only way to set the object's period", within 5
// to 3600 s, 0 setting it back to 30 s; "at most one edit per second per
// object" (FLOOD_WAIT); an id of another bot or none is WIDGET_ID_INVALID
// (Android jni/twidget/widget-sandbox/docs/tgw-format.md, "The bot's edits
// of the state"). The carriers get the state by polling
// (messages.getWidgetPublicData): the clients follow a shared state only so.
func (c *BotsCore) MessagesEditWidgetData(in *mtproto.TLMessagesEditWidgetData) (*mtproto.Bool, error) {
	if _, err := c.meBot(); err != nil {
		return nil, err
	}
	data := wrapperspb.String(in.GetPublicData())
	if err := botwidgets.CheckData(data, mtproto.ErrWidgetDataTooLarge, mtproto.ErrWidgetDataInvalid); err != nil {
		return nil, err
	}
	if ok, err := c.svcCtx.AllowWidgetEdit(c.ctx, in.GetWidgetId()); err != nil {
		c.Logger.Errorf("messages.editWidgetData - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	} else if !ok {
		return nil, mtproto.NewErrFloodWaitX(1)
	}
	if _, err := c.setWidgetPublicData(in.GetWidgetId(), in.GetPublicData(), in.GetCacheTime()); err != nil {
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
