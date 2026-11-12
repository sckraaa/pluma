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

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/app/bff/autodownload/internal/config"
	"github.com/teamgram/teamgram-server/pkg/autodownload"
)

// Saved keeps what the accounts saved as their auto-download settings
// (pkg/autodownload).
type Saved interface {
	Save(ctx context.Context, userID int64, preset autodownload.Preset, settings autodownload.Settings, now int64) error
}

type ServiceContext struct {
	Config config.Config
	Saved  Saved
}

func NewServiceContext(c config.Config) *ServiceContext {
	return &ServiceContext{
		Config: c,
		Saved:  autodownload.NewStore(sqlx.NewMySQL(&c.Mysql)),
	}
}
