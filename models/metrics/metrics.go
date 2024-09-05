package metrics

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/Azure/arn-sdk/models"
)

const (
	subsystem    = "arn-sdk"
	successLabel = "success"
	errorLabel   = "error"
	inlineLabel  = "inline"
	timeoutLabel = "timeout"
)

type eventMetrics struct {
	sent    metric.Int64Counter
	bytes   metric.Int64Counter
	latency metric.Int64Histogram
}

type promiseMetrics struct {
	current   metric.Int64UpDownCounter
	completed metric.Int64Counter
}

var (
	events   eventMetrics
	promises promiseMetrics
)

func metricName(name string) string {
	return fmt.Sprintf("%s_%s", subsystem, name)
}

// Init initializes the arn sdk model metrics. This should only be called by the tattler constructor or tests.
func Init(meter metric.Meter) error {
	var err error
	events.sent, err = meter.Int64Counter(metricName("event_sent_total"), metric.WithDescription("total number of events sent by the ARN client"))
	if err != nil {
		return err
	}

	events.bytes, err = meter.Int64Counter(metricName("event_sent_bytes_total"), metric.WithDescription("total number of bytes in event data sent by the ARN client"))
	if err != nil {
		return err
	}

	// TODO: adjust buckets
	events.latency, err = meter.Int64Histogram(
		metricName("event_sent_ms"),
		metric.WithDescription("time spent to send ARN event"),
		metric.WithExplicitBucketBoundaries(50, 100, 200, 400, 600, 800, 1000, 1250, 1500, 2000, 3000, 4000, 5000, 10000, 60000, 300000, 600000),
	)
	if err != nil {
		return err
	}

	promises.completed, err = meter.Int64Counter(metricName("promise_total"), metric.WithDescription("total number of promises made by the ARN client"))
	if err != nil {
		return err
	}

	promises.current, err = meter.Int64UpDownCounter(metricName("current_promise_count"), metric.WithDescription("current number of promises made by the ARN client"))
	if err != nil {
		return err
	}

	return nil
}

// SendEventSuccess increases the events.sent metric with success == true
// and records the latency.
func SendEventSuccess(ctx context.Context, elapsed time.Duration, inline bool, dataSize int64) {
	opt := metric.WithAttributes(
		attribute.Key(successLabel).Bool(true),
		attribute.Key(inlineLabel).Bool(inline),
	)
	if events.sent != nil {
		events.sent.Add(ctx, 1, opt)
	}
	if events.bytes != nil {
		events.bytes.Add(ctx, dataSize, opt)
	}
	if events.latency != nil {
		events.latency.Record(ctx, elapsed.Milliseconds(), opt)
	}
}

// SendEventFailure increases the events.sent metric with success == false
// and records the latency.
func SendEventFailure(ctx context.Context, elapsed time.Duration, inline bool, dataSize int64) {
	opt := metric.WithAttributes(
		attribute.Key(successLabel).Bool(false),
		attribute.Key(inlineLabel).Bool(inline),
	)
	if events.sent != nil {
		events.sent.Add(ctx, 1, opt)
	}
	if events.bytes != nil {
		events.bytes.Add(ctx, dataSize, opt)
	}
	if events.latency != nil {
		events.latency.Record(ctx, elapsed.Milliseconds(), opt)
	}
}

// Promise increases the promises.completed metric with timeout label.
// This also decrements the current promise count.
// This should be called on promise completion.
func Promise(ctx context.Context, err error) {
	var isErr, isTimeout bool
	if err != nil {
		isErr = true
		if errors.Is(err, models.ErrPromiseTimeout) {
			isTimeout = true
		}
	}
	opt := metric.WithAttributes(
		attribute.Key(errorLabel).Bool(isErr),
		attribute.Key(timeoutLabel).Bool(isTimeout),
	)
	if promises.completed != nil {
		promises.completed.Add(ctx, 1, opt)
	}
	if promises.current != nil {
		promises.current.Add(ctx, -1)
	}
}

// ActivePromise increases the promises.current metric.
// This should be called when a promise is sent.
func ActivePromise(ctx context.Context) {
	if promises.current != nil {
		promises.current.Add(ctx, 1)
	}
}
