package client

import (
	"context"
	"errors"
	"testing"

	"github.com/Azure/arn-sdk/internal/conn"
	"github.com/Azure/arn-sdk/internal/conn/http"
	"github.com/Azure/arn-sdk/internal/conn/maxvals"
	"github.com/Azure/arn-sdk/internal/conn/storage"
	"github.com/Azure/arn-sdk/models"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
)

func TestHTTPArgsValidate(t *testing.T) {
	t.Parallel()

	valid := HTTPArgs{
		Endpoint: "http://localhost:8080",
		Cred:     struct{ azcore.TokenCredential }{},
	}

	tests := []struct {
		name    string
		args    func() HTTPArgs
		wantErr bool
	}{
		{
			name: "Error: endpoint is empty",
			args: func() HTTPArgs {
				args := copyStruct(valid)
				args.Endpoint = ""
				return args
			},
			wantErr: true,
		},
		{
			name: "Error: cred is nil",
			args: func() HTTPArgs {
				args := copyStruct(valid)
				args.Cred = nil
				return args
			},
			wantErr: true,
		},
		{
			name: "valid",
			args: func() HTTPArgs {
				return valid
			},
		},
	}

	for _, test := range tests {
		err := test.args().validate()
		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestHTTPArgsValidate(%s): got nil, want error", test.name)
			continue
		case err != nil && !test.wantErr:
			t.Errorf("TestHTTPArgsValidate(%s): got %s, want nil", test.name, err)
			continue
		case err != nil:
			continue
		}
	}
}

func TestBlobArgs(t *testing.T) {
	t.Parallel()

	valid := BlobArgs{
		Endpoint: "http://localhost:8080",
		Cred:     struct{ azcore.TokenCredential }{},
	}

	tests := []struct {
		name    string
		args    func() BlobArgs
		wantErr bool
	}{
		{
			name: "Error: endpoint is empty",
			args: func() BlobArgs {
				args := copyStruct(valid)
				args.Endpoint = ""
				return args
			},
			wantErr: true,
		},
		{
			name: "Error: cred is nil",
			args: func() BlobArgs {
				args := copyStruct(valid)
				args.Cred = nil
				return args
			},
			wantErr: true,
		},
		{
			name: "Valid",
			args: func() BlobArgs {
				return valid
			},
		},
	}

	for _, test := range tests {
		err := test.args().validate()
		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestBlobArgs(%s): got nil, want error", test.name)
			continue
		case err != nil && !test.wantErr:
			t.Errorf("TestBlobArgs(%s): got %s, want nil", test.name, err)
			continue
		case err != nil:
			continue
		}
	}
}

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
		count:    count,
		eventErr: eventErr,
	}
}

func (f fakeNotify) Recycle() {
	conn.PromisePool.Put(f.ch)
}

func (f fakeNotify) SetCtx(ctx context.Context) models.Notifications {
	f.ctx = ctx
	return f
}

func (f fakeNotify) Ctx() context.Context {
	return f.ctx
}

func (f fakeNotify) SetPromise(ch chan error) models.Notifications {
	f.ch = ch
	return f
}

func (f fakeNotify) SendPromise(e error, backupCh chan error) {
	if f.ch == nil {
		if backupCh == nil {
			return
		}
		select {
		case backupCh <- e:
		default:
		}
		return
	}

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

func TestNotify(t *testing.T) {
	t.Parallel()

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name     string
		ctx      context.Context
		n        models.Notifications
		connSend func(n models.Notifications)
		wantErr  bool
	}{
		{
			name: "Datacount is zero",
			ctx:  context.Background(),
			n:    newFakeNotify(nil, 0, false),
		},
		{
			name:    "Error: Datacount is > maxvals.NotificationItems",
			ctx:     context.Background(),
			n:       newFakeNotify(nil, maxvals.NotificationItems+1, false),
			wantErr: true,
		},
		{
			name:    "Error: context is cancelled",
			ctx:     cancelCtx,
			n:       newFakeNotify(nil, 1, false),
			wantErr: true,
		},
		{
			name: "Success",
			ctx:  context.Background(),
			n:    newFakeNotify(nil, 1, false),
			connSend: func(n models.Notifications) {
				n.SendPromise(nil, nil)
			},
		},
	}

	for _, test := range tests {
		a := &ARN{
			testConn:        test.connSend,
			in:              make(chan models.Notifications, 1),
			sigSenderClosed: make(chan struct{}),
		}
		go a.sender()
		defer a.Close()

		err := a.Notify(test.ctx, test.n)
		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestNotify(%s): got nil, want error", test.name)
			continue
		case err != nil && !test.wantErr:
			t.Errorf("TestNotify(%s): got %s, want nil", test.name, err)
			continue
		case err != nil:
			continue
		}
	}
}

func TestAsync(t *testing.T) {
	t.Parallel()

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()

	backup := make(chan error, 1)

	tests := []struct {
		name     string
		ctx      context.Context
		n        models.Notifications
		promise  bool
		connSend func(n models.Notifications)
		wantErr  bool
	}{
		{
			name:    "Datacount is zero, promise",
			ctx:     context.Background(),
			n:       newFakeNotify(nil, 0, false),
			promise: true,
		},
		{
			name: "Datacount is zero, no promise",
			ctx:  context.Background(),
			n:    newFakeNotify(nil, 0, false),
		},

		{
			name:    "Error: Datacount is > maxvals.NotificationItems, promise",
			ctx:     context.Background(),
			n:       newFakeNotify(nil, maxvals.NotificationItems+1, false),
			promise: true,
			wantErr: true,
		},
		{
			name:    "Error: Datacount is > maxvals.NotificationItems, no promise",
			ctx:     context.Background(),
			n:       newFakeNotify(nil, maxvals.NotificationItems+1, false),
			wantErr: true,
		},
		{
			name:    "Error: context is cancelled, promise",
			ctx:     cancelCtx,
			n:       newFakeNotify(nil, 1, false),
			promise: true,
			wantErr: true,
		},
		{
			name:    "Error: context is cancelled, no promise",
			ctx:     cancelCtx,
			n:       newFakeNotify(nil, 1, false),
			wantErr: true,
		},
		{
			name:    "Success promise",
			ctx:     context.Background(),
			n:       newFakeNotify(nil, 1, false),
			promise: true,
			connSend: func(n models.Notifications) {
				n.SendPromise(nil, nil)
			},
		},
		{
			name: "Success no promise",
			ctx:  context.Background(),
			n:    newFakeNotify(nil, 1, false),
			connSend: func(n models.Notifications) {
				n.SendPromise(nil, backup)
			},
		},
	}

	for _, test := range tests {
		a := &ARN{
			testConn:        test.connSend,
			in:              make(chan models.Notifications, 1),
			errs:            backup,
			sigSenderClosed: make(chan struct{}),
		}
		go a.sender()
		defer a.Close()

		n := a.Async(test.ctx, test.n, test.promise)
		var err error
		if test.promise {
			err = n.Promise(context.Background())
		} else {
			err = <-backup
		}
		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestNotify(%s): got nil, want error", test.name)
			continue
		case err != nil && !test.wantErr:
			t.Errorf("TestNotify(%s): got %s, want nil", test.name, err)
			continue
		case err != nil:
			continue
		}
	}
}

func copyStruct[T any](a T) T {
	return a
}
