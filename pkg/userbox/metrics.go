package userbox

import "github.com/zeromicro/go-zero/core/metric"

// The SQL metrics of go-zero are off (pkg/logpolicy), so Run reports its own.
var (
	txBuckets = []float64{0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

	metricTxSeconds = metric.NewHistogramVec(&metric.HistogramVecOpts{
		Namespace: "userbox",
		Name:      "tx_seconds",
		Help:      "Duration of userbox.Run with all its attempts, in seconds.",
		Labels:    []string{},
		Buckets:   txBuckets,
	})

	metricLockWaitSeconds = metric.NewHistogramVec(&metric.HistogramVecOpts{
		Namespace: "userbox",
		Name:      "lock_wait_seconds",
		Help:      "Time to lock the user_state row of a box transaction, in seconds.",
		Labels:    []string{},
		Buckets:   txBuckets,
	})

	metricTxRetries = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "userbox",
		Name:      "tx_retries_total",
		Help:      "Box transactions tried again, by cause.",
		Labels:    []string{"code"}, // 1213, 1205, commit
	})

	metricTxErrors = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "userbox",
		Name:      "tx_errors_total",
		Help:      "Box transactions failed for a reason other than the operation's own error, by cause.",
		Labels:    []string{"code"}, // MySQL error number, commit_unknown, context, ...
	})

	metricCommitUnknown = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "userbox",
		Name:      "commit_unknown_total",
		Help:      "Failed COMMITs of box transactions, by what checking them found.",
		Labels:    []string{"outcome"}, // applied, not_applied, unknown
	})
)
