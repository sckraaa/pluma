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
	"context"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/folders/internal/core"
)

// MessagesGetDialogFiltersEFD48C89
// messages.getDialogFilters#efd48c89 = messages.DialogFilters;
func (s *Service) MessagesGetDialogFiltersEFD48C89(ctx context.Context, request *mtproto.TLMessagesGetDialogFiltersEFD48C89) (*mtproto.Messages_DialogFilters, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getDialogFilters - metadata: {%s}", c.MD)
	return c.MessagesGetDialogFiltersEFD48C89(request)
}

// MessagesGetDialogFiltersF19ED96D
// messages.getDialogFilters#f19ed96d = Vector<DialogFilter>;
func (s *Service) MessagesGetDialogFiltersF19ED96D(ctx context.Context, request *mtproto.TLMessagesGetDialogFiltersF19ED96D) (*mtproto.Vector_DialogFilter, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getDialogFilters - metadata: {%s}", c.MD)
	return c.MessagesGetDialogFiltersF19ED96D(request)
}

// MessagesGetSuggestedDialogFilters
// messages.getSuggestedDialogFilters#a29cd42c = Vector<DialogFilterSuggested>;
func (s *Service) MessagesGetSuggestedDialogFilters(ctx context.Context, request *mtproto.TLMessagesGetSuggestedDialogFilters) (*mtproto.Vector_DialogFilterSuggested, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getSuggestedDialogFilters - metadata: {%s}", c.MD)
	return c.MessagesGetSuggestedDialogFilters(request)
}

// MessagesUpdateDialogFilter
// messages.updateDialogFilter#1ad4a04a flags:# id:int filter:flags.0?DialogFilter = Bool;
func (s *Service) MessagesUpdateDialogFilter(ctx context.Context, request *mtproto.TLMessagesUpdateDialogFilter) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.updateDialogFilter - metadata: {%s}", c.MD)
	return c.MessagesUpdateDialogFilter(request)
}

// MessagesUpdateDialogFiltersOrder
// messages.updateDialogFiltersOrder#c563c1e4 order:Vector<int> = Bool;
func (s *Service) MessagesUpdateDialogFiltersOrder(ctx context.Context, request *mtproto.TLMessagesUpdateDialogFiltersOrder) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.updateDialogFiltersOrder - metadata: {%s}", c.MD)
	return c.MessagesUpdateDialogFiltersOrder(request)
}

// MessagesToggleDialogFilterTags
// messages.toggleDialogFilterTags#fd2dda49 enabled:Bool = Bool;
func (s *Service) MessagesToggleDialogFilterTags(ctx context.Context, request *mtproto.TLMessagesToggleDialogFilterTags) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.toggleDialogFilterTags - metadata: {%s}", c.MD)
	return c.MessagesToggleDialogFilterTags(request)
}
