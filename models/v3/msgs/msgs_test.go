package msgs

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/Azure/arn-sdk/internal/conn/http"
	"github.com/Azure/arn-sdk/internal/conn/storage"
	"github.com/Azure/arn-sdk/models"
	"github.com/Azure/arn-sdk/models/v3/schema/envelope"
	"github.com/Azure/arn-sdk/models/v3/schema/types"
	"github.com/Azure/arn-sdk/models/version"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/go-json-experiment/json"
	"github.com/google/uuid"
	"github.com/kylelemons/godebug/pretty"
)

// testRscPrefix is the ARM resource-ID prefix shared by the fixtures in this file.
const testRscPrefix = "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/test/providers/Microsoft.Test/testResources/"

var expectedNow = time.Now().UTC()

func init() {
	nower = func() time.Time {
		return expectedNow
	}
}

func TestPromise(t *testing.T) {
	t.Parallel()

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name string
		ctx  context.Context
		// unresolved leaves the promise channel empty, so nothing is waiting to be received.
		unresolved  bool
		promise     chan error
		prommiseErr bool
		wantErr     bool
		wantTimeout bool
	}{
		{
			name: "Success: a nil promise returns nil",
			ctx:  context.Background(),
		},
		{
			name:    "Success: a delivered result beats an expired context",
			ctx:     cancelCtx,
			promise: make(chan error, 1),
		},
		{
			name:        "Error: the context expires with the notification still in flight",
			ctx:         cancelCtx,
			promise:     make(chan error, 1),
			unresolved:  true,
			wantErr:     true,
			wantTimeout: true,
		},
		{
			name:        "Error: the notification failed",
			ctx:         context.Background(),
			promise:     make(chan error, 1),
			prommiseErr: true,
			wantErr:     true,
		},
		{
			name:    "Success: the notification succeeded",
			ctx:     context.Background(),
			promise: make(chan error, 1),
		},
	}

	for _, test := range tests {
		n := Notifications{
			promise: test.promise,
		}
		if n.promise != nil && !test.unresolved {
			if test.prommiseErr {
				n.promise <- errors.New("promise error")
			} else {
				n.promise <- nil
			}
		}

		err := n.Promise(test.ctx)
		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestPromise(%s): got err == nil, want err != nil", test.name)
			continue
		case err != nil && !test.wantErr:
			t.Errorf("TestPromise(%s): got err == %s, want err == nil", test.name, err)
			continue
		}

		// A timeout must be distinguishable from a send failure, which is the whole point of
		// wrapping ErrPromiseTimeout rather than returning the raw context error.
		if got := errors.Is(err, models.ErrPromiseTimeout); got != test.wantTimeout {
			t.Errorf("TestPromise(%s): got errors.Is(err, ErrPromiseTimeout) == %v, want %v", test.name, got, test.wantTimeout)
		}
	}
}

func TestDataCount(t *testing.T) {
	t.Parallel()

	n := Notifications{Data: make([]types.NotificationResource, 100)}

	if n.DataCount() != 100 {
		t.Errorf("TestDataCount(): got %d, want 100", n.DataCount())
	}
}

func TestVersion(t *testing.T) {
	t.Parallel()

	n := Notifications{}

	if n.Version() != version.V3 {
		t.Errorf("TestVersion(): got %v, want 3.0", n.Version())
	}
}

func TestCtx(t *testing.T) {
	t.Parallel()

	n := Notifications{}

	ni := n.SetCtx(context.Background())
	if ni.Ctx() != context.Background() {
		t.Errorf("TestCtx(): got %v, want %v", n.Ctx(), context.Background())
	}
}

func TestSetPromise(t *testing.T) {
	t.Parallel()

	n := Notifications{}

	n = n.SetPromise(make(chan error, 1)).(Notifications)
	if n.promise == nil {
		t.Errorf("TestSetPromise(): got nil, want not nil")
	}
}

