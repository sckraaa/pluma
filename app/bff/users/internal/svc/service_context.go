// Copyright 2022 Teamgram Authors
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
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package svc

import (
	"context"
	"fmt"

	"github.com/teamgram/teamgram-server/app/bff/users/internal/config"
	"github.com/teamgram/teamgram-server/app/bff/users/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/users/internal/plugin"
	"github.com/teamgram/teamgram-server/pkg/translate"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// PeerTranslations reads the chats whose translation bar their reader hid
// with messages.togglePeerTranslations (task 101): *translate.Store.
type PeerTranslations interface {
	TranslationsDisabled(ctx context.Context, userId int64, peers ...translate.TranslationPeer) (map[translate.TranslationPeer]bool, error)
}

type ServiceContext struct {
	Config config.Config
	*dao.Dao
	// Translations gives userFull.translations_disabled (task 101).
	Translations PeerTranslations
}

func NewServiceContext(c config.Config, plugin1 plugin.StoryPlugin, plugin2 plugin.PersonalChannelPlugin, plugin3 plugin.WallpaperPlugin) *ServiceContext {
	db, err := userbox.Open(c.Mysql.DSN)
	if err != nil {
		panic(fmt.Errorf("users: %w", err))
	}
	return &ServiceContext{
		Config:       c,
		Dao:          dao.New(c, plugin1, plugin2, plugin3),
		Translations: translate.NewStore(db),
	}
}
