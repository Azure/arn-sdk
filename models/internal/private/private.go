// Private provides private interfaces that are not meant to be used outside of the models package.
package private

import (
	"context"

	"github.com/Azure/arn-sdk/internal/conn/http"
	"github.com/Azure/arn-sdk/internal/conn/storage"
	"github.com/Azure/arn-sdk/models/version"
)

// Notifications is the interface that must be implemented by all notification types across models.
type Notifications interface {
	// Promise blocks until the promise for the notification is resolved or the context is done. If the
	// context is done it must return an error for which errors.Is(err, models.ErrPromiseTimeout) is true;
	// wrapping the context error alongside it is permitted.
	//
	// The implementation owns the promise channel handed to it by SetPromise and must return it to
	// conn.PromisePool, but only on the path that receives a result. Returning it after a context timeout
	// pools a channel the sender still holds, and its late write is then delivered to whichever
	// notification draws that channel next.
	Promise(context.Context) error
	// Attrs provides methods to get the attributes of the notification.
	Attrs
	// Setters provides methods to set private fields.
	Setters
	// Senders provides methods to send information in the notification.
	Senders
}

// Attrs is an interface that must be implemented by all notification types across models.
// It holds methods that return the attributes of the notification.
type Attrs interface {
	// Ctx returns the context for the notification.
	Ctx() context.Context
	// DataCount returns the number of data items in the notification.
	DataCount() int
	// Version returns the schema version of the API.
	Version() version.Schema
	// GetPublisherInfo returns the publisher information for the notification.
	GetPublisherInfo() string
}

// Senders is an interface that must be implemented by all notification types across models.
// It holds methods that send various information in the notification to either the client caller
// or the ARN service.
type Senders interface {
	// SendEvent sends the event to the ARN service. It is also responsible for
	// calling Event.Validate() before sending the event.
	SendEvent(*http.Client, *storage.Client) error
	// SendPromise sends "e" on the promise to the notification. If the promise is nil on the notification,
	// this call will send on the backup channel (the backup channel should be client.Errors()).
	SendPromise(e error, backupCh chan error)
}

// Setters is an interface that must be implemented by all notification types across models.
type Setters interface {
	// SetCtx sets the context for the notification.
	SetCtx(context.Context) Notifications
	// SetPromise sets the promise for the notification. Ownership of the channel passes to the
	// implementation; see Promise for when it must be returned to the pool.
	SetPromise(chan error) Notifications
}

// Event is the interface that is JSON encoded and sent over the wire. Notifications (which are wrappers) are converted to events.
type Event interface {
	// IsEvent is a marker method that indicates this is an event.
	IsEvent()
	// Validate validates the event.
	Validate() error
}