func TestSendPromise(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		e         error
		promiseCh chan error
		backupCh  chan error
		wantValue bool
	}{
		{
			name:     "Promise is nil and e is nil",
			backupCh: make(chan error, 1),
		},
		{
			name:      "Promise is nil but backupCh is not",
			e:         errors.New("error"),
			backupCh:  make(chan error, 1),
			wantValue: true,
		},
		{
			name:      "Promise is not nil",
			e:         errors.New("error"),
			backupCh:  make(chan error, 1),
			promiseCh: make(chan error, 1),
			wantValue: true,
		},
	}

	for _, test := range tests {
		n := Notifications{
			promise: test.promiseCh,
		}

		var getFrom chan error
		if test.promiseCh != nil {
			getFrom = test.promiseCh
		} else if test.backupCh != nil {
			getFrom = test.backupCh
		}

		n.SendPromise(test.e, test.backupCh)

		if test.wantValue {
			got := <-getFrom
			if got != test.e {
				t.Errorf("TestSendPromise(%s): got %v, want %v", test.name, got, test.e)
			}
		}
	}
}

func TestSendEvent(t *testing.T) {
	t.Parallel()

	prefix := `/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/test/providers/Microsoft.ContainerService/managedClusters/something/`
	suffix := `nodes/aks-nodepool1-12345678-vmss000000`
	rescID, err := arm.ParseResourceID(path.Join(prefix, suffix))
	if err != nil {
		panic(err)
	}

	goodNotifyResrc := types.NotificationResource{
		ResourceID: uuid.New().String(),
		APIVersion: "2024-01-01",
		ResourceSystemProperties: types.ResourceSystemProperties{
			ChangeAction: types.CADelete,
		},
		ArmResource: mustNewArm(types.ActDelete, rescID, "2024-01-01", nil),
	}

	blobNotificationResrcs := []types.NotificationResource{}
	for i := 0; i < 100; i++ {
		blobNotificationResrcs = append(blobNotificationResrcs, goodNotifyResrc)
	}

	httpCalled := false
	blobCalled := false

	tests := []struct {
		name         string
		n            Notifications
		expectInline bool
		wantErr      bool
	}{
		{
			name:    "Error: no data",
			n:       Notifications{},
			wantErr: true,
		},
		{
			name: "Error: event data doesn't validate",
			n: Notifications{
				Data: []types.NotificationResource{{}},
			},
			wantErr: true,
		},
		{
			name: "Error: inline HTTP call fails",
			n: Notifications{
				Data: []types.NotificationResource{goodNotifyResrc},
				testSendHTTP: func(*http.Client, envelope.Event) error {
					httpCalled = true
					return errors.New("http error")
				},
			},
			expectInline: true,
			wantErr:      true,
		},
		{
			name: "Success: Inline",
			n: Notifications{
				Data: []types.NotificationResource{goodNotifyResrc},
				testSendHTTP: func(*http.Client, envelope.Event) error {
					httpCalled = true
					return nil
				},
			},
			expectInline: true,
		},
		{
			name: "Error: Blob upload fails",
			n: Notifications{
				Data: blobNotificationResrcs,
				testSendHTTP: func(*http.Client, envelope.Event) error {
					httpCalled = true
					return nil
				},
				testSendBlob: func(*storage.Client, []byte) (*url.URL, error) {
					blobCalled = true
					return nil, errors.New("blob error")
				},
			},
			wantErr: true,
		},
		{
			name: "Error: Blob succeeds but HTTP fails",
			n: Notifications{
				Data: blobNotificationResrcs,
				testSendHTTP: func(*http.Client, envelope.Event) error {
					httpCalled = true
					return errors.New("http error")
				},
				testSendBlob: func(*storage.Client, []byte) (*url.URL, error) {
					blobCalled = true
					u, _ := url.Parse("https://blob")
					return u, nil
				},
			},
			wantErr: true,
		},
		{
			name: "Error: Blob upload returns a nil URL",
			n: Notifications{
				Data: blobNotificationResrcs,
				testSendHTTP: func(*http.Client, envelope.Event) error {
					httpCalled = true
					return nil
				},
				testSendBlob: func(*storage.Client, []byte) (*url.URL, error) {
					blobCalled = true
					return nil, nil
				},
			},
			wantErr: true,
		},
		{
			name: "Error: Blob upload returns a URL with no host or path",
			n: Notifications{
				Data: blobNotificationResrcs,
				testSendHTTP: func(*http.Client, envelope.Event) error {
					httpCalled = true
					return nil
				},
				testSendBlob: func(*storage.Client, []byte) (*url.URL, error) {
					blobCalled = true
					return &url.URL{}, nil
				},
			},
			wantErr: true,
		},
		{
			name: "Success: Blob",
			n: Notifications{
				Data: blobNotificationResrcs,
				testSendHTTP: func(*http.Client, envelope.Event) error {
					httpCalled = true
					return nil
				},
				testSendBlob: func(*storage.Client, []byte) (*url.URL, error) {
					blobCalled = true
					u, _ := url.Parse("https://blob")
					return u, nil
				},
			},
		},
	}

	for _, test := range tests {
		// These are set by the test closures above, which capture them. They must be reset per
		// case or a case that sets one leaks it into every case that follows.
		httpCalled = false
		blobCalled = false

		err := test.n.SendEvent(nil, nil)
		switch {
		case test.wantErr && err == nil:
			t.Errorf("TestSendEvent(%s): got err == nil, want err != nil", test.name)
			continue
		case !test.wantErr && err != nil:
			t.Errorf("TestSendEvent(%s): got err == %s, want err == nil", test.name, err)
			continue
		case err != nil:
			continue
		}

		if !httpCalled {
			t.Errorf("TestSendEvent(%s): got httpCalled == false, want true", test.name)
		}

		if test.expectInline {
			if blobCalled {
				t.Errorf("TestSendEvent(%s): got blobCalled == true, want false", test.name)
			}
			continue
		}
		if !blobCalled {
			t.Errorf("TestSendEvent(%s): got blobCalled == false, want true", test.name)
		}
	}
}

