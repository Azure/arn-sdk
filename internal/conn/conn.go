// Package conn provides the top level package for all connection types to the ARN service.
package conn

import (
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/Azure/arn/internal/conn/http"
	"github.com/Azure/arn/internal/conn/storage"
	"github.com/Azure/arn/models"
)

// PromisePool is a pool of promises to use for notifications.
var PromisePool = sync.Pool{
	New: func() any {
		return make(chan error, 1)
	},
}

// Reset provides a REST connection to the ARN service.
type Service struct {
	endpoint   string
	http       *http.Client
	store      *storage.Client
	clientErrs chan error
	in         chan models.Notifications

	id atomic.Uint64

	log *slog.Logger
}

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
		in: make(chan models.Notifications, 1),

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

// Close closes the connection to the ARN service.
func (r *Service) Close() error {
	close(r.in)
	return nil
}

// Send sends a notification to the ARN service. This will block if the internal channel is full.
// notify.DataCount() must indicate no more than 1000 items. Not thread safe.
func (s *Service) Send(notify models.Notifications) {
	if notify.DataCount() > 1000 {
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
	return
}

// sender sends notifications to the ARN service.
func (s *Service) sender() {
	for n := range s.in {
		if err := n.SendEvent(s.http, s.store); err != nil {
			n.SendPromise(err, s.clientErrs)
			continue
		}
		n.SendPromise(nil, s.clientErrs)
	}
}
