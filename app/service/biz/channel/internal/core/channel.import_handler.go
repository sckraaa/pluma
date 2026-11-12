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
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// The import of a group from an export (task 083, ADR 0051): the RPCs of
// pluma-admin import-chat. Each runs in the box of the import's creator.

// ChannelImportCreate
// channel.importCreate creator_id:long source_type:string source_chat_id:long title:string about:string mapping:string first:ImportedMessage = ChannelImport;
//
// Starts the import of a chat (dao.ImportCreate) or, for a chat whose import
// runs already, adds the authors of mapping to it. The answer is the import
// as it is now.
func (c *ChannelCore) ChannelImportCreate(in *channel.TLChannelImportCreate) (*channel.ChannelImport, error) {
	var importId int64
	if _, err := c.svcCtx.Dao.RunBox(c.ctx, in.CreatorId, func(b *userbox.Box) error {
		var err error
		importId, err = c.svcCtx.Dao.ImportCreate(b, in)
		return err
	}); err != nil {
		c.Logger.Errorf("channel.importCreate - chat %d: %v", in.SourceChatId, err)
		return nil, err
	}
	return c.svcCtx.Dao.ImportState(c.ctx, importId, "", 0)
}

// ChannelImportMessages
// channel.importMessages import_id:long first_seq:long messages:Vector<ImportedMessage> = ChannelImport;
//
// Writes one batch of an import in one transaction (dao.ImportMessages).
// Nobody is in the channel yet, so nothing is pushed.
func (c *ChannelCore) ChannelImportMessages(in *channel.TLChannelImportMessages) (*channel.ChannelImport, error) {
	state, err := c.svcCtx.Dao.ImportState(c.ctx, in.ImportId, "", 0)
	if err != nil {
		return nil, err
	}
	if _, err = c.svcCtx.Dao.RunBox(c.ctx, state.CreatorId, func(b *userbox.Box) error {
		return c.svcCtx.Dao.ImportMessages(b, in.ImportId, in.FirstSeq, in.Messages)
	}); err != nil {
		c.Logger.Errorf("channel.importMessages - import %d, %d records from %d: %v", in.ImportId, len(in.Messages), in.FirstSeq, err)
		return nil, err
	}
	return c.svcCtx.Dao.ImportState(c.ctx, in.ImportId, "", 0)
}

// ChannelImportFinish
// channel.importFinish import_id:long = ChannelImport;
//
// Ends an import (dao.ImportFinish): the creator is in the channel, with
// everything read, and its devices get updateChannel with seq, as after
// channels.createChannel. The other members are added by
// channel.inviteToChannel afterwards.
func (c *ChannelCore) ChannelImportFinish(in *channel.TLChannelImportFinish) (*channel.ChannelImport, error) {
	state, err := c.svcCtx.Dao.ImportState(c.ctx, in.ImportId, "", 0)
	if err != nil {
		return nil, err
	}
	finished := false
	if _, err = c.svcCtx.Dao.RunBox(c.ctx, state.CreatorId, func(b *userbox.Box) error {
		var err error
		finished, err = c.svcCtx.Dao.ImportFinish(b, in.ImportId, megagroupCreatorRights)
		return err
	}); err != nil {
		c.Logger.Errorf("channel.importFinish - import %d: %v", in.ImportId, err)
		return nil, err
	}
	if finished {
		c.Logger.Infof("channel.importFinish - import %d: channel %d of %d", in.ImportId, state.ChannelId, state.CreatorId)
		// The channel as the creator sees it, read once committed.
		mc, err := c.svcCtx.Dao.GetMutableChannel(c.ctx, state.ChannelId, []int64{state.CreatorId})
		if err == nil {
			view := mc.ToChat(state.CreatorId, int32(time.Now().Unix()))
			_, err = c.svcCtx.Dao.RunBox(c.ctx, state.CreatorId, func(b *userbox.Box) error {
				return c.svcCtx.Dao.EmitSeqUpdate(b, mtproto.MakeTLUpdateChannel(&mtproto.Update{ChannelId: state.ChannelId}).To_Update(), nil, []*mtproto.Chat{view})
			})
		}
		if err != nil {
			// The import is finished; the creator's devices learn the
			// channel from the dialogs.
			c.Logger.Errorf("channel.importFinish - updateChannel of %d: %v", state.ChannelId, err)
		}
	}
	return c.svcCtx.Dao.ImportState(c.ctx, in.ImportId, "", 0)
}

// ChannelImportGetState
// channel.importGetState import_id:long source_type:string source_chat_id:long = ChannelImport;
func (c *ChannelCore) ChannelImportGetState(in *channel.TLChannelImportGetState) (*channel.ChannelImport, error) {
	return c.svcCtx.Dao.ImportState(c.ctx, in.ImportId, in.SourceType, in.SourceChatId)
}

// ChannelImportAbort
// channel.importAbort import_id:long = ChannelImport;
//
// Drops an import that is not finished (dao.ImportAbort).
func (c *ChannelCore) ChannelImportAbort(in *channel.TLChannelImportAbort) (*channel.ChannelImport, error) {
	state, err := c.svcCtx.Dao.ImportState(c.ctx, in.ImportId, "", 0)
	if err != nil {
		return nil, err
	}
	if _, err = c.svcCtx.Dao.RunBox(c.ctx, state.CreatorId, func(b *userbox.Box) error {
		return c.svcCtx.Dao.ImportAbort(b, in.ImportId)
	}); err != nil {
		c.Logger.Errorf("channel.importAbort - import %d: %v", in.ImportId, err)
		return nil, err
	}
	c.Logger.Infof("channel.importAbort - import %d: channel %d", in.ImportId, state.ChannelId)
	return c.svcCtx.Dao.ImportState(c.ctx, in.ImportId, "", 0)
}
