package metrics

import (
	"time"
	"fmt"
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	subsystem      = "arn"
	successLabel   = "success"
)
var (
	eventSentCount          metric.Int64Counter
	eventSentLatency        metric.Int64Histogram
)

func metricName(name string) string {
	return fmt.Sprintf("%s_%s", subsystem, name)
}

// Init initializes the arn model metrics. This should only be called by the tattler constructor or tests.
func Init(meter metric.Meter) error {
	var err error
	eventSentCount, err = meter.Int64Counter(metricName("event_sent_total"), metric.WithDescription("total number of events sent by the ARN client"))
	if err != nil {
		return err
	}
	// TODO: adjust buckets
	eventSentLatency, err = meter.Int64Histogram(
		metricName("event_sent_ms"),
		metric.WithDescription("age of batch when emitted"),
		metric.WithExplicitBucketBoundaries(50, 100, 200, 400, 600, 800, 1000, 1250, 1500, 2000, 3000, 4000, 5000, 10000, 60000, 300000, 600000),
	)
	return nil
}

// RecordSendEventSuccess increases the eventSentCount metric with success == true
// and records the latency
// pass []entry.Data here and check entry creation time for latency?
func RecordSendEventSuccess(ctx context.Context, elapsed time.Duration) {
	opt := metric.WithAttributes(
		attribute.Key(successLabel).String("true"),
	)
	if eventSentCount != nil {
		eventSentCount.Add(ctx, 1, opt)
	}
	if eventSentLatency != nil {
		eventSentLatency.Record(ctx, elapsed.Milliseconds(), opt)
	}
}

// RecordSendEventFailure increases the eventSentCount metric with success == false
// and records the latency
func RecordSendEventFailure(ctx context.Context, elapsed time.Duration) {
	opt := metric.WithAttributes(
		attribute.Key(successLabel).String("false"),
	)
	if eventSentCount != nil {
		eventSentCount.Add(ctx, 1, opt)
	}
	if eventSentLatency != nil {
		eventSentLatency.Record(ctx, elapsed.Milliseconds(), opt)
	}
}
