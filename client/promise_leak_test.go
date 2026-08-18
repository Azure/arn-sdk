package client

import (
	"testing"
	"time"

	"github.com/Azure/arn-sdk/models"
	modelmetrics "github.com/Azure/arn-sdk/models/metrics"
	"github.com/Azure/arn-sdk/models/v3/msgs"
	"github.com/Azure/arn-sdk/models/v3/schema/types"
	"github.com/gostdlib/base/context"
	"github.com/prometheus/client_golang/prometheus"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// activePromises reads the current_promise_count gauge out of the registry.
func activePromises(t *testing.T, reg *prometheus.Registry) float64 {
	t.Helper()

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("activePromises: Gather(): got err == %s, want err == nil", err)
	}
	for _, family := range families {
		if family.GetName() != "arn_sdk_current_promise_count" {
			continue
		}
		for _, m := range family.GetMetric() {
			return m.GetGauge().GetValue()
		}
	}
	return 0
}

// TestNotifyPromiseAccounting pins that Notify leaves no promise outstanding. Promise() deliberately
// keeps the gauge up on a timeout so an Async caller can wait again, but Notify never hands the
// notification back, so a timed-out promise there is unrecoverable and must be settled.
func TestNotifyPromiseAccounting(t *testing.T) {
	reg := prometheus.NewRegistry()
	exporter, err := otelprometheus.New(otelprometheus.WithRegisterer(reg))
	if err != nil {
		t.Fatalf("TestNotifyPromiseAccounting: prometheus exporter: got err == %s, want err == nil", err)
	}
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	if err := modelmetrics.Init(provider.Meter("promiseaccounting")); err != nil {
		t.Fatalf("TestNotifyPromiseAccounting: Init(): got err == %s, want err == nil", err)
	}

	tests := []struct {
		name string
		// answer makes the fake sender resolve the promise; otherwise it accepts and stays silent.
		answer  bool
		wantErr bool
	}{
		{name: "Success: a resolved send leaves no promise outstanding", answer: true},
		{name: "Error: a timed out send leaves no promise outstanding", wantErr: true},
	}

	for _, test := range tests {
		a := &ARN{in: make(chan models.Notifications, 1), errs: make(chan error, 1)}
		done := make(chan struct{})
		go func() {
			for m := range a.in {
				if test.answer {
					m.SendPromise(nil, a.errs)
				}
			}
			close(done)
		}()

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		err := a.Notify(ctx, msgs.Notifications{Data: make([]types.NotificationResource, 1)})
		cancel()
		close(a.in)
		<-done

		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestNotifyPromiseAccounting(%s): got err == nil, want err != nil", test.name)
			continue
		case err != nil && !test.wantErr:
			t.Errorf("TestNotifyPromiseAccounting(%s): got err == %s, want err == nil", test.name, err)
			continue
		}

		if got := activePromises(t, reg); got != 0 {
			t.Errorf("TestNotifyPromiseAccounting(%s): got %v promises outstanding, want 0", test.name, got)
		}
	}
}
