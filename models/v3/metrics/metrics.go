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

// Init initializes the batching metrics. This should only be called by the tattler constructor or tests.
func Init(meter metric.Meter) error {
	var err error
	// eventSentCount = prom.NewCounterVec(prom.CounterOpts{
	// 	Name:      "event_sent_total",
	// 	Help:      "total number of events sent by the ARN client",
	// 	Subsystem: subsystem,
	// }, []string{successLabel})
	eventSentCount, err = meter.Int64Counter(metricName("event_sent_total"), metric.WithDescription("total number of events sent by the ARN client"))
	if err != nil {
		return err
	}


	// eventSentLatency = prom.NewHistogramVec(prom.HistogramOpts{
	// 	Name:      "event_sent_seconds",
	// 	Help:      "latency distributions of events sent by the ARN client",
	// 	Subsystem: subsystem,
	// 	Buckets: []float64{0.05, 0.1, 0.2, 0.4, 0.6, 0.8, 1.0, 1.25, 1.5, 2, 3,
	// 					4, 5, 6, 8, 10, 15, 20, 30, 45, 60},
	// }, []string{})

	eventSentLatency, err = meter.Int64Histogram(
		metricName("event_sent_ms"),
		metric.WithDescription("age of batch when emitted"),
		metric.WithExplicitBucketBoundaries(50, 100, 200, 400, 600, 800, 1000, 1250, 1500, 2000, 3000, 4000, 5000, 10000),
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

// package batching

// import (
// 	"context"
// 	"fmt"
// 	"time"

// 	"go.opentelemetry.io/otel/attribute"
// 	"go.opentelemetry.io/otel/metric"
// 	api "go.opentelemetry.io/otel/metric"

// 	"github.com/Azure/tattler/data"
// )

// const (
// 	subsystem       = "tattler"
// 	sourceTypeLabel = "source_type"
// 	successLabel    = "success"
// )

// var (
// 	batchingCount          metric.Int64Counter
// 	batchesEmittedCount    metric.Int64Counter
// 	batchItemsEmittedCount metric.Int64Counter
// 	batchAgeMilliseconds   metric.Int64Histogram
// )

// func metricName(name string) string {
// 	return fmt.Sprintf("%s_%s", subsystem, name)
// }

// // Init initializes the batching metrics.
// func Init(meter api.Meter) error {
// 	var err error
// 	batchingCount, err = meter.Int64Counter(metricName("batching_total"), api.WithDescription("total number of times tattler handles batching input"))
// 	if err != nil {
// 		return err
// 	}
// 	batchesEmittedCount, err = meter.Int64Counter(metricName("batches_emitted_total"), api.WithDescription("total number of batches emitted by tattler"))
// 	if err != nil {
// 		return err
// 	}
// 	batchItemsEmittedCount, err = meter.Int64Counter(metricName("batch_items_emitted_total"), api.WithDescription("total number of batch items emitted by tattler"))
// 	if err != nil {
// 		return err
// 	}
// 	batchAgeMilliseconds, err = meter.Int64Histogram(
// 		metricName("batch_age_ms"),
// 		api.WithDescription("age of batch when emitted"),
// 		api.WithExplicitBucketBoundaries(50, 100, 200, 400, 600, 800, 1000, 1250, 1500, 2000, 3000, 4000, 5000, 10000),
// 	)

// 	return nil
// }

// // RecordBatchingSuccess records successful handling of batch input
// // so we can calculate error rate.
// func RecordBatchingSuccess(ctx context.Context) {
// 	opt := api.WithAttributes(
// 		attribute.Key(successLabel).String("true"),
// 	)
// 	if batchingCount != nil {
// 		batchingCount.Add(ctx, 1, opt)
// 	}
// }

// // RecordBatchingError records an error when handling batch input.
// func RecordBatchingError(ctx context.Context) {
// 	opt := api.WithAttributes(
// 		attribute.Key(successLabel).String("false"),
// 	)
// 	if batchingCount != nil {
// 		batchingCount.Add(ctx, 1, opt)
// 	}
// }

// // RecordBatchEmitted should be called when emitting a batch to record the batch emitted count, batch item emitted count,
// // and time elapsed from when the batch was created.
// func RecordBatchEmitted(ctx context.Context, sourceType data.SourceType, batchItemCount int, elapsed time.Duration) {
// 	opt := api.WithAttributes(
// 		attribute.Key(sourceTypeLabel).String(sourceType.String()),
// 	)
// 	// check if initialized first so someone doesn't have to initialize metrics
// 	if batchesEmittedCount != nil {
// 		batchesEmittedCount.Add(ctx, 1, opt)
// 	}
// 	if batchItemsEmittedCount != nil {
// 		batchItemsEmittedCount.Add(ctx, int64(batchItemCount), opt)
// 	}
// 	if batchAgeMilliseconds != nil {
// 		batchAgeMilliseconds.Record(ctx, elapsed.Milliseconds(), opt)
// 	}
// }