func TestDataToJSON(t *testing.T) {
	t.Parallel()

	n := Notifications{
		Data: []types.NotificationResource{{}},
	}

	// statusCode is emitted by types.Status.MarshalJSONTo, which defaults the zero value to
	// types.StatusCode. Producers never set it themselves.
	want := []byte(`[{"resourceId":"","statusCode":"OK"}]`)

	got, err := n.dataToJSON()
	if err != nil {
		panic(err)
	}

	if string(got) != string(want) {
		t.Errorf("TestDataToJSON: got %s, want %s", got, want)
	}
}

func TestToEvent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		n       Notifications
		want    envelope.Event
		wantErr bool
	}{
		{
			name:    "Error: no data",
			n:       Notifications{},
			wantErr: true,
		},
		{
			name: "Success: inline data",
			n: Notifications{
				ResourceLocation: "location",
				PublisherInfo:    "publisher",
				Data:             []types.NotificationResource{createTestResource(1, "", "")},
			},
			want: envelope.Event{
				Data: types.Data{
					ResourcesContainer: types.RCInline,
					ResourceLocation:   "location",
					PublisherInfo:      "publisher",
					Resources:          []types.NotificationResource{createTestResource(1, "", "")},
					AdditionalBatchProperties: types.AdditionalBatchProperties{
						SDKVersion: "golang@0.1.0",
						BatchSize:  1,
					},
				},
			},
		},
	}

	for _, test := range tests {
		if !test.wantErr {
			em, err := newEventMeta(test.want.Data.Resources)
			if err != nil {
				panic(err)
			}
			em.ID = ""
			test.want.EventMeta = em

			if len(test.n.Data) == 1 {
				b, err := json.Marshal(test.n.Data)
				if err != nil {
					panic(err)
				}
				test.want.Data.Data = b
			}
		}

		_, got, err := test.n.toEvent()
		switch {
		case test.wantErr && err == nil:
			t.Errorf("TestToEvent(%s): got err == nil, want err != nil", test.name)
			continue
		case !test.wantErr && err != nil:
			t.Errorf("TestToEvent(%s): got err == %s, want err == nil", test.name, err)
			continue
		case err != nil:
			continue
		}
		got.EventMeta.ID = ""

		if diff := pretty.Compare(test.want, got); diff != "" {
			t.Errorf("TestToEvent(%s): -want/+got:\n%s", test.name, diff)
		}
	}
}

