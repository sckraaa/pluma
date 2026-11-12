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
	"fmt"
	"sync"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/stories/internal/svc"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/stories"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	sweepTick = 3 * time.Second
	// sweepBatch bounds one pass: a backlog only shortens the next pass.
	sweepBatch = 64
	// sweepDelay is how long a request has to finish what it stored before
	// the worker takes it over.
	sweepDelay = 5
	// giveUpMentions: after this long the messages of a story's mentions
	// are not tried again.
	giveUpMentions = 3600
)

// Sweeper does what follows from time and what a request left undone:
//
//   - a story leaves the feeds when its period ends (24 hours or the one
//     chosen); the row stays as the archive of its owner, and the service
//     of users is told so that user.stories_max_id of the viewers follows;
//   - a change of a user's stories whose telling failed (stories.Store.Dirty)
//     is told again;
//   - the messages of a story's mentions that were not sent are sent.
//
// It holds no state of its own: everything is in the tables, so a restart
// or a second instance changes nothing.
type Sweeper struct {
	svcCtx *svc.ServiceContext
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// StartSweeper starts the sweep over svcCtx.
func StartSweeper(svcCtx *svc.ServiceContext) *Sweeper {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Sweeper{svcCtx: svcCtx, cancel: cancel}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		t := time.NewTicker(sweepTick)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.Pass(ctx)
			}
		}
	}()
	return s
}

// Stop stops the sweep and waits for the pass in progress.
func (s *Sweeper) Stop() {
	s.cancel()
	s.wg.Wait()
}

// Pass is one sweep.
func (s *Sweeper) Pass(ctx context.Context) {
	c := &StoriesCore{ctx: ctx, svcCtx: s.svcCtx, Logger: logx.WithContext(ctx), MD: &metadata.RpcMetadata{}}
	store := s.svcCtx.Dao.Store
	at := now()

	expired, err := store.Expired(ctx, at, sweepBatch)
	if err != nil {
		c.Logger.Errorf("stories: expired stories: %v", err)
	}
	for _, peer := range expired {
		if err := c.expire(peer, at); err != nil {
			c.Logger.Errorf("stories: expiry of peer %d/%d: %v", peer.Type, peer.Id, err)
		}
	}

	dirty, err := store.Dirty(ctx, at-sweepDelay, sweepBatch)
	if err != nil {
		c.Logger.Errorf("stories: changed peers: %v", err)
	}
	for peer, changedAt := range dirty {
		if err := refreshMaxId(c, peer, changedAt); err != nil {
			c.Logger.Errorf("stories: refresh of peer %d/%d: %v", peer.Type, peer.Id, err)
		}
	}

	pending, err := store.PendingMentions(ctx, at-sweepDelay, sweepBatch)
	if err != nil {
		c.Logger.Errorf("stories: pending mentions: %v", err)
	}
	for _, st := range pending {
		if at-st.Date > giveUpMentions {
			// A message that cannot go out must not be tried forever.
			c.Logger.Errorf("stories: the mentions of story %d of user %d given up", st.Id, st.Peer.Id)
			if err := store.SetMentionsPending(ctx, st.Peer, st.Id, false); err != nil {
				c.Logger.Errorf("stories: mentions of story %d of user %d: %v", st.Id, st.Peer.Id, err)
			}
			continue
		}
		c.tellMentions(st)
	}
}

// expire tells the service of users that stories of the peer left the
// feeds by the time at, then records them as told: a failure between the
// two repeats the telling, which changes nothing the second time.
func (c *StoriesCore) expire(peer stories.Peer, at int32) error {
	if peer.IsUser() {
		id, err := c.svcCtx.Dao.Store.MaxActiveId(c.ctx, peer, at)
		if err != nil {
			return err
		}
		ok, err := c.svcCtx.Dao.UserSetStoriesMaxId(c.ctx, &userpb.TLUserSetStoriesMaxId{UserId: peer.Id, Id: id})
		if err != nil {
			return err
		}
		if !mtproto.FromBool(ok) {
			return fmt.Errorf("user.setStoriesMaxId of user %d: not stored", peer.Id)
		}
	}
	return c.svcCtx.Dao.Store.MarkExpired(c.ctx, peer, at)
}
