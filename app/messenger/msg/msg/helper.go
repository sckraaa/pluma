/*
 * Created from 'scheme.tl' by 'mtprotoc'
 *
 * Copyright (c) 2021-present,  Teamgram Studio (https://teamgram.io).
 *  All rights reserved.
 *
 * Author: teamgramio (teamgram.io@gmail.com)
 */

package msg_helper

import (
	"context"
	"sync"
	"time"

	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/internal/core"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/internal/server/grpc/service"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/internal/svc"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/plugin"
)

// New returns the msg RPC service over the storage d, which it shares with
// the inbox consumer.
func New(d *dao.Dao, plugin plugin.MsgPlugin) *service.Service {
	return service.New(svc.NewServiceContext(d, plugin))
}

// pollSweep is how often the polls past their closing date are closed; the
// votes are refused from that date on whether or not the sweep got there
// (polls.Record.IsClosed).
const pollSweep = 5 * time.Second

// PollSweeper closes the polls past their closing date on a timer.
type PollSweeper struct {
	stop chan struct{}
	done sync.WaitGroup
}

// StartPollSweeper starts the sweep over d; Stop waits for it.
func StartPollSweeper(d *dao.Dao) *PollSweeper {
	s := &PollSweeper{stop: make(chan struct{})}
	svcCtx := svc.NewServiceContext(d, nil)
	s.done.Add(1)
	go func() {
		defer s.done.Done()
		t := time.NewTicker(pollSweep)
		defer t.Stop()
		for {
			select {
			case <-s.stop:
				return
			case <-t.C:
				ctx, cancel := context.WithTimeout(context.Background(), pollSweep*2)
				c := core.New(ctx, svcCtx)
				if _, err := c.SweepDuePolls(); err != nil {
					c.Logger.Errorf("msg: sweep polls: %v", err)
				}
				cancel()
			}
		}
	}()
	return s
}

// Stop ends the sweep.
func (s *PollSweeper) Stop() {
	close(s.stop)
	s.done.Wait()
}