func TestNewInline(t *testing.T) {
	t.Parallel()

	blob := []types.NotificationResource{}
	for i := 0; i < 1000; i++ {
		n := types.NotificationResource{ResourceID: uuid.New().String()}
		blob = append(blob, n)
	}

	tests := []struct {
		name         string
		n            Notifications
		shouldInline bool
	}{
		{
			name: "Success: inline data",
			n: Notifications{
				Data: []types.NotificationResource{{}},
			},
			shouldInline: true,
		},
		{
			name: "Success: send data to blob",
			n: Notifications{
				Data: blob,
			},
			shouldInline: false,
		},
	}

	for _, test := range tests {
		data, inline, err := test.n.inline()
		if err != nil {
			t.Errorf("TestNewInline(%s): got err == %s, want err == nil", test.name, err)
			continue
		}
		if inline != test.shouldInline {
			t.Errorf("TestNewInline(%s): got inline == %t, want inline == %t", test.name, inline, test.shouldInline)
		}
		if len(data) == 0 {
			t.Errorf("TestNewInline(%s): got no JSON data, want JSON data", test.name)
		}
	}
}

func TestNewEventMetaData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		data    []types.NotificationResource
		want    envelope.EventMeta
		wantErr bool
	}{
		{
			name:    "Error: no data",
			wantErr: true,
		},
		{
			name: "Success",
			data: []types.NotificationResource{{}},
			want: envelope.EventMeta{
				DataVersion:     version.V3,
				MetadataVersion: "1.0",
				EventTime:       expectedNow,
				EventType:       `/`,
			},
		},
	}

	for _, test := range tests {
		env, err := newEventMeta(test.data)
		switch {
		case test.wantErr && err == nil:
			t.Errorf("TestNewEventMetaData(%s): got err == nil, want err != nil", test.name)
			continue
		case !test.wantErr && err != nil:
			t.Errorf("TestNewEventMetaData(%s): got err == %s, want err == nil", test.name, err)
			continue
		case err != nil:
			continue
		}

		if env.ID == "" {
			t.Errorf("TestNewEventMetaData(%s): envelope.ID: got %s, want not empty", test.name, env.ID)
		}

		env.ID = ""
		if env.Subject == "" {
			t.Errorf("TestNewEventMetaData(%s): envelope.Subject: got %s, want not empty", test.name, env.Subject)
		}
		env.Subject = ""

		if diff := pretty.Compare(test.want, env); diff != "" {
			t.Errorf("TestNewEventMetaData(%s): -want/+got:\n%s", test.name, diff)
		}
	}
}

