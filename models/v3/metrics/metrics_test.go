package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

func TestRecordSendMessage(t *testing.T) {
	started := time.Now()
	elapsed := time.Since(started)
	Reset()
	RecordSendMessageSuccess(elapsed)
	assert.Equal(t, 1.0, testutil.ToFloat64(messageSentCount.WithLabelValues("true")))
	assert.Equal(t, 0.0, testutil.ToFloat64(messageSentCount.WithLabelValues("false")))
	RecordSendMessageSuccess(elapsed)
	assert.Equal(t, 2.0, testutil.ToFloat64(messageSentCount.WithLabelValues("true")))
	assert.Equal(t, 0.0, testutil.ToFloat64(messageSentCount.WithLabelValues("false")))
	RecordSendMessageSuccess(elapsed)
	assert.Equal(t, 3.0, testutil.ToFloat64(messageSentCount.WithLabelValues("true")))
	assert.Equal(t, 0.0, testutil.ToFloat64(messageSentCount.WithLabelValues("false")))
	RecordSendMessageFailure(elapsed)
	assert.Equal(t, 3.0, testutil.ToFloat64(messageSentCount.WithLabelValues("true")))
	assert.Equal(t, 1.0, testutil.ToFloat64(messageSentCount.WithLabelValues("false")))
}
