package userbox

import (
	"testing"

	prom "github.com/prometheus/client_golang/prometheus"
	"github.com/zeromicro/go-zero/core/prometheus"
)

// go-zero records metrics only once Prometheus is enabled.
func init() { prometheus.Enable() }

// counter returns the value of a userbox counter with the given label value
// (the counters have one label).
func counter(t testing.TB, name, label string) float64 {
	t.Helper()
	families, err := prom.DefaultGatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if f.GetName() != name {
			continue
		}
		for _, m := range f.GetMetric() {
			if lp := m.GetLabel(); len(lp) == 1 && lp[0].GetValue() == label {
				return m.GetCounter().GetValue()
			}
		}
	}
	return 0
}

func TestMetricsRecorded(t *testing.T) {
	s := newFakeServer()
	db := s.open(t)
	before := counter(t, "userbox_tx_retries_total", "1213")
	s.failNext("write_other", injection{err: mysqlErr(1213)})
	if _, err := Run(t.Context(), db, 7, sendMessage); err != nil {
		t.Fatal(err)
	}
	if got := counter(t, "userbox_tx_retries_total", "1213") - before; got != 1 {
		t.Errorf("userbox_tx_retries_total{code=1213} grew by %v, want 1", got)
	}

	before = counter(t, "userbox_tx_errors_total", "1062")
	s.failNext("write_other", injection{err: mysqlErr(1062)})
	if _, err := Run(t.Context(), db, 7, sendMessage); err == nil {
		t.Fatal("no error")
	}
	if got := counter(t, "userbox_tx_errors_total", "1062") - before; got != 1 {
		t.Errorf("userbox_tx_errors_total{code=1062} grew by %v, want 1", got)
	}
}
