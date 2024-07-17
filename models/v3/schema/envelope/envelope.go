// Package  envelope provides the types and functions for sending events to the ARN service.
// This is the outter wrapper that is roughly related to EventGrid events.
// This isn't directly used by a client.
package envelope

import (
	"errors"
	"fmt"
	"time"

	"github.com/Azure/arn/models/v3/schema/types"
	"github.com/Azure/arn/models/version"
)

// Event is the event being sent to the ARN service.
type Event struct {
	// EventMeta is the metadata of the event. This is inlined during Marshaling.
	EventMeta EventMeta `json:",inline"`
	// Data is the data of the event.
	Data types.Data `json:"data"`
}

// IsEvent implements private.Event.IsEvent().
func (e Event) IsEvent() {}

// Validate validates the event.
func (e Event) Validate() error {
	if err := e.EventMeta.Validate(); err != nil {
		return fmt.Errorf("Event.EventMeta: %w", err)
	}
	if err := e.Data.Validate(); err != nil {
		return fmt.Errorf("Event.Data: %w", err)
	}
	return nil
}

// EventMeta is the metadata of the event. This is part of the event envelope and
// isn't directly used by a client.
type EventMeta struct {
	// Topic is the topic of the event.
	// NOTE: This isn't used in the bulk upload scenario we use in the old SDK, so....
	Topic string `json:"topic"`
	// Subject is the subject of the event.
	// "/subscriptions/{subId}/resourceGroups/{rgName}/resourproviders/{providerNamespace}/{resourceType}/{resourceName}",
	// resource id if notification contains a single id, else, the tenant (/tenants/<tenand_guid>) or subscription (/subscriptions/<subscription_guid>) which groups the resources,
	// could also be "/" if resources span multiple tenants/subscriptions
	// This is set automatically.
	Subject string `json:"subject"`
	// EventType is the type of event.
	// "{providerNamespace}/{resourceType}/{action: write | delete | move\action | start\action … | snapshot}",
	// This is set automatically.
	EventType string `json:"eventType"`
	// EventTime is the time of the event.
	// This is set automatically.
	EventTime time.Time `json:"eventTime" format:"RFC3339"`
	// ID is the GUID of the event. This is set automatically.
	ID string `json:"id"`
	// DataVersion is the schema version. In this case it should always be 3.0 .
	// This is automatically set.
	DataVersion version.Schema `json:"dataVersion"`
	// The Metadata version of this event notification. For the moment, should always be 1.0 .
	// This is automatically set.
	MetadataVersion string `json:"metadataVersion"`
}

// Validate validates the event metadata.
func (e EventMeta) Validate() error {
	if e.Subject == "" {
		return errors.New("EventMeta.Subject is required")
	}
	if e.EventType == "" {
		return errors.New("EventMeta.EventType is required")
	}
	// TODO: Do an extra validation on the EventType.
	if e.EventTime.IsZero() {
		return errors.New("EventMeta.EventTime is required")
	}
	if e.ID == "" {
		return errors.New("EventMeta.ID is required")
	}
	if e.DataVersion != version.V3 {
		return fmt.Errorf("EventMeta.DataVersion must be %s", version.V3)
	}
	if e.MetadataVersion != "1.0" {
		return errors.New("EventMeta.MetadataVersion must be 1")
	}
	return nil
}
