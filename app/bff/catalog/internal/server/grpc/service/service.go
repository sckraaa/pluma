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

package service

import (
	"github.com/teamgram/teamgram-server/app/bff/catalog/internal/svc"
)

// Service serves the services of the catalog (task 024): mtproto.RPCReactions,
// RPCStickers, RPCCustomEmojis, RPCEmoji, RPCEmojiCategories, RPCEmojiStatus,
// RPCAccentColors, RPCThemes, RPCWallpapers, RPCTimezones, RPCMessageEffects
// and RPCSavedMessageTags. Their methods that are not the catalog's answer
// ErrEnterpriseIsBlocked, as before they were routed here.
type Service struct {
	svcCtx *svc.ServiceContext
}

func (s *Service) GetServiceContext() *svc.ServiceContext {
	return s.svcCtx
}

func New(ctx *svc.ServiceContext) *Service {
	return &Service{
		svcCtx: ctx,
	}
}
