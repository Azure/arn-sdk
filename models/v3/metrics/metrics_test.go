package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

func TestRecordSendEvent(t *testing.T) {
	started := time.Now()
	elapsed := time.Since(started)
	Reset()
	RecordSendEventSuccess(elapsed)
	assert.Equal(t, 1.0, testutil.ToFloat64(eventSentCount.WithLabelValues("true")))
	assert.Equal(t, 0.0, testutil.ToFloat64(eventSentCount.WithLabelValues("false")))
	RecordSendEventSuccess(elapsed)
	assert.Equal(t, 2.0, testutil.ToFloat64(eventSentCount.WithLabelValues("true")))
	assert.Equal(t, 0.0, testutil.ToFloat64(eventSentCount.WithLabelValues("false")))
	RecordSendEventSuccess(elapsed)
	assert.Equal(t, 3.0, testutil.ToFloat64(eventSentCount.WithLabelValues("true")))
	assert.Equal(t, 0.0, testutil.ToFloat64(eventSentCount.WithLabelValues("false")))
	RecordSendEventFailure(elapsed)
	assert.Equal(t, 3.0, testutil.ToFloat64(eventSentCount.WithLabelValues("true")))
	assert.Equal(t, 1.0, testutil.ToFloat64(eventSentCount.WithLabelValues("false")))
}
