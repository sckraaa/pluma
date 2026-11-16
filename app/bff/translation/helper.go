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

package translation_helper

import (
	"github.com/teamgram/teamgram-server/app/bff/translation/internal/config"
	"github.com/teamgram/teamgram-server/app/bff/translation/internal/server/grpc/service"
	"github.com/teamgram/teamgram-server/app/bff/translation/internal/svc"
)

type (
	Config = config.Config
	// AutotranslationToggler is channels.toggleAutotranslation of the
	// channels area, which RPCTranslation carries along.
	AutotranslationToggler = svc.AutotranslationToggler
	// Background is the queue of the engine and the sweep of the cache.
	Background = svc.Background
)

// New returns RPCTranslation and RPCAiComposeTone (task 101, ADR 0064) and
// starts their background work; Stop of the background waits for it.
func New(c Config, autotranslation AutotranslationToggler) (*service.Service, *Background) {
	ctx, bg := svc.NewServiceContext(c, autotranslation)
	return service.New(ctx), bg
}
