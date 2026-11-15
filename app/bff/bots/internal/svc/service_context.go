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

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/config"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/dao"
	"github.com/teamgram/teamgram-server/pkg/inlinebots"
)

// ServiceContext is the state of the bots methods.
type ServiceContext struct {
	Config config.Config
	*dao.Dao

	// InlineBots tells which built-in inline bots (pkg/inlinebots) have
	// their accounts in order and may be served (ADR 0062, p. 1).
	InlineBots *inlinebots.Health
}

// NewServiceContext connects the service. It checks the accounts of the
// built-in inline bots before it returns and keeps checking them in the
// background: a bot whose account does not match the registry is not served
// until it does, the rest work.
func NewServiceContext(c config.Config) *ServiceContext {
	health := inlinebots.NewHealth()
	health.Watch(context.Background(), sqlx.NewMySQL(&c.Mysql))
	return &ServiceContext{
		Config:     c,
		Dao:        dao.New(c),
		InlineBots: health,
	}
}