// TestTenantIDPropagation verifies that tenant IDs set by caller at parent level
// are correctly preserved and propagated to child resources for inline notifications.
func TestTenantIDPropagation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                 string
		homeTenantID         string
		resourceHomeTenantID string
		resourceCount        int
	}{
		{
			name:                 "Single resource with tenant IDs",
			homeTenantID:         "11111111-1111-1111-1111-111111111111",
			resourceHomeTenantID: "22222222-2222-2222-2222-222222222222",
			resourceCount:        1,
		},
		{
			name:                 "Multiple resources with same tenant IDs",
			homeTenantID:         "33333333-3333-3333-3333-333333333333",
			resourceHomeTenantID: "44444444-4444-4444-4444-444444444444",
			resourceCount:        3,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var resources []types.NotificationResource
			for i := 1; i <= test.resourceCount; i++ {
				resources = append(resources, createTestResource(i, test.homeTenantID, test.resourceHomeTenantID))
			}

			notifications := Notifications{
				ResourceLocation:     "eastus",
				PublisherInfo:        "Microsoft.Test",
				HomeTenantID:         test.homeTenantID,
				ResourceHomeTenantID: test.resourceHomeTenantID,
				Data:                 resources,
			}

			_, event, err := notifications.toEvent()
			if err != nil {
				t.Fatalf("toEvent() failed: %v", err)
			}

			// Verify inline path was used (not blob)
			if event.Data.ResourcesContainer != types.RCInline {
				t.Errorf("Expected inline path (RCInline), got: %v", event.Data.ResourcesContainer)
			}

			// Verify parent Data struct preserves caller's tenant ID values
			if event.Data.HomeTenantID != test.homeTenantID {
				t.Errorf("Parent HomeTenantID not preserved: got %q, want %q",
					event.Data.HomeTenantID, test.homeTenantID)
			}
			if event.Data.ResourceHomeTenantID != test.resourceHomeTenantID {
				t.Errorf("Parent ResourceHomeTenantID not preserved: got %q, want %q",
					event.Data.ResourceHomeTenantID, test.resourceHomeTenantID)
			}

			// Verify all child resources retain their tenant IDs
			for i, resource := range event.Data.Resources {
				if resource.HomeTenantID != test.homeTenantID {
					t.Errorf("Child resource[%d] HomeTenantID: got %q, want %q",
						i, resource.HomeTenantID, test.homeTenantID)
				}
				if resource.ResourceHomeTenantID != test.resourceHomeTenantID {
					t.Errorf("Child resource[%d] ResourceHomeTenantID: got %q, want %q",
						i, resource.ResourceHomeTenantID, test.resourceHomeTenantID)
				}
			}
		})
	}
}

// tenantIDs holds the pair of tenant IDs carried by either a Notifications or a NotificationResource, so a
// test case can state both levels in the same shape.
type tenantIDs struct {
	home         string
	resourceHome string
}

