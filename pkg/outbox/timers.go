package outbox

import (
	"context"
	"time"

	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// copyFate is what the broker says about a published copy of an inbox row
// that is still not applied.
type copyFate uint8

const (
	copyAhead   copyFate = iota // the consumer has not reached it yet
	copySkipped                 // the consumer passed it without applying it: a fault
	copyLost                    // it is not in the log (a lost volume, a topic made again)
)

// fateOf decides from the copy's offset, the group's committed offset of
// its partition (-1 without one) and the bounds of the partition's log.
func fateOf(offset, committed, first, end int64) copyFate {
	switch {
	case committed > offset:
		return copySkipped
	case first > offset || end <= offset:
		return copyLost
	}
	return copyAhead
}

func (f copyFate) String() string {
	switch f {
	case copySkipped:
		return "skipped"
	case copyLost:
		return "log_lost"
	}
	return "ahead"
}

func (r *Relay) groupOf(topic string) string {
	for _, g := range r.conf.Groups {
		if g.Topic == topic {
			return g.Group
		}
	}
	return ""
}

// redeliver looks at the published inbox rows due for a check (ADR 0012,
// p. 3): a copy the consumer passed or the log lost is published again; a
// copy not reached yet is left alone, so a stopped partition does not get
// its tail written twice.
func (r *Relay) redeliver(ctx context.Context) {
	dbctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), checkTimeout)
	defer cancel()
	now := r.now()
	due, err := r.store.DueForCheck(dbctx, now.UnixMilli(), checkBatch)
	if err != nil {
		r.log.Errorf("outbox: select published rows to check: %v", err)
		return
	}
	type key struct {
		topic     string
		partition int32
	}
	committed := map[string]map[int32]int64{}
	bounds := map[key][2]int64{}
	redelivered := false
	for _, p := range due {
		group := r.groupOf(p.topic)
		if group == "" {
			continue
		}
		offsets, ok := committed[p.topic]
		if !ok {
			partitions := make([]int32, Partitions)
			for i := range partitions {
				partitions[i] = int32(i)
			}
			if offsets, err = r.kafka.Committed(group, p.topic, partitions); err != nil {
				r.log.Errorf("outbox: committed offsets of %s on %s: %v", group, p.topic, err)
				return
			}
			committed[p.topic] = offsets
		}
		k := key{p.topic, p.partition}
		b, ok := bounds[k]
		if !ok {
			first, end, err := r.kafka.Bounds(p.topic, p.partition)
			if err != nil {
				r.log.Errorf("outbox: bounds of %s/%d: %v", p.topic, p.partition, err)
				return
			}
			b = [2]int64{first, end}
			bounds[k] = b
		}
		c, ok := offsets[p.partition]
		if !ok {
			c = -1
		}
		ref := ref{id: p.id, revision: p.revision}
		switch fate := fateOf(p.offset, c, b[0], b[1]); fate {
		case copyAhead:
			if err = r.store.Postpone(dbctx, ref, now.Add(recheckDelay).UnixMilli()); err != nil {
				r.log.Errorf("outbox: postpone the check of event %d: %v", p.id, err)
			}
		default:
			ok, err := r.store.Redeliver(dbctx, ref)
			if err != nil {
				r.log.Errorf("outbox: redeliver event %d: %v", p.id, err)
				continue
			}
			if ok {
				redelivered = true
				metricRedelivered.Inc(p.topic, fate.String())
				r.log.Errorf("outbox: event %d of %s published at %d/%d is not applied and its copy is %s: published again",
					p.id, p.topic, p.partition, p.offset, fate)
			}
		}
	}
	if redelivered {
		Notify()
	}
}

// replay returns the dead lanes due for it to pending (all of them when
// the relay has just taken the lock): a fix deployed since takes the lane
// out of parking by itself; an event that fails again is dead again with a
// longer delay (userbox.ReplayDelay).
func (r *Relay) replay(ctx context.Context, all bool) {
	dbctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), queryTimeout)
	defer cancel()
	var now int64
	if !all {
		now = r.now().UnixMilli()
	}
	lanes, err := r.store.DeadLanes(dbctx, now)
	if err != nil {
		r.log.Errorf("outbox: select dead lanes: %v", err)
		return
	}
	for _, l := range lanes {
		lctx, lcancel := context.WithTimeout(context.WithoutCancel(ctx), queryTimeout)
		n, err := r.store.ReplayLane(lctx, l, true)
		lcancel()
		if err != nil {
			r.log.Errorf("outbox: replay the lane of user %d on %s: %v", l.Recipient, l.Topic, err)
			continue
		}
		if n > 0 {
			metricAutoReplays.Inc()
			r.log.Infof("outbox: %d dead and parked events of user %d on %s replayed", n, l.Recipient, l.Topic)
		}
	}
}

// counts reports the rows by state and the age of the oldest unapplied.
func (r *Relay) counts(ctx context.Context) {
	dbctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), queryTimeout)
	defer cancel()
	c, err := r.store.Counts(dbctx)
	if err != nil {
		r.log.Errorf("outbox: count rows: %v", err)
		return
	}
	for _, st := range []userbox.EventState{userbox.EventPending, userbox.EventPublished, userbox.EventDead, userbox.EventParked} {
		metricRows.Set(float64(c.rows[st]), st.String())
	}
	age := 0.0
	if c.oldest > 0 {
		age = max(r.now().Sub(time.UnixMilli(c.oldest)).Seconds(), 0)
	}
	metricOldest.Set(age)
}

// lag reports the lag of the consumer groups as the broker sees it: it
// does not depend on the consumers being alive.
func (r *Relay) lag() {
	partitions := make([]int32, Partitions)
	for i := range partitions {
		partitions[i] = int32(i)
	}
	for _, g := range r.conf.Groups {
		committed, err := r.kafka.Committed(g.Group, g.Topic, partitions)
		if err != nil {
			r.log.Errorf("outbox: committed offsets of %s on %s: %v", g.Group, g.Topic, err)
			continue
		}
		var lag int64
		for _, p := range partitions {
			first, end, err := r.kafka.Bounds(g.Topic, p)
			if err != nil {
				r.log.Errorf("outbox: bounds of %s/%d: %v", g.Topic, p, err)
				lag = -1
				break
			}
			c := committed[p]
			if c < first {
				c = first
			}
			lag += max(end-c, 0)
		}
		if lag >= 0 {
			metricGroupLag.Set(float64(lag), g.Group, g.Topic)
		}
	}
}
