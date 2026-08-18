// Package msgs provides the Notifications type which is used to send notifications to the ARN service.
package msgs

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
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
	"github.com/gostdlib/base/concurrency/sync"
	"github.com/gostdlib/base/context"

	"github.com/go-json-experiment/json"
	"github.com/google/uuid"
)

// Compile time check to ensure Notifications implements models.Notifications.
var _ models.Notifications = Notifications{}

// Notifications is a notification to send to the ARN service. This is a wrapper around the actual data
// that is sent in the notification described in types.Data. The data will be converted to an Event and
// sent over the wire.
//
// Ownership: passing a Notifications to Client.Async() transfers ownership of everything it references to the SDK,
// which reads it on an internal goroutine after Async() returns. That covers Data and everything reachable from it
// (ArmResource.Properties, AdditionalResourceProperties, and the *arm.ResourceID given to NewArmResource -- the SDK
// calls String() on it, which memoizes into the value you still hold) as well as AdditionalBatchProperties.Others.
//
// Do not mutate or reuse any of it afterwards. With promise == true you may reclaim it once Promise() returns a
// result -- that is, any return that does not wrap ErrPromiseTimeout. A timeout means the notification is still in
// flight and ownership stays with the SDK; wait again with a fresh context rather than reclaiming. With
// promise == false there is no completion signal at all, so the transfer is permanent. Notify() is synchronous
// and unaffected.
type Notifications struct {
	// AdditionalBatchProperties can contain the sdkversion, batchsize, subscription partition tag etc.
	AdditionalBatchProperties types.AdditionalBatchProperties

	// ctx is the context for the notification. This honors the context deadline.
	ctx context.Context
	// Promise is a channel that will be used to send the result of the notification.
	// If this is nil, no promise will be sent unless callling Notify(). In that case
	// a promise will be created automatically. A promise is only good until you receive
	// the result from it. After that, the promise can be reused in another Notification.
	// This is not required to be set if you are using Notify().
	promise chan error

	testSendHTTP func(*http.Client, envelope.Event) error
	testSendBlob func(*storage.Client, []byte) (*url.URL, error)

	// ResourceLocation is the location of the resources in this notification. This is the normalized ARM location enum
	// like "eastus".
	ResourceLocation string
	// FrontdoorLocation is the ARM region that emitted the notification. Omitted for notifications not emitted by ARM.
	FrontdoorLocation string
	// PublisherInfo is the Namespace of the publisher sending the data of this notification, for example Microsoft.Resources is be the publisherInfo for ARM.
	PublisherInfo string

	// HomeTenantID is the tenant from which the resources in this notification are managed. This should be set by
	// the caller for provider-scoped resources per ARN V3 spec. If set, every NotificationResource in Data must set
	// the same value; if left empty, every NotificationResource must leave it empty. The levels must be identical.
	HomeTenantID string
	// ResourceHomeTenantID is the tenant in which the resources in this notification exist. This should be set by
	// the caller for provider-scoped resources per ARN V3 spec. If set, every NotificationResource in Data must set
	// the same value; if left empty, every NotificationResource must leave it empty. The levels must be identical.
	ResourceHomeTenantID string

	// APIVersion is the API version of the resource schema, in the format "yyyy-MM-dd" followed
	// by an optional string like "-preview" or "-privatepreview". Optional. If left empty, every
	// NotificationResource in Data must set its own APIVersion. If set here, each
	// NotificationResource must either match it or leave its own APIVersion empty.
	APIVersion string
	// DataBoundary is the data boundary for the resources in this notification. Optional;
	// leave as types.DBUnknown to omit it from the wire format.
	DataBoundary types.DataBoundary

	// Data is the data to send in the notification. See the ownership note on Notifications.
	Data []types.NotificationResource
}

// Promise waits for the promise to be fulfilled. It returns an error wrapping ErrPromiseTimeout if the
// context passed times out, to distinguish that from a context timeout on sending the notification.
//
// Call Promise at most once per resolved result. It may be called repeatedly while it keeps returning
// ErrPromiseTimeout -- the notification is still in flight and the promise is still valid. Once it
// returns anything else the promise is spent: calling again, or calling concurrently on a copy of the
// value, consumes a result belonging to a different notification.
//
// The promise channel is returned to the pool here, on the branch that actually receives the result,
// because that is the only point at which the sender is known to be finished with it. It is
// deliberately not recycled on the timeout branches: the notification is still in flight there, and
// pooling the channel would let the sender's late write land in whichever notification drew it next.
// Those channels are left to the garbage collector instead, which costs a pool entry and nothing else.
func (n Notifications) Promise(ctx context.Context) error {
	if n.promise == nil {
		return nil
	}

	if ctx.Err() != nil {
		return n.timedOut(ctx)
	}

	select {
	case <-ctx.Done():
		return n.timedOut(ctx)
	case e := <-n.promise:
		return n.resolved(ctx, e)
	}
}

// resolved records the result and returns the promise channel to the pool. This is the only point at
// which the sender is known to be finished with the channel.
func (n Notifications) resolved(ctx context.Context, e error) error {
	conn.PromisePool.Put(ctx, n.promise)
	metrics.Promise(context.Background(), e)
	return e
}