// TestTenantIDValidation tests that validation catches inconsistent tenant IDs between the
// Data level and the resource level. Tenant IDs must be identical at both levels, so each
// error case below wrongs exactly one field relative to the valid baseline in the first case.
func TestTenantIDValidation(t *testing.T) {
	t.Parallel()

	const (
		home         = "11111111-1111-1111-1111-111111111111"
		resourceHome = "22222222-2222-2222-2222-222222222222"
		other        = "99999999-9999-9999-9999-999999999999"
	)

	tests := []struct {
		name      string
		parent    tenantIDs
		resources []tenantIDs
		wantErr   bool
	}{
		{
			name:      "Success: parent and resource tenant IDs match",
			parent:    tenantIDs{home: home, resourceHome: resourceHome},
			resources: []tenantIDs{{home: home, resourceHome: resourceHome}},
		},
		{
			name:      "Success: tenant IDs unset at both levels",
			parent:    tenantIDs{},
			resources: []tenantIDs{{}},
		},
		{
			name:      "Success: two resources both matching the parent",
			parent:    tenantIDs{home: home, resourceHome: resourceHome},
			resources: []tenantIDs{{home: home, resourceHome: resourceHome}, {home: home, resourceHome: resourceHome}},
		},
		{
			name:      "Error: resource HomeTenantID differs from the parent",
			parent:    tenantIDs{home: home, resourceHome: resourceHome},
			resources: []tenantIDs{{home: other, resourceHome: resourceHome}},
			wantErr:   true,
		},
		{
			name:      "Error: resource HomeTenantID empty while the parent sets it",
			parent:    tenantIDs{home: home, resourceHome: resourceHome},
			resources: []tenantIDs{{home: "", resourceHome: resourceHome}},
			wantErr:   true,
		},
		{
			name:      "Error: parent HomeTenantID empty while the resource sets it",
			parent:    tenantIDs{home: "", resourceHome: resourceHome},
			resources: []tenantIDs{{home: home, resourceHome: resourceHome}},
			wantErr:   true,
		},
		{
			name:      "Error: resource ResourceHomeTenantID differs from the parent",
			parent:    tenantIDs{home: home, resourceHome: resourceHome},
			resources: []tenantIDs{{home: home, resourceHome: other}},
			wantErr:   true,
		},
		{
			name:      "Error: resource ResourceHomeTenantID empty while the parent sets it",
			parent:    tenantIDs{home: home, resourceHome: resourceHome},
			resources: []tenantIDs{{home: home, resourceHome: ""}},
			wantErr:   true,
		},
		{
			name:      "Error: parent ResourceHomeTenantID empty while the resource sets it",
			parent:    tenantIDs{home: home, resourceHome: ""},
			resources: []tenantIDs{{home: home, resourceHome: resourceHome}},
			wantErr:   true,
		},
		{
			name:      "Error: second resource HomeTenantID differs from the parent",
			parent:    tenantIDs{home: home, resourceHome: resourceHome},
			resources: []tenantIDs{{home: home, resourceHome: resourceHome}, {home: other, resourceHome: resourceHome}},
			wantErr:   true,
		},
		{
			name:      "Error: second resource ResourceHomeTenantID differs from the parent",
			parent:    tenantIDs{home: home, resourceHome: resourceHome},
			resources: []tenantIDs{{home: home, resourceHome: resourceHome}, {home: home, resourceHome: other}},
			wantErr:   true,
		},
	}

	for _, test := range tests {
		var resources []types.NotificationResource
		for i, ids := range test.resources {
			resources = append(resources, createTestResource(i+1, ids.home, ids.resourceHome))
		}

		notifications := Notifications{
			ResourceLocation:     "eastus",
			PublisherInfo:        "Microsoft.Test",
			HomeTenantID:         test.parent.home,
			ResourceHomeTenantID: test.parent.resourceHome,
			Data:                 resources,
		}

		_, event, err := notifications.toEvent()
		if err != nil {
			t.Errorf("TestTenantIDValidation(%s): toEvent(): got err == %s, want err == nil", test.name, err)
			continue
		}

		err = event.Validate()
		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestTenantIDValidation(%s): got err == nil, want err != nil", test.name)
			continue
		case err != nil && !test.wantErr:
			t.Errorf("TestTenantIDValidation(%s): got err == %s, want err == nil", test.name, err)
			continue
		case err != nil:
			continue
		}
	}
}

// TestSendEventDoesNotMutateCallerData verifies SendEvent never writes StatusCode into the caller's
// slice. The default is applied at marshal time by types.Status.MarshalJSONTo instead, so the caller's
// resources come back exactly as they were handed over.
func TestSendEventDoesNotMutateCallerData(t *testing.T) {
	t.Parallel()

	rescID, err := arm.ParseResourceID(testRscPrefix + "resource1")
	if err != nil {
		t.Fatalf("TestSendEventDoesNotMutateCallerData: ParseResourceID(): got err == %s, want err == nil", err)
	}

	tests := []struct {
		name     string
		count    int
		wantBlob bool
	}{
		{name: "Success: inline path leaves caller data untouched", count: 1},
		{name: "Success: blob path leaves caller data untouched", count: 100, wantBlob: true},
	}

	for _, test := range tests {
		blobCalled := false
		callerData := make([]types.NotificationResource, 0, test.count)
		for i := 0; i < test.count; i++ {
			callerData = append(callerData, types.NotificationResource{
				ResourceID:               rescID.String(),
				APIVersion:               "2024-01-01",
				ResourceSystemProperties: types.ResourceSystemProperties{ChangeAction: types.CACreate},
				ArmResource:              mustNewArm(types.ActWrite, rescID, "2024-01-01", map[string]any{"k": "v"}),
			})
		}

		n := Notifications{
			ResourceLocation: "eastus",
			PublisherInfo:    "Microsoft.Test",
			Data:             callerData,
			testSendHTTP:     func(*http.Client, envelope.Event) error { return nil },
			testSendBlob: func(*storage.Client, []byte) (*url.URL, error) {
				blobCalled = true
				return url.Parse("https://blob/x")
			},
		}

		if err := n.SendEvent(nil, nil); err != nil {
			t.Errorf("TestSendEventDoesNotMutateCallerData(%s): got err == %s, want err == nil", test.name, err)
			continue
		}

		// Without this the "blob path" case would silently degrade into a second inline test if the
		// fixture or maxvals.InlineSize ever changed.
		if blobCalled != test.wantBlob {
			t.Errorf("TestSendEventDoesNotMutateCallerData(%s): got blobCalled == %v, want %v", test.name, blobCalled, test.wantBlob)
		}

		for i, r := range callerData {
			if r.StatusCode != "" {
				t.Errorf("TestSendEventDoesNotMutateCallerData(%s): callerData[%d]: got %q, want empty", test.name, i, r.StatusCode)
			}
		}
	}
}

