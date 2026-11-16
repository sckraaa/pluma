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

package svc

import (
	"context"
	"errors"
	"fmt"

	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/translation/internal/config"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	channel_client "github.com/teamgram/teamgram-server/app/service/biz/channel/client"
	dialog_client "github.com/teamgram/teamgram-server/app/service/biz/dialog/client"
	dialogpb "github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	message_client "github.com/teamgram/teamgram-server/app/service/biz/message/client"
	messagepb "github.com/teamgram/teamgram-server/app/service/biz/message/message"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/translate"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/stores/kv"
)

// Messages reads the copies of messages in a user's box.
type Messages interface {
	MessageGetUserMessageList(ctx context.Context, in *messagepb.TLMessageGetUserMessageList) (*messagepb.Vector_MessageBox, error)
}

// Channels reads a channel and its messages as a user sees them.
type Channels interface {
	ChannelGetMutableChannel(ctx context.Context, in *channelpb.TLChannelGetMutableChannel) (*channelpb.MutableChannel, error)
	ChannelGetMessages(ctx context.Context, in *channelpb.TLChannelGetMessages) (*channelpb.Vector_Message, error)
}

// Dialogs tells whether a user has a dialog with a user or a chat.
type Dialogs interface {
	DialogGetDialogById(ctx context.Context, in *dialogpb.TLDialogGetDialogById) (*dialogpb.DialogExt, error)
}

// Users reads the users a mention of a client's text names.
type Users interface {
	UserGetUserDataListByIdList(ctx context.Context, in *userpb.TLUserGetUserDataListByIdList) (*userpb.Vector_UserData, error)
}

// Translator translates the items of a request: *translate.Scheduler.
type Translator interface {
	Translate(ctx context.Context, a translate.Ask) ([]*mtproto.TextWithEntities, error)
}

// PeerSettings keeps the chats whose translation bar their reader hid:
// *translate.Store.
type PeerSettings interface {
	SetTranslationsDisabled(ctx context.Context, userId int64, peer translate.TranslationPeer, disabled bool) error
}

// AutotranslationToggler is channels.toggleAutotranslation of the channels
// area (task 082, ADR 0050). The gRPC service RPCTranslation is registered
// once, here, so the setting of a channel stays where the other settings
// of channels are and is reached through this seam.
type AutotranslationToggler interface {
	ChannelsToggleAutotranslation(ctx context.Context, in *mtproto.TLChannelsToggleAutotranslation) (*mtproto.Updates, error)
}

type ServiceContext struct {
	// Config has the defaults of Translate in.
	Config config.Config
	// Profile and Translator are nil without a configured engine: the
	// translation is off.
	Profile    translate.Profile
	Translator Translator
	Settings   PeerSettings
	Messages   Messages
	Channels   Channels
	Dialogs    Dialogs
	Users      Users

	Autotranslation AutotranslationToggler
}

// Background is the work of the translation that outlives a request: the
// queue of the engine and the sweep of the cache.
type Background struct {
	scheduler *translate.Scheduler
	sweeper   *translate.CacheSweeper
}

// Stop answers the queued items, waits for those in the engine (ADR 0064,
// section 6) and stops the sweep.
func (b *Background) Stop() {
	if b.scheduler != nil {
		b.scheduler.Stop()
	}
	if b.sweeper != nil {
		b.sweeper.Stop()
	}
}

// NewServiceContext connects the translation and starts its background
// work. A configuration that cannot work stops bff at its start, as the
// other areas do.
func NewServiceContext(c config.Config, autotranslation AutotranslationToggler) (*ServiceContext, *Background) {
	if autotranslation == nil {
		panic(errors.New("translation: no channels.toggleAutotranslation"))
	}
	c.Translate = c.Translate.WithDefaults()
	if err := c.Translate.Validate(); err != nil {
		panic(err)
	}
	db, err := userbox.Open(c.Mysql.DSN)
	if err != nil {
		panic(fmt.Errorf("translation: %w", err))
	}
	store := translate.NewStore(db)
	s := &ServiceContext{
		Config:          c,
		Settings:        store,
		Messages:        message_client.NewMessageClient(rpcx.GetCachedRpcClient(c.MessageClient)),
		Channels:        channel_client.NewChannelClient(rpcx.GetCachedRpcClient(c.ChannelClient)),
		Dialogs:         dialog_client.NewDialogClient(rpcx.GetCachedRpcClient(c.DialogClient)),
		Users:           user_client.NewUserClient(rpcx.GetCachedRpcClient(c.UserClient)),
		Autotranslation: autotranslation,
	}
	bg := &Background{}
	// The cache is swept with the translation off as well: what an earlier
	// engine translated expires on its own.
	if bg.sweeper, err = translate.StartCacheSweeper(store, c.Translate.CacheTTL); err != nil {
		panic(err)
	}
	if !c.Translate.Enabled() {
		return s, bg
	}
	if s.Profile, err = translate.NewProfile(c.Translate.Profile, c.Translate.SlotContext()); err != nil {
		bg.Stop()
		panic(err)
	}
	limits := translate.NewRedisLimits(kv.NewStore(c.KV), c.Translate)
	scheduler, err := translate.StartScheduler(c.Translate, s.Profile, translate.NewHTTPEngine(c.Translate.HTTPConfig()), store, limits)
	if err != nil {
		bg.Stop()
		panic(err)
	}
	s.Translator, bg.scheduler = scheduler, scheduler
	return s, bg
}
