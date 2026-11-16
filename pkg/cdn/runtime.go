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

package cdn

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

// Rhythm of the runtime.
const (
	// queueGaugeEvery is how often metricQueue is read from the table.
	queueGaugeEvery = time.Minute
	// originShutdown is how long the stop waits for the requests the
	// origin is serving; then their connections are closed.
	originShutdown = 10 * time.Second
	// originIdleTimeout closes a connection of a CDN idle for so long.
	originIdleTimeout = 2 * time.Minute
	// originMaxHeaderBytes bounds the headers of a request to the origin.
	originMaxHeaderBytes = 32 << 10
)

// Runtime is the HTTPS CDN of a bff process (ADR 0071): the Home the
// methods use, and beside them the origin the CDNs pull from, a preparer
// for each target of the deliveries, the checks of the deliveries and
// their budget.
type Runtime struct {
	// Home decides the redirects of upload.getFile and serves the CDN
	// methods.
	Home *Home
	// Usage is the traffic and budget of the deliveries: the redirects
	// note theirs (NoteRedirect) and leave a delivery out of the routes
	// once it is OverBudget.
	Usage *Usage
	// Health checks the deliveries; the redirects read its outcome from
	// cdn_deliveries.
	Health *Health

	preparers []*Preparer
	origin    *http.Server // nil when no delivery pulls from the origin
	// originAddr is the address the origin listens at.
	originAddr net.Addr

	cancel context.CancelFunc
	done   chan struct{}
}

// Start starts the CDN of set over store: it listens at the origin (an
// address in use stops the start) and starts the preparers, the checks
// and the budget. Several processes may run them at once: preparations
// are claimed by lease, the checks write the same rows and the traffic is
// added up.
func Start(store Store, set *Settings, sizer Sizer, reader FileReader) (*Runtime, error) {
	if len(set.Deliveries) == 0 {
		return nil, errors.New("cdn: no delivery")
	}
	home, err := NewHome(store, set, sizer)
	if err != nil {
		return nil, err
	}
	r := &Runtime{
		Home:   home,
		Usage:  NewUsage(store, set.Deliveries),
		Health: NewHealth(store, set),
		done:   make(chan struct{}),
	}

	byID := map[int32]Target{}
	for i := range set.Deliveries {
		d := &set.Deliveries[i]
		t, err := targetOf(d)
		if err != nil {
			return nil, err
		}
		byID[t.ID()] = t
		dev := 0.0
		if d.InsecureHTTP {
			dev = 1
			logx.Errorf("cdn: delivery %d has an http:// BaseURL (CDN_DEV_ALLOW_HTTP=1): only the test stand may run so", d.ID)
		}
		metricDevHTTP.Set(dev, dcLabel(d.ID))
	}
	for _, t := range byID {
		r.preparers = append(r.preparers, NewPreparer(store, t, reader, set, home.Forget))
	}
	home.wake = func() {
		for _, p := range r.preparers {
			p.Wake()
		}
	}

	var ln net.Listener
	if set.Origin.Listen != "" {
		origin, err := NewOrigin(store, set, reader, r.Usage)
		if err != nil {
			return nil, err
		}
		if ln, err = net.Listen("tcp", set.Origin.Listen); err != nil {
			return nil, fmt.Errorf("cdn: origin: %w", err)
		}
		r.originAddr = ln.Addr()
		r.origin = &http.Server{
			Handler:           origin,
			ReadHeaderTimeout: OriginReadHeaderTimeout,
			WriteTimeout:      OriginWriteTimeout,
			IdleTimeout:       originIdleTimeout,
			MaxHeaderBytes:    originMaxHeaderBytes,
			ErrorLog:          log.New(serverLog{}, "", 0),
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	var wg sync.WaitGroup
	run := func(f func(context.Context)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f(ctx)
		}()
	}
	for _, p := range r.preparers {
		run(p.Run)
	}
	run(r.Health.Run)
	run(r.Usage.Run)
	run(func(ctx context.Context) { gaugeQueue(ctx, store, byID) })
	if r.origin != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := r.origin.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logx.Errorf("cdn: origin stopped: %v", err)
			}
		}()
	}
	go func() {
		wg.Wait()
		close(r.done)
	}()
	return r, nil
}

// Stop stops the origin (the requests it serves get originShutdown to
// end), the preparers, the checks and the budget, and waits for them; the
// traffic counted is written a last time. A preparation cut short is
// claimed again when its lease runs out.
func (r *Runtime) Stop() {
	if r.origin != nil {
		ctx, cancel := context.WithTimeout(context.Background(), originShutdown)
		if err := r.origin.Shutdown(ctx); err != nil {
			_ = r.origin.Close()
		}
		cancel()
	}
	r.cancel()
	<-r.done
}

// gaugeQueue sets metricQueue from the queues of the targets every
// queueGaugeEvery.
func gaugeQueue(ctx context.Context, store Store, targets map[int32]Target) {
	t := time.NewTicker(queueGaugeEvery)
	defer t.Stop()
	for {
		counts := map[ReplicaState]int64{}
		var err error
		for id := range targets {
			var c map[ReplicaState]int64
			if c, err = store.QueueCounts(ctx, id); err != nil {
				break
			}
			for s, n := range c {
				counts[s] += n
			}
		}
		if err == nil {
			for state, name := range stateNames {
				metricQueue.Set(float64(counts[state]), name)
			}
		} else if ctx.Err() == nil {
			logx.WithContext(ctx).Errorf("cdn: the queue of the preparations: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// serverLog takes what net/http logs about the connections of the origin
// into the log of the process.
type serverLog struct{}

func (serverLog) Write(p []byte) (int, error) {
	logx.Errorf("cdn origin: %s", strings.TrimSpace(string(p)))
	return len(p), nil
}
