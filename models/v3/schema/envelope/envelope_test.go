package envelope

import (
	stdjson "encoding/json"
	"encoding/json/v2"
	"fmt"
	"testing"
	"time"

	"github.com/Azure/arn-sdk/models/v3/schema/types"
	"github.com/Azure/arn-sdk/models/version"
)

func TestEventMarshalEmbedding(t *testing.T) {
	t.Parallel()

	const fleetID = "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.ContainerService/fleets/test"
	event := Event{
		EventMeta: EventMeta{ID: "event-id", Subject: "subject"},
		Data: types.Data{
			AdditionalBatchProperties: types.AdditionalBatchProperties{
				Others:    map[string]any{"FleetResourceId": fleetID},
				BatchSize: 1,
			},
		},
	}
	b, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		ID        string `json:"id"`
		Subject   string `json:"subject"`
		EventMeta any    `json:"EventMeta"`
		Data      struct {
			AdditionalBatchProperties struct {
				FleetResourceID string `json:"FleetResourceId"`
				BatchSize       int    `json:"batchSize"`
				Others          any    `json:"Others"`
			} `json:"additionalBatchProperties"`
		} `json:"data"`
	}
	if err := stdjson.Unmarshal(b, &wire); err != nil {
		t.Fatal(err)
	}
	if wire.ID != event.EventMeta.ID || wire.Subject != event.EventMeta.Subject || wire.EventMeta != nil {
		t.Errorf("event metadata is not flattened: %s", b)
	}
	props := wire.Data.AdditionalBatchProperties
	if props.FleetResourceID != fleetID || props.BatchSize != 1 || props.Others != nil {
		t.Errorf("additional batch properties are not flattened: %s", b)
	}
}

func TestEventValidate(t *testing.T) {
	t.Parallel()

	e := EventMeta{
		Topic:           "topic",
		Subject:         "subject",
		EventType:       "eventType",
		EventTime:       time.Now(),
		ID:              "id",
		DataVersion:     version.V3,
		MetadataVersion: "1.0",
	}

	tests := []struct {
		name    string
		e       func() EventMeta
		wantErr bool
	}{
		{
			name: "Error: subject is empty",
			e: func() EventMeta {
				e := copyEventMeta(e)
				e.Subject = ""
				return e
			},
			wantErr: true,
		},
		{
			name: "Error: eventType is empty",
			e: func() EventMeta {
				e := copyEventMeta(e)
				e.EventType = ""
				return e
			},
			wantErr: true,
		},
		{
			name: "Error: eventTime is zero",
			e: func() EventMeta {
				e := copyEventMeta(e)
				e.EventTime = time.Time{}
				return e
			},
			wantErr: true,
		},
		{
			name: "Error: id is empty",
			e: func() EventMeta {
				e := copyEventMeta(e)
				e.ID = ""
				return e
			},
			wantErr: true,
		},
		{
			name: "Error: dataVersion is not 3.0",
			e: func() EventMeta {
				e := copyEventMeta(e)
				e.DataVersion = version.Schema("2.0")
				return e
			},
			wantErr: true,
		},
		{
			name: "Error: metadataVersion is not 1.0",
			e: func() EventMeta {
				e := copyEventMeta(e)
				e.MetadataVersion = "2.0"
				return e
			},
			wantErr: true,
		},
		{
			name: "Valid",
			e: func() EventMeta {
				return e
			},
		},
	}

	for _, test := range tests {
		err := test.e().Validate()
		if errCheck(t, fmt.Sprintf("TestEventValidate(%s)", test.name), err, test.wantErr) {
			continue
		}
	}
}

// errCheck checks if the error is as expected.
// I wanted to try a helper function again for this, but I still hate it. The code
// above is still not as clean as just using switch.
func errCheck(t *testing.T, header string, got error, want bool) bool {
	switch {
	case want && got == nil:
		t.Errorf("%s: got err == nil, want err != nil", header)
		return true
	case !want && got != nil:
		t.Errorf("%s: got err == %s, want err == nil", header, got)
		return true
	case got != nil:
		return true
	}
	return false
}

func copyEventMeta(e EventMeta) EventMeta {
	return e
}
