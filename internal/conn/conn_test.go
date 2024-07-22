package conn

import (
	"context"
	"errors"
	"testing"

	"github.com/Azure/arn/internal/conn/http"
	"github.com/Azure/arn/internal/conn/storage"
	"github.com/Azure/arn/models"
)

type fakeNotify struct {
	ctx context.Context
	models.Notifications
	count    int
	ch       chan error
	eventErr bool
}

func newFakeNotify(ctx context.Context, count int, eventErr bool) fakeNotify {
	return fakeNotify{
		ctx:      ctx,
		ch:       make(chan error, 1),
		count:    count,
		eventErr: eventErr,
	}
}

func (f fakeNotify) Ctx() context.Context {
	return f.ctx
}

func (f fakeNotify) SendPromise(e error, backupCh chan error) {
	select {
	case f.ch <- e:
	default:
		panic("channel full")
	}
}

func (f fakeNotify) Promise(ctx context.Context) error {
	return <-f.ch
}

func (f fakeNotify) SendEvent(h *http.Client, s *storage.Client) error {
	if f.eventErr {
		return errors.New("event error")
	}
	return nil
}

func (f fakeNotify) DataCount() int {
	return f.count
}

func TestSend(t *testing.T) {
	t.Parallel()

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name           string
		notify         fakeNotify
		wantPromiseErr bool
	}{
		{
			name:           "Error: data count in > 1000",
			notify:         newFakeNotify(context.Background(), 1001, false),
			wantPromiseErr: true,
		},
		{
			name:           "Error: context cancelled",
			notify:         newFakeNotify(cancelCtx, 1000, false),
			wantPromiseErr: true,
		},
		{
			name:           "Error: event error",
			notify:         newFakeNotify(context.Background(), 1000, true),
			wantPromiseErr: true,
		},
		{
			name:           "Success",
			notify:         newFakeNotify(context.Background(), 1000, false),
			wantPromiseErr: false,
		},
	}

	s := &Service{in: make(chan models.Notifications, 1)}
	go s.sender()
	defer s.Close()

	for _, test := range tests {
		s.Send(test.notify)
		err := test.notify.Promise(context.Background())
		switch {
		case test.wantPromiseErr && err == nil:
			t.Errorf("TestSend(%s): got err == nil, want err != nil", test.name)
			continue
		case !test.wantPromiseErr && err != nil:
			t.Errorf("TestSend(%s): got err != %s, want err == nil", test.name, err)
			continue
		}
	}
}
