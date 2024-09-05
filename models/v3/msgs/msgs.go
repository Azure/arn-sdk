package msgs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/Azure/arn-sdk/internal/conn"
	"github.com/Azure/arn-sdk/internal/conn/http"
	"github.com/Azure/arn-sdk/internal/conn/maxvals"
	"github.com/Azure/arn-sdk/internal/conn/storage"
	"github.com/Azure/arn-sdk/models"
	"github.com/Azure/arn-sdk/models/metrics"
	"github.com/Azure/arn-sdk/models/v3/schema/envelope"
	"github.com/Azure/arn-sdk/models/v3/schema/types"
	"github.com/Azure/arn-sdk/models/version"

	"github.com/go-json-experiment/json"
	"github.com/google/uuid"
)

// Compile time check to ensure Notifications implements models.Notifications.
var _ models.Notifications = Notifications{}

// Notifications is a notification to send to the ARN service. This is a wrapper around the actual data
// that is sent in the notification. The data will be converted to an Event and sent over the wire.
type Notifications struct {
	// ctx is the context for the notification. This honors the context deadline.
	ctx context.Context
	// Promise is a channel that will be used to send the result of the notification.
	// If this is nil, no promise will be sent unless callling Notify(). In that case
	// a promise will be created automatically. A promise is only good until you receive
	// the result from it. After that, the promise can be reused in another Notification.
	// This is not required to be set if you are using Notify().
	promise chan error

	// ResourceLocation is the location of the resources in this notification. This is the normalized ARM location enum
	// like "eastus".
	ResourceLocation string
	// PublisherInfo is the Namespace of the publisher sending the data of this notification, for example Microsoft.Resources is be the publisherInfo for ARM.
	PublisherInfo string

	// Data is the data to send in the notification.
	Data []types.NotificationResource

	testSendHTTP func(*http.Client, envelope.Event) error
	testSendBlob func(*storage.Client, []byte) (*url.URL, error)
}

// Promise waits for the promise to be fulfilled. This will return an ErrPromiseTimeout if the context
// passed times out to distiguish it from a context timeout on sending the notification.
func (n Notifications) Promise(ctx context.Context) error {
	if n.promise == nil {
		return nil
	}
	defer func() {
		conn.PromisePool.Put(n.promise)
	}()

	if ctx.Err() != nil {
		metrics.Promise(context.Background(), ctx.Err())
		return ctx.Err()
	}

	select {
	case <-ctx.Done():
		metrics.Promise(context.Background(), models.ErrPromiseTimeout)
		return models.ErrPromiseTimeout
	case e := <-n.promise:
		metrics.Promise(context.Background(), e)
		return e
	}
}

// Recycle can be used to recycle the promise of a notification once it has been used.
// It is a terrible idea to use the promise after it has been recycled.
func (n Notifications) Recycle() {
	if n.promise != nil {
		select {
		case <-n.promise:
		default:
		}
		conn.PromisePool.Put(n.promise)
	}
}

func (n Notifications) Ctx() context.Context {
	return n.ctx
}

// DataCount implements models.Notifications.DataCount().
func (n Notifications) DataCount() int {
	return len(n.Data)
}

// DataJSON implements models.Notifications.Version().
func (n Notifications) Version() version.Schema {
	return version.V3
}

func (n Notifications) SetCtx(ctx context.Context) models.Notifications {
	n.ctx = ctx
	return n
}

// SetPromise sets the promise channel used for the notification.
func (n Notifications) SetPromise(promise chan error) models.Notifications {
	n.promise = promise
	return n
}

// SendPromise sends an error on the promise to the notification.
func (n Notifications) SendPromise(e error, backupCh chan error) {
	if n.promise == nil {
		if e == nil {
			return
		}
		if backupCh != nil {
			select {
			case backupCh <- e:
			default:
			}
		}
		return
	}
	select {
	case n.promise <- e:
	default:
		slog.Default().Error("Bug: had a Notification promise, but it blocked")
	}
}