// timedOut is the give-up path. It drains first because select chooses randomly when both the context
// and the promise are ready, so a deadline expiring at the instant the result lands would otherwise
// report a timeout for a notification that actually shipped. The channel is deliberately not pooled
// here: the notification is still in flight, and the promise is still valid to wait on again.
func (n Notifications) timedOut(ctx context.Context) error {
	select {
	case e := <-n.promise:
		return n.resolved(ctx, e)
	default:
	}
	// Only the completed counter is recorded. The gauge is decremented by resolved(), because the
	// promise is not finished -- the caller may wait on it again with a fresh context.
	metrics.PromiseTimeout(context.Background())
	return fmt.Errorf("%w: %w", models.ErrPromiseTimeout, ctx.Err())
}

// Ctx returns the Context for this Notifications instance.
func (n Notifications) Ctx() context.Context {
	if n.ctx == nil {
		return context.Background()
	}
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

// GetPublisherInfo implements models.Notifications.GetPublisherInfo().
func (n Notifications) GetPublisherInfo() string {
	return n.PublisherInfo
}

// SetCtx implements models.Notifications.SetCtx().
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

func EventType(res types.NotificationResource) string {
	return fmt.Sprintf("%s/%s", res.ArmResource.Type, res.ArmResource.Activity().String())
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

	if err = event.Validate(); err != nil {
		return err
	}

	// Measured from dataJSON, not event.Data.Data: toEvent() only populates Data.Data on the inline
	// branch, so reading it here reported 0 bytes for every blob send.
	dataSize = int64(len(dataJSON))

	// If the data is marked inline, we can send over HTTP directly.
	if event.Data.ResourcesContainer == types.RCInline {
		inline = true
		return n.sendHTTP(hc, event)
	}

	u, err := n.sendBlob(store, dataJSON)
	if err != nil {
		return err
	}
	// A fake Uploader supplied through client.WithFakeClients may return (nil, nil); without this
	// the deref below panics on the sender goroutine, which has no recover.
	if u == nil {
		return errors.New("blob upload returned no URL")
	}

	// Tell the service (via HTTP) where to find the blob.
	event.Data.ResourcesBlobInfo.BlobURI = u.String()
	event.Data.ResourcesBlobInfo.BlobSize = int64(len(dataJSON))
	// Data.Validate() cannot check this, as it runs before the upload gives us the URI and size.
	if err = event.Data.ResourcesBlobInfo.Validate(); err != nil {
		return err
	}
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

	if len(n.Data) > math.MaxUint16 {
		return dataJSON, envelope.Event{}, fmt.Errorf("too many resources to send in a single event: %d", len(n.Data))
	}

	n.AdditionalBatchProperties.BatchSize = uint16(len(n.Data))
	n.AdditionalBatchProperties.SDKVersion = version.SDK.AsARNFormat()

	if inline {
		return dataJSON, envelope.Event{
			EventMeta: meta,
			Data: types.Data{
				Data:                      dataJSON, // This serializes into the "Resources" field.
				FrontdoorLocation:         n.FrontdoorLocation,
				AdditionalBatchProperties: n.AdditionalBatchProperties,
				ResourcesContainer:        types.RCInline,
				ResourceLocation:          n.ResourceLocation,
				PublisherInfo:             n.PublisherInfo,
				Resources:                 n.Data, // See the ownership note on Notifications.Data.
				HomeTenantID:              n.HomeTenantID,
				ResourceHomeTenantID:      n.ResourceHomeTenantID,
				APIVersion:                n.APIVersion,
				DataBoundary:              n.DataBoundary,
			},
		}, nil
	}

	return dataJSON, envelope.Event{
		EventMeta: meta,
		Data: types.Data{
			FrontdoorLocation:         n.FrontdoorLocation,
			AdditionalBatchProperties: n.AdditionalBatchProperties,
			ResourcesContainer:        types.RCBlob,
			ResourceLocation:          n.ResourceLocation,
			PublisherInfo:             n.PublisherInfo,
			Resources:                 n.Data, // See the ownership note on Notifications.Data.
			HomeTenantID:              n.HomeTenantID,
			ResourceHomeTenantID:      n.ResourceHomeTenantID,
			APIVersion:                n.APIVersion,
			DataBoundary:              n.DataBoundary,
		},
	}, nil
}

var headerPool = sync.NewPool(
	context.Background(),
	"headerPool",
	func() []string {
		return make([]string, 2) // Two headers: "publisherinfo" and the actual publisher info.
	},
	sync.WithBuffer(100), // Preallocate a pool of 100 header slices to avoid allocations during sendHTTP.
)

func (n Notifications) sendHTTP(hc *http.Client, event envelope.Event) error {
	if n.testSendHTTP != nil {
		return n.testSendHTTP(hc, event)
	}

	b, err := json.Marshal(event)
	if err != nil {
		return err
	}

	headers := headerPool.Get(n.Ctx())
	headers[0] = "publisherinfo"
	headers[1] = event.Data.PublisherInfo
	defer headerPool.Put(n.Ctx(), headers)

	return hc.Send(n.Ctx(), b, headers)
}

func (n Notifications) sendBlob(store *storage.Client, dataJSON []byte) (*url.URL, error) {
	if n.testSendBlob != nil {
		return n.testSendBlob(store, dataJSON)
	}

	// If store isn't set then this message is too large to send.
	if store == nil {
		return nil, fmt.Errorf("event exceeds max inline size and no storage client provided to store the data in a blob")
	}

	return store.Upload(n.Ctx(), uuid.New().String(), dataJSON)
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
		EventType:       EventType(data[0]),
	}, nil
}