// TestTenantIDPropagationBlobPath tests that caller-set tenant IDs are preserved correctly
// for notifications that use the blob storage path
func TestTenantIDPropagationBlobPath(t *testing.T) {
	t.Parallel()

	testHomeTenantID := "55555555-5555-5555-5555-555555555555"
	testResourceHomeTenantID := "66666666-6666-6666-6666-666666666666"

	// Create many resources to force blob path (exceeding inline size limit)
	var resources []types.NotificationResource
	for i := 1; i <= 50; i++ {
		resource := createTestResource(i, testHomeTenantID, testResourceHomeTenantID)
		// Add properties to increase payload size
		resource.ArmResource = mustNewArm(types.ActWrite, resource.ArmResource.ResourceID(), "2024-01-01", map[string]any{
			"property1": strings.Repeat(fmt.Sprintf("large-property-value-%d-", i), 10),
			"property2": make(map[string]string),
		})
		resources = append(resources, resource)
	}

	notifications := Notifications{
		ResourceLocation:     "westus2",
		PublisherInfo:        "Microsoft.Test",
		HomeTenantID:         testHomeTenantID,
		ResourceHomeTenantID: testResourceHomeTenantID,
		Data:                 resources,
	}

	_, event, err := notifications.toEvent()
	if err != nil {
		t.Fatalf("toEvent() failed: %v", err)
	}

	// Verify this took the blob path
	if event.Data.ResourcesContainer != types.RCBlob {
		t.Errorf("Expected blob path (RCBlob), got: %v", event.Data.ResourcesContainer)
	}

	if event.Data.HomeTenantID != testHomeTenantID {
		t.Errorf("Blob path HomeTenantID not preserved: got %q, want %q",
			event.Data.HomeTenantID, testHomeTenantID)
	}
	if event.Data.ResourceHomeTenantID != testResourceHomeTenantID {
		t.Errorf("Blob path ResourceHomeTenantID not preserved: got %q, want %q",
			event.Data.ResourceHomeTenantID, testResourceHomeTenantID)
	}

	// Verify all child resources still have their tenant IDs in blob path
	for i, resource := range event.Data.Resources {
		if resource.HomeTenantID != testHomeTenantID {
			t.Errorf("Child resource[%d] HomeTenantID: got %q, want %q",
				i, resource.HomeTenantID, testHomeTenantID)
		}
		if resource.ResourceHomeTenantID != testResourceHomeTenantID {
			t.Errorf("Child resource[%d] ResourceHomeTenantID: got %q, want %q",
				i, resource.ResourceHomeTenantID, testResourceHomeTenantID)
		}
	}
}

