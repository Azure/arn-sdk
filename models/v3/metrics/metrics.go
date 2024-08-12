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
	messageSentCount = prom.NewCounterVec(prom.CounterOpts{
		Name:      "message_sent_total",
		Help:      "total number of messages sent by the ARN client",
		Subsystem: subsystem,
	}, []string{successLabel})

	messageSentLatency = prom.NewHistogramVec(prom.HistogramOpts{
		Name:      "message_sent_seconds",
		Help:      "latency distributions of messages sent by the ARN client",
		Subsystem: subsystem,
		Buckets: []float64{0.05, 0.1, 0.2, 0.4, 0.6, 0.8, 1.0, 1.25, 1.5, 2, 3,
						4, 5, 6, 8, 10, 15, 20, 30, 45, 60},
	}, []string{})
)

func Init(reg prom.Registerer) {
	reg.MustRegister(
		messageSentCount,
	)
}

// IncSendMessageSuccessCount increases the MessageSentCount metric with success == true
func RecordSendMessageSuccess(elapsed time.Duration) {
	messageSentCount.With(
		prom.Labels{
			successLabel: "true",
		}).Inc()
	messageSentLatency.WithLabelValues().Observe(elapsed.Seconds())
}

// IncSendMessageFailureCount increases the MessageSentCount metric with success == false
func RecordSendMessageFailure(elapsed time.Duration) {
	messageSentCount.With(
		prom.Labels{
			successLabel: "false",
		}).Inc()
	messageSentLatency.WithLabelValues().Observe(elapsed.Seconds())
}

func Reset() {
	messageSentCount.Reset()
	messageSentLatency.Reset()
}
