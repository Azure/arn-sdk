package metrics

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/Azure/arn-sdk/models/v3/schema/types"
)

const (
	subsystem    = "arn"
	successLabel = "success"
	inlineLabel  = "inline"
	timeoutLabel = "timeout"
)

var (
	eventSentCount      metric.Int64Counter
	eventSentBytesCount metric.Int64Counter
	eventSentLatency    metric.Int64Histogram

	currentPromiseCount metric.Int64UpDownCounter
	promiseCount        metric.Int64Counter
	promiseLatency      metric.Int64Histogram
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

	eventSentBytesCount, err = meter.Int64Counter(metricName("event_sent_bytes_total"), metric.WithDescription("total number of bytes in event data sent by the ARN client"))
	if err != nil {
		return err
	}

	// TODO: adjust buckets
	eventSentLatency, err = meter.Int64Histogram(
		metricName("event_sent_ms"),
		metric.WithDescription("time spent to send ARN event"),
		metric.WithExplicitBucketBoundaries(50, 100, 200, 400, 600, 800, 1000, 1250, 1500, 2000, 3000, 4000, 5000, 10000, 60000, 300000, 600000),
	)
	if err != nil {
		return err
	}

	promiseCount, err = meter.Int64Counter(metricName("promise_total"), metric.WithDescription("total number of promises made by the ARN client"))
	if err != nil {
		return err
	}

	currentPromiseCount, err = meter.Int64UpDownCounter(metricName("current_promise_count"), metric.WithDescription("current number of promises made by the ARN client"))
	if err != nil {
		return err
	}

	// TODO: adjust buckets
	promiseLatency, err = meter.Int64Histogram(
		metricName("promise_ms"),
		metric.WithDescription("time elapsed between checking for promise and promise completion"),
		metric.WithExplicitBucketBoundaries(50, 100, 200, 400, 600, 800, 1000, 1250, 1500, 2000, 3000, 4000, 5000, 10000, 60000, 300000, 600000),
	)
	if err != nil {
		return err
	}

	return nil
}

// SendEventSuccess increases the eventSentCount metric with success == true
// and records the latency
func SendEventSuccess(ctx context.Context, elapsed time.Duration, inline types.ResourcesContainer, dataSize int64) {
	opt := metric.WithAttributes(
		attribute.Key(successLabel).Bool(true),
		attribute.Key(inlineLabel).String(strings.Trim(inline.String(), "\"")),
	)
	if eventSentCount != nil {
		eventSentCount.Add(ctx, 1, opt)
	}
	if eventSentBytesCount != nil {
		eventSentBytesCount.Add(ctx, dataSize, opt)
	}
	if eventSentLatency != nil {
		eventSentLatency.Record(ctx, elapsed.Milliseconds(), opt)
	}
}

// SendEventFailure increases the eventSentCount metric with success == false
// and records the latency
func SendEventFailure(ctx context.Context, elapsed time.Duration, inline types.ResourcesContainer, dataSize int64) {
	opt := metric.WithAttributes(
		attribute.Key(successLabel).Bool(false),
		attribute.Key(inlineLabel).String(strings.Trim(inline.String(), "\"")),
	)
	if eventSentCount != nil {
		eventSentCount.Add(ctx, 1, opt)
	}
	if eventSentBytesCount != nil {
		eventSentBytesCount.Add(ctx, dataSize, opt)
	}
	if eventSentLatency != nil {
		eventSentLatency.Record(ctx, elapsed.Milliseconds(), opt)
	}
}

// Promise increases the promiseCount metric with timeout label,
// and records the approximate latency (which may be an underestimation).
// This also decrements the current promise count.
// This should be called on promise completion.
func Promise(ctx context.Context, elapsed time.Duration, timeout bool) {
	opt := metric.WithAttributes(
		attribute.Key(timeoutLabel).Bool(timeout),
	)
	if promiseCount != nil {
		promiseCount.Add(ctx, 1, opt)
	}
	if currentPromiseCount != nil {
		currentPromiseCount.Add(ctx, -1, metric.WithAttributes())
	}
}

// ActivePromise increases the currentPromiseCount metric.
// This should be called when a promise is sent.
func ActivePromise(ctx context.Context) {
	if currentPromiseCount != nil {
		currentPromiseCount.Add(ctx, 1, metric.WithAttributes())
	}
}