// TestTenantIDJSONMarshaling tests that tenant IDs are correctly written out during
// JSON marshaling and survive a marshal/unmarshal roundtrip cycle
func TestTenantIDJSONMarshaling(t *testing.T) {
	t.Parallel()

	testHomeTenantID := "77777777-7777-7777-7777-777777777777"
	testResourceHomeTenantID := "88888888-8888-8888-8888-888888888888"

	resource := createTestResource(1, testHomeTenantID, testResourceHomeTenantID)

	notifications := Notifications{
		ResourceLocation:     "eastus",
		PublisherInfo:        "Microsoft.Test",
		HomeTenantID:         testHomeTenantID,
		ResourceHomeTenantID: testResourceHomeTenantID,
		Data:                 []types.NotificationResource{resource},
	}

	_, event, err := notifications.toEvent()
	if err != nil {
		t.Fatalf("toEvent() failed: %v", err)
	}

	jsonBytes, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Failed to marshal event to JSON: %v", err)
	}

	var unmarshaled envelope.Event
	_ = json.Unmarshal(jsonBytes, &unmarshaled)
	if unmarshaled.Data.HomeTenantID != testHomeTenantID {
		t.Errorf("Data.HomeTenantID not preserved in roundtrip: got %q, want %q",
			unmarshaled.Data.HomeTenantID, testHomeTenantID)
	}
	if unmarshaled.Data.ResourceHomeTenantID != testResourceHomeTenantID {
		t.Errorf("Data.ResourceHomeTenantID not preserved in roundtrip: got %q, want %q",
			unmarshaled.Data.ResourceHomeTenantID, testResourceHomeTenantID)
	}
}

func createTestResource(id int, homeTenantID, resourceHomeTenantID string) types.NotificationResource {
	rescID, err := arm.ParseResourceID(fmt.Sprintf("%sresource%d", testRscPrefix, id))
	if err != nil {
		panic(err)
	}

	return types.NotificationResource{
		ResourceID:           rescID.String(),
		APIVersion:           "2024-01-01",
		StatusCode:           types.StatusCode, // Required by validation
		HomeTenantID:         homeTenantID,
		ResourceHomeTenantID: resourceHomeTenantID,
		ResourceSystemProperties: types.ResourceSystemProperties{
			ChangeAction: types.CACreate,
		},
		ArmResource: mustNewArm(types.ActWrite, rescID, "2024-01-01", map[string]any{
			"property1": fmt.Sprintf("value%d", id),
		}),
	}
}

func mustNewArm(act types.Activity, id *arm.ResourceID, apiVersion string, props any) types.ArmResource {
	resc, err := types.NewArmResource(act, id, apiVersion, props)
	if err != nil {
		panic(err)
	}
	return resc
}

// TestNewFieldPropagation pins that Notifications.APIVersion and Notifications.DataBoundary reach
// types.Data on both the inline and blob paths. Both fields were added without any assertion that
// toEvent() actually carries them.
func TestNewFieldPropagation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		count         int
		wantContainer types.ResourcesContainer
	}{
		{name: "Success: inline path carries both fields", count: 1, wantContainer: types.RCInline},
		{name: "Success: blob path carries both fields", count: 100, wantContainer: types.RCBlob},
	}

	for _, test := range tests {
		var resources []types.NotificationResource
		for i := 0; i < test.count; i++ {
			resources = append(resources, createTestResource(i+1, "", ""))
		}

		n := Notifications{
			ResourceLocation: "eastus",
			PublisherInfo:    "Microsoft.Test",
			APIVersion:       "2024-01-01",
			DataBoundary:     types.DBGlobal,
			Data:             resources,
		}

		_, event, err := n.toEvent()
		if err != nil {
			t.Errorf("TestNewFieldPropagation(%s): toEvent(): got err == %s, want err == nil", test.name, err)
			continue
		}

		if event.Data.ResourcesContainer != test.wantContainer {
			got := event.Data.ResourcesContainer
			t.Errorf("TestNewFieldPropagation(%s): got container == %v, want %v", test.name, got, test.wantContainer)
		}
		if event.Data.APIVersion != "2024-01-01" {
			t.Errorf("TestNewFieldPropagation(%s): got APIVersion == %q, want %q", test.name, event.Data.APIVersion, "2024-01-01")
		}
		if event.Data.DataBoundary != types.DBGlobal {
			t.Errorf("TestNewFieldPropagation(%s): got DataBoundary == %v, want %v", test.name, event.Data.DataBoundary, types.DBGlobal)
		}
	}
}
