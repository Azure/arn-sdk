package storage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/sas"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/service"
	"github.com/Azure/retry/exponential"
)

// credData is the data stored in the credCache.
type credData struct {
	cred    *service.UserDelegationCredential
	expires time.Time
	err     error
}

// getCred is an interface for getting a user delegation credential. I am violating go naming
// conventions because GetUserDelegationCredential is already violating it, and I want a shorter name.
// This is implemented by *service.Client.
type getCreder interface {
	GetUserDelegationCredential(ctx context.Context, info service.KeyInfo, o *service.GetUserDelegationCredentialOptions) (*service.UserDelegationCredential, error)
}

// credCache is a cache for user delegation credentials. It is non-blocking and updates
// the credential in the background.
type credCache struct {
	now  func() time.Time
	cli  getCreder
	cred atomic.Pointer[credData]

	log     *slog.Logger
	closeCh chan struct{}

	fakeRefreshCred func(ctx context.Context, now time.Time) error
	start           bool
}

type ccOption func(*credCache) error

// withLogger sets the logger on the credCache.
func withLogger(log *slog.Logger) ccOption {
	return func(c *credCache) error {
		c.log = log
		return nil
	}
}

// newCredCache creates a new credCache.
func newCredCache(client getCreder, options ...ccOption) (*credCache, error) {
	cc := &credCache{
		now:     time.Now,
		cli:     client,
		log:     slog.Default(),
		closeCh: make(chan struct{}),
		start:   true,
	}

	for _, o := range options {
		if err := o(cc); err != nil {
			return nil, err
		}
	}

	if cc.start {
		if err := cc.refreshCred(context.Background(), cc.now().UTC()); err != nil {
			return nil, fmt.Errorf("credCache: problem getting credential: %w", err)
		}
		go cc.refresher()
	}

	return cc, nil
}

// close closes the credCache.
func (c *credCache) close() {
	close(c.closeCh)
}

// get gets the user delegation credential from the cache.  If the credential is expired you will receive an
// error. This only occurs if the background goroutine fails to refresh the credential.
// In that case, this will return the last error received.
func (c *credCache) get(ctx context.Context) (*service.UserDelegationCredential, error) {
	cred := c.cred.Load()
	if cred == nil {
		return nil, errors.New("no credential")
	}
	if cred.expires.Before(c.now()) {
		if cred.err != nil {
			return nil, cred.err
		}
		return nil, errors.New("credential expired")
	}

	return cred.cred, nil
}

// refresher is a background goroutine that refreshes the user delegation credential.
func (c *credCache) refresher() {
	const (
		nextRefresh = 23 * time.Hour
	)

	boff, err := exponential.New()
	if err != nil {
		// We aren't passing any options, this should never happen and
		// we should panic if it does.
		panic(err)
	}
	ctx := context.Background()

	for {
		next := time.Now().Add(nextRefresh)
		// This will block until the next refresh time.
		// An error will only be returned if the cache is closed, so it can be ignored.
		if err := c.refresh(ctx, boff, next); err != nil {
			return
		}
	}
}

// refresh refreshes the user delegation credential after a next time.
// It will retry forever with a backoff policy or until close() is called.
// Only returns an error if closed is called, which can be ignored.
func (c *credCache) refresh(ctx context.Context, boff *exponential.Backoff, next time.Time) error {
	select {
	case <-c.closeCh:
		return errors.New("closed")
	case <-time.After(next.Sub(c.now())):
		// This will retry forever. On any failures it will log the error and continue.
		// Every retry can only take up to 30 seconds. Uses the default policy which has a
		// maximum time of 1min - 1min30s between attempts.
		err := boff.Retry(ctx, func(ctx context.Context, r exponential.Record) error {
			select {
			case <-c.closeCh:
				return fmt.Errorf("credCache closed: %w", exponential.ErrPermanent)
			default:
			}
			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			if err := c.refreshCred(ctx, c.now().UTC()); err != nil {
				c.log.Error(fmt.Sprintf("credCache: problem refreshing credential: %s", err.Error()))
				return err
			}
			return nil
		})
		return err
	}
}

// refresh refreshes the user delegation credential.
func (c *credCache) refreshCred(ctx context.Context, now time.Time) error {
	if c.fakeRefreshCred != nil {
		return c.fakeRefreshCred(ctx, now)
	}
	start := now.Truncate(time.Second)
	expiry := start.Add(7 * 24 * time.Hour)

	cred, err := c.cli.GetUserDelegationCredential(
		ctx,
		service.KeyInfo{
			Expiry: toPtr(expiry.UTC().Format(sas.TimeFormat)),
			Start:  toPtr(start.UTC().Format(sas.TimeFormat)),
		},
		nil,
	)
	if err != nil {
		current := c.cred.Load()
		if current == nil {
			current = &credData{}
		}
		if current.expires.Before(c.now()) {
			cd := &credData{
				expires: current.expires,
				err:     err,
			}
			c.cred.Store(cd)
		}
		return err
	}
	cd := &credData{
		cred:    cred,
		expires: expiry,
	}

	c.cred.Store(cd)
	return nil
}
