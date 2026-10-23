package dao

import "github.com/zeromicro/go-zero/core/metric"

// Failures of the steps that follow a commit (ADR 0011, p. 2): they do not
// fail the operation, so they are counted.
var metricPostCommitErrors = metric.NewCounterVec(&metric.CounterVecOpts{
	Namespace: "msg",
	Name:      "post_commit_errors_total",
	Help:      "Failures after a box transaction committed, by kind.",
	Labels:    []string{"kind"}, // cache
})
