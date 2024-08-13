package metrics

import (
	"time"

	prom "github.com/prometheus/client_golang/prometheus"
)

const (
	subsystem      = "arn_sdk"
	successLabel   = "success"
)

var (
	eventSentCount = prom.NewCounterVec(prom.CounterOpts{
		Name:      "event_sent_total",
		Help:      "total number of events sent by the ARN client",
		Subsystem: subsystem,
	}, []string{successLabel})

	eventSentLatency = prom.NewHistogramVec(prom.HistogramOpts{
		Name:      "event_sent_seconds",
		Help:      "latency distributions of events sent by the ARN client",
		Subsystem: subsystem,
		Buckets: []float64{0.05, 0.1, 0.2, 0.4, 0.6, 0.8, 1.0, 1.25, 1.5, 2, 3,
						4, 5, 6, 8, 10, 15, 20, 30, 45, 60},
	}, []string{})
)

func Init(reg prom.Registerer) {
	reg.MustRegister(
		eventSentCount,
	)
}

// RecordSendEventSuccess increases the eventSentCount metric with success == true
// and records the latency
func RecordSendEventSuccess(elapsed time.Duration) {
	eventSentCount.With(
		prom.Labels{
			successLabel: "true",
		}).Inc()
	eventSentLatency.WithLabelValues().Observe(elapsed.Seconds())
}

// RecordSendEventFailure increases the eventSentCount metric with success == false
// and records the latency
func RecordSendEventFailure(elapsed time.Duration) {
	eventSentCount.With(
		prom.Labels{
			successLabel: "false",
		}).Inc()
	eventSentLatency.WithLabelValues().Observe(elapsed.Seconds())
}

// Reset resets the metrics
func Reset() {
	eventSentCount.Reset()
	eventSentLatency.Reset()
}
