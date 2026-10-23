package outbox

import "github.com/zeromicro/go-zero/core/metric"

// Metrics of the relay (ADR 0012, p. 10).
var (
	metricRows = metric.NewGaugeVec(&metric.GaugeVecOpts{
		Namespace: "outbox",
		Name:      "rows",
		Help:      "Rows of mq_outbox by state, as the relay leader counted them.",
		Labels:    []string{"state"},
	})
	metricOldest = metric.NewGaugeVec(&metric.GaugeVecOpts{
		Namespace: "outbox",
		Name:      "oldest_unapplied_seconds",
		Help:      "Age of the oldest pending or published row, in seconds; 0 without one.",
		Labels:    []string{},
	})
	metricPublished = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "outbox",
		Name:      "published_total",
		Help:      "Rows the broker took.",
		Labels:    []string{"topic"},
	})
	metricPublishErrors = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "outbox",
		Name:      "publish_errors_total",
		Help:      "Rows the broker answered with an error, by kind.",
		Labels:    []string{"topic", "kind"}, // transient, permanent, topic
	})
	metricDropped = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "outbox",
		Name:      "dropped_total",
		Help:      "Push rows deleted without delivery, by reason.",
		Labels:    []string{"topic", "reason"},
	})
	metricRedelivered = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "outbox",
		Name:      "redelivered_total",
		Help:      "Published inbox rows returned to pending, by reason.",
		Labels:    []string{"topic", "reason"}, // skipped, log_lost, leader
	})
	metricAutoReplays = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "outbox",
		Name:      "auto_replays_total",
		Help:      "Lanes with dead rows the relay returned to pending.",
		Labels:    []string{},
	})
	metricPublishDelay = metric.NewHistogramVec(&metric.HistogramVecOpts{
		Namespace: "outbox",
		Name:      "publish_delay_seconds",
		Help:      "From the event's creation to the broker's answer, in seconds.",
		Labels:    []string{"topic"},
		Buckets:   []float64{0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60},
	})
	metricRoundSeconds = metric.NewHistogramVec(&metric.HistogramVecOpts{
		Namespace: "outbox",
		Name:      "round_seconds",
		Help:      "Duration of the phases of a relay round, in seconds.",
		Labels:    []string{"phase"}, // select, send, finish
		Buckets:   []float64{0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	})
	metricLeader = metric.NewGaugeVec(&metric.GaugeVecOpts{
		Namespace: "outbox",
		Name:      "relay_leader",
		Help:      "1 while this relay holds the lock and publishes.",
		Labels:    []string{},
	})
	metricConnected = metric.NewGaugeVec(&metric.GaugeVecOpts{
		Namespace: "outbox",
		Name:      "relay_connected",
		Help:      "1 while the relay leader has a connection to Kafka.",
		Labels:    []string{},
	})
	metricTopicReady = metric.NewGaugeVec(&metric.GaugeVecOpts{
		Namespace: "outbox",
		Name:      "topic_ready",
		Help:      "1 when the topic exists with the expected partitions; its rows wait otherwise.",
		Labels:    []string{"topic"},
	})
	metricGroupLag = metric.NewGaugeVec(&metric.GaugeVecOpts{
		Namespace: "kafka",
		Name:      "group_lag",
		Help:      "Messages of the topic the consumer group has not committed, as the broker sees it.",
		Labels:    []string{"group", "topic"},
	})
)

func gaugeBool(g metric.GaugeVec, v bool, labels ...string) {
	if v {
		g.Set(1, labels...)
	} else {
		g.Set(0, labels...)
	}
}
