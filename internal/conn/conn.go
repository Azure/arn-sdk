// Package conn provides the top level package for all connection types to the ARN service.
package conn

import (
	"fmt"
	"log/slog"

	"github.com/Azure/arn-sdk/internal/conn/http"
	"github.com/Azure/arn-sdk/internal/conn/maxvals"
	"github.com/Azure/arn-sdk/internal/conn/storage"
	"github.com/Azure/arn-sdk/models"
	"github.com/gostdlib/base/concurrency/sync"
	"github.com/gostdlib/base/context"
)

// PromisePool is a pool of promises to use for notifications.
var PromisePool = sync.NewPool(
	context.Background(),
	"promisePool",
	func() chan error {
		return make(chan error, 1)
	},
	sync.WithBuffer(10),
)

// Reset provides a REST connection to the ARN service.
type Service struct {
	endpoint   string
	http       *http.Client
	store      *storage.Client
	clientErrs chan error
	in         chan models.Notifications

	// sigSenderClosed is closed by sender() once it has drained in. Close() waits on it so that
	// everything Send() accepted is delivered before Close() returns.
	sigSenderClosed chan struct{}

	log *slog.Logger
}

// Option is an option for the New constructor.
type Option func(*Service) error

// WithLogger sets the logger on the client. By default it uses slog.Default().
func WithLogger(log *slog.Logger) Option {
	return func(c *Service) error {
		c.log = log
		return nil
	}
}

// New creates a new connection to the ARN service.
func New(httpClient *http.Client, store *storage.Client, clientErrs chan error, options ...Option) (*Service, error) {
	if httpClient == nil {
		return nil, fmt.Errorf("httpClient is required")
	}
	if store == nil {
		return nil, fmt.Errorf("store is required")
	}
	if clientErrs == nil {
		return nil, fmt.Errorf("clientErrs is required")
	}

	conn := &Service{
		in:              make(chan models.Notifications, 1),
		sigSenderClosed: make(chan struct{}),

		http:       httpClient,
		store:      store,
		clientErrs: clientErrs,
	}

	for _, o := range options {
		if err := o(conn); err != nil {
			return nil, err
		}
	}

	go conn.sender()

	return conn, nil
}

// Close closes the connection to the ARN service. It blocks until every notification that Send()
// accepted has been sent and its promise resolved. Returning before that drained the queue into a
// goroutine the caller had no way to wait on, so a process that exited after Close() silently
// dropped the notifications still in flight.
func (r *Service) Close() error {
	close(r.in)
	<-r.sigSenderClosed
	return nil
}

// Send sends a notification to the ARN service. This will block if the internal channel is full.
// notify.DataCount() must indicate no more than maxvals.NotificationItems items. Not thread safe.
func (s *Service) Send(notify models.Notifications) {
	if notify.DataCount() > maxvals.NotificationItems {
		notify.SendPromise(models.ErrBatchSize, s.clientErrs)
		return
	}

	// Makes this predictable for testing, as select is non-deterministic.
	if notify.Ctx().Err() != nil {
		notify.SendPromise(notify.Ctx().Err(), s.clientErrs)
		return
	}

	select {
	case <-notify.Ctx().Done():
		notify.SendPromise(notify.Ctx().Err(), s.clientErrs)
	case s.in <- notify:
	}
}

// sender sends notifications to the ARN service.
func (s *Service) sender() {
	defer close(s.sigSenderClosed)

	for n := range s.in {
		if err := n.SendEvent(s.http, s.store); err != nil {
			n.SendPromise(err, s.clientErrs)
			continue
		}
		n.SendPromise(nil, s.clientErrs)
	}
}
