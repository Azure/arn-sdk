// Package models provides the interfaces and types that various model packages
// will need to implement or use. These are exposed private types that can only
// be implemented by sub-packages of the models package.
package models

import (
	"fmt"

	"github.com/Azure/arn/models/internal/private"
)

var (
	// ErrPromiseTimeout is returned when the context on a Promise() call times out.
	ErrPromiseTimeout = fmt.Errorf("promise timeout")
	// ErrBatchSize is returned when the batch size is too large.
	ErrBatchSize = fmt.Errorf("batch size too large")
)

// Event is the interface that is JSON encoded and sent over the wire. Notifications (which are wrappers) are converted to events.
type Event = private.Event

// Notifications is the interface that must be implemented by all notification types across models.
type Notifications = private.Notifications
