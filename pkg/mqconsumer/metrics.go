package mqconsumer

import "github.com/zeromicro/go-zero/core/metric"

// Metrics of the consumers (ADR 0012, p. 10).
var (
	metricEvents = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "mq_consumer",
		Name:      "events_total",
		Help:      "Messages done, by outcome.",
		Labels:    []string{"topic", "outcome"}, // applied, duplicate, deferred, parked, dead, skipped, stale, mismatch
	})
	metricRetries = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "mq_consumer",
		Name:      "retries_total",
		Help:      "Failed attempts of messages, by class of the error.",
		Labels:    []string{"topic", "class"}, // transient, deterministic
	})
	metricStuck = metric.NewGaugeVec(&metric.GaugeVecOpts{
		Namespace: "mq_consumer",
		Name:      "stuck_seconds",
		Help:      "How long the message at the head of a partition has been failing, in seconds.",
		Labels:    []string{"topic", "partition"},
	})
	metricAssigned = metric.NewGaugeVec(&metric.GaugeVecOpts{
		Namespace: "mq_consumer",
		Name:      "assigned_partitions",
		Help:      "Partitions assigned to this instance.",
		Labels:    []string{"group"},
	})
	metricAge = metric.NewHistogramVec(&metric.HistogramVecOpts{
		Namespace: "mq_consumer",
		Name:      "event_age_seconds",
		Help:      "From the record's timestamp (the event's creation) to the message done, in seconds.",
		Labels:    []string{"topic"},
		Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 300},
	})
)
