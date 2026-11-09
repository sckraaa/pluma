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
	"context"
	"time"

	"github.com/teamgram/teamgram-server/app/bff/secretchats/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/threading"
)

const (
	sweepInterval = time.Minute
	sweepTimeout  = 30 * time.Second
)

// StartSweeper runs the janitor of the secret chats (Store.Sweep) for the
// life of the process.
func StartSweeper(svcCtx *svc.ServiceContext) {
	threading.GoSafe(func() {
		ticker := time.NewTicker(sweepInterval)
		defer ticker.Stop()
		for range ticker.C {
			sweep(svcCtx)
		}
	})
}

func sweep(svcCtx *svc.ServiceContext) {
	ctx, cancel := context.WithTimeout(context.Background(), sweepTimeout)
	defer cancel()
	discarded, err := svcCtx.Dao.Store.Sweep(ctx)
	if err != nil {
		logx.WithContext(ctx).Errorf("secretchats: sweep: %v", err)
	}
	for _, d := range discarded {
		for _, t := range d.Targets {
			push(ctx, svcCtx, t, nil, d.Chat.UpdateEncryption(t, now()))
		}
	}
}