// dataToJSON returns the JSON representation of the data in the notification.
// Once this is called, the data is cached. So new data added to the Notification will not be included in the JSON.
func (n Notifications) dataToJSON() ([]byte, error) {
	b, err := json.Marshal(n.Data)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// SendEvent converts the notification to an event and sends it to the ARN service.
// Do not call this function directly, use methods on the Client instead.
func (n Notifications) SendEvent(hc *http.Client, store *storage.Client) (err error) {
	started := time.Now()
	// keep track so we can record whether the data was inlined or not (receiver or blob)
	inline := false
	var dataSize int64
	defer func() {
		elapsed := time.Since(started)
		if err != nil {
			metrics.SendEventFailure(context.Background(), elapsed, inline, dataSize)
			return
		}
		metrics.SendEventSuccess(context.Background(), elapsed, inline, dataSize)
	}()

	if len(n.Data) == 0 {
		return errors.New("no data to send")
	}

	// Convert the notification to an event.
	dataJSON, event, err := n.toEvent()
	if err != nil {
		return err
	}

	// As a producer, we have to set the status code for all Resources to OK.
	for i, e := range event.Data.Resources {
		e.StatusCode = types.StatusCode
		event.Data.Resources[i] = e
	}
	if err = event.Validate(); err != nil {
		return err
	}

	dataSize = int64(len(event.Data.Data))

	// If the data is marked inline, we can send over HTTP directly.
	if event.Data.ResourcesContainer == types.RCInline {
		inline = true
		return n.sendHTTP(hc, event)
	}

	u, err := n.sendBlob(store, dataJSON)
	if err != nil {
		return err
	}

	// Tell the service (via HTTP) where to find the blob.
	event.Data.ResourcesBlobInfo.BlobURI = u.String()
	event.Data.ResourcesBlobInfo.BlobSize = int64(len(dataJSON))
	return n.sendHTTP(hc, event)
}

// toEvent converts the notification to an event. If the data is inline, the data will be included in the event.
// Otherwise you will need to set Event.Data.ResourceBlobInfo.BlobURI to the URI of the blob.
func (n Notifications) toEvent() ([]byte, envelope.Event, error) {
	dataJSON, inline, err := n.inline()
	if err != nil {
		return dataJSON, envelope.Event{}, err
	}

	meta, err := newEventMeta(n.Data)
	if err != nil {
		return dataJSON, envelope.Event{}, fmt.Errorf("problem creating an EventMeta: %w", err)
	}

	if inline {
		return dataJSON, envelope.Event{
			EventMeta: meta,
			Data: types.Data{
				Data:               dataJSON,
				ResourcesContainer: types.RCInline,
				ResourceLocation:   n.ResourceLocation,
				PublisherInfo:      n.PublisherInfo,
				Resources:          n.Data,
			},
		}, nil
	}

	return dataJSON, envelope.Event{
		EventMeta: meta,
		Data: types.Data{
			ResourcesContainer: types.RCBlob,
			ResourceLocation:   n.ResourceLocation,
			PublisherInfo:      n.PublisherInfo,
			Resources:          n.Data,
		},
	}, nil
}

func (n Notifications) sendHTTP(hc *http.Client, event envelope.Event) error {
	if n.testSendHTTP != nil {
		return n.testSendHTTP(hc, event)
	}

	b, err := json.Marshal(event)
	if err != nil {
		return err
	}

	return hc.Send(n.ctx, b)
}

func (n Notifications) sendBlob(store *storage.Client, dataJSON []byte) (*url.URL, error) {
	if n.testSendBlob != nil {
		return n.testSendBlob(store, dataJSON)
	}

	// If store isn't set then this message is too large to send.
	if store == nil {
		return nil, fmt.Errorf("event exceeds max inline size and no storage client provided to store the data in a blob")
	}

	return store.Upload(n.ctx, uuid.New().String(), dataJSON)
}

// inline determines if the notification should be inlined. It returns the JSON representation of the data
// so that we don't have to marshal it again, if the data should be inlined and an error if there was a problem.
func (n Notifications) inline() ([]byte, bool, error) {
	b, err := n.dataToJSON()
	if err != nil {
		return nil, false, err
	}

	if len(b) < maxvals.InlineSize {
		return b, true, nil
	}
	return b, false, nil
}

var nower = time.Now

// newEventMeta creates a new EventMeta. This is not intended to be used by
// a caller, so this constructor is here instead of in the types package.
func newEventMeta(data []types.NotificationResource) (envelope.EventMeta, error) {
	if len(data) == 0 {
		return envelope.EventMeta{}, errors.New("data must not be empty")
	}
	return envelope.EventMeta{
		ID:              uuid.New().String(),
		Subject:         subject(data),
		DataVersion:     version.V3,
		MetadataVersion: "1.0",
		EventTime:       nower().UTC(),
		EventType:       fmt.Sprintf("%s/%s", data[0].ArmResource.Type, data[0].ArmResource.Activity().String()),
	}, nil
}
