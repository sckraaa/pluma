package dao

import "github.com/zeromicro/go-zero/core/metric"

// Metrics of the steps that follow a commit and of the inbox consumer
// (ADR 0011, p. 2): failures there do not fail the operation, so they are
// counted.
var (
	metricPostCommitErrors = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "msg",
		Name:      "post_commit_errors_total",
		Help:      "Failures after a box transaction committed, by kind.",
		Labels:    []string{"kind"}, // publish, cache, idempotency
	})

	metricInboxHandlerErrors = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "msg",
		Name:      "inbox_handler_errors_total",
		Help:      "Inbox events that were not applied, by method.",
		Labels:    []string{"method"},
	})

	metricIdempotencyBypass = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "msg",
		Name:      "idempotency_bypass_total",
		Help:      "Sends done without the idempotency window because Redis failed.",
		Labels:    []string{},
	})
)

// InboxHandlerFailed counts an inbox event that was not applied.
func InboxHandlerFailed(method string) {
	metricInboxHandlerErrors.Inc(method)
}
