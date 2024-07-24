package storage

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/service"
	"github.com/Azure/retry/exponential"
	"github.com/kylelemons/godebug/pretty"
)

func TestGet(t *testing.T) {
	t.Parallel()

	expired := time.Now().Add(-1 * time.Hour)

	tests := []struct {
		name    string
		cred    *credData
		wantErr bool
	}{
		{
			name:    "Error: credData == nil",
			wantErr: true,
		},
		{
			name:    "Error: credData has expired, but credData.err == nil",
			cred:    &credData{expires: expired},
			wantErr: true,
		},
		{
			name: "Error: credData has expired, credData.err != nil",
			cred: &credData{
				cred:    &service.UserDelegationCredential{},
				expires: expired,
				err:     errors.New("error"),
			},
			wantErr: true,
		},
		{
			name: "Success: credData has not expired",
			cred: &credData{
				cred:    &service.UserDelegationCredential{},
				expires: time.Now().Add(1 * time.Hour),
			},
		},
	}

	for _, test := range tests {
		cc := &credCache{now: time.Now, log: slog.Default()}
		cc.cred.Store(test.cred)

		_, err := cc.get(context.Background())
		switch {
		case test.wantErr && err == nil:
			t.Errorf("TestGet(%s): got err == nil, want err != nil", test.name)
			continue
		case !test.wantErr && err != nil:
			t.Errorf("TestGet(%s): got err == %s, want err == nil", test.name, err)
			continue
		}
	}
}

func TestRefresh(t *testing.T) {
	t.Parallel()

	closed := make(chan struct{})
	close(closed)

	attempts := 0

	tests := []struct {
		name            string
		fakeRefreshCred func(ctx context.Context, now time.Time) error
		closeDuring     bool
		closeCh         chan struct{}
		wantErr         bool
	}{
		{
			name:    "Error: closeCh closed",
			closeCh: closed,
			wantErr: true,
		},
		{
			name: "Error: fakeRefreshCred returns error and close is called during retries",
			fakeRefreshCred: func(ctx context.Context, now time.Time) error {
				return errors.New("error")
			},
			closeCh:     make(chan struct{}),
			closeDuring: true,
			wantErr:     true,
		},
		{
			name: "Success: fakeRefreshCred eventually succeeds",
			fakeRefreshCred: func(ctx context.Context, now time.Time) error {
				attempts++
				if attempts < 3 {
					return errors.New("error")
				}
				return nil
			},
		},
		{
			name: "Success: fakeRefreshCred returns no error",
			fakeRefreshCred: func(ctx context.Context, now time.Time) error {
				return nil
			},
			closeCh: make(chan struct{}),
		},
	}

	for _, test := range tests {
		attempts = 0
		cc := &credCache{
			fakeRefreshCred: test.fakeRefreshCred,
			now:             time.Now,
			log:             slog.Default(),
		}
		cc.closeCh = test.closeCh
		boff, err := exponential.New()
		if err != nil {
			panic(err)
		}

		if test.closeDuring {
			go func() {
				time.Sleep(100 * time.Millisecond)
				close(test.closeCh)
			}()
		}

		err = cc.refresh(context.Background(), boff, time.Now())
		switch {
		case test.wantErr && err == nil:
			t.Errorf("TestRefresh(%s): got err == nil, want err != nil", test.name)
			continue
		case !test.wantErr && err != nil:
			t.Errorf("TestRefresh(%s): got err == %s, want err == nil", test.name, err)
			continue
		case err != nil:
			continue
		}
	}
}

func TestRefreshCred(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	expired := now.Add(-1 * time.Hour)
	notExpired := now.Add(1 * time.Hour)

	tests := []struct {
		name       string
		fakeCreder fakeCreder
		wantErr    bool
		current    *credData
		want       *credData
	}{
		{
			name:       "Error: nil current && GetUserDelegationCredential returns error",
			fakeCreder: fakeCreder{err: errors.New("error")},
			wantErr:    true,
			want:       &credData{err: errors.New("error")},
		},
		{
			name:       "Error: current expired && GetUserDelegationCredential returns error",
			fakeCreder: fakeCreder{err: errors.New("error")},
			current:    &credData{expires: expired},
			wantErr:    true,
			want:       &credData{expires: expired, err: errors.New("error")},
		},
		{
			name:       "Error: current not expired && GetUserDelegationCredential returns error",
			fakeCreder: fakeCreder{err: errors.New("error")},
			current:    &credData{expires: notExpired},
			wantErr:    true,
			want:       &credData{expires: notExpired},
		},
		{
			name:       "Success: nil current && GetUserDelegationCredential returns no error",
			fakeCreder: fakeCreder{},
			want:       &credData{cred: &service.UserDelegationCredential{}, expires: now.Truncate(time.Second).Add(7 * 24 * time.Hour)},
		},
		{
			name:       "Success: current expired && GetUserDelegationCredential returns no error",
			fakeCreder: fakeCreder{},
			current:    &credData{expires: expired},
			want:       &credData{cred: &service.UserDelegationCredential{}, expires: now.Truncate(time.Second).Add(7 * 24 * time.Hour)},
		},
	}

	for _, test := range tests {
		cc := &credCache{
			cli: &test.fakeCreder,
			now: time.Now,
			log: slog.Default(),
		}
		cc.cred.Store(test.current)

		err := cc.refreshCred(context.Background(), now)
		switch {
		case test.wantErr && err == nil:
			t.Errorf("TestRefreshCred(%s): got err == nil, want err != nil", test.name)
			continue
		case !test.wantErr && err != nil:
			t.Errorf("TestRefreshCred(%s): got err == %s, want err == nil", test.name, err)
			continue
		}
		if diff := pretty.Compare(test.want, cc.cred.Load()); diff != "" {
			t.Errorf("TestRefreshCred(%s): -want/+got:\n%s", test.name, diff)
		}
	}
}
