package msgs

import (
	"context"
	"errors"
	"net/url"
	"path"
	"testing"
	"time"

	"github.com/Azure/arn-sdk/internal/conn/http"
	"github.com/Azure/arn-sdk/internal/conn/storage"
	"github.com/Azure/arn-sdk/models/v3/schema/envelope"
	"github.com/Azure/arn-sdk/models/v3/schema/types"
	"github.com/Azure/arn-sdk/models/version"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/go-json-experiment/json"
	"github.com/google/uuid"
	"github.com/kylelemons/godebug/pretty"
)

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
		name        string
		ctx         context.Context
		promise     chan error
		prommiseErr bool
		wantErr     bool
	}{
		{
			name: "Promise is nil",
			ctx:  context.Background(),
		},
		{
			name:    "Error: context cancelled",
			ctx:     cancelCtx,
			promise: make(chan error, 1),
			wantErr: true,
		},
		{
			name:        "Error: promise error",
			ctx:         context.Background(),
			promise:     make(chan error, 1),
			prommiseErr: true,
			wantErr:     true,
		},
		{
			name:    "Success",
			ctx:     context.Background(),
			promise: make(chan error, 1),
			wantErr: false,
		},
	}

	for _, test := range tests {
		n := Notifications{
			promise: test.promise,
		}
		if n.promise != nil {
			if test.prommiseErr {
				n.promise <- errors.New("promise error")
			} else {
				n.promise <- nil
			}
		}

		err := n.Promise(test.ctx)
		switch {
		case test.wantErr && err == nil:
			t.Errorf("TestPromise(%s): got err == nil, want err != nil", test.name)
			continue
		case !test.wantErr && err != nil:
			t.Errorf("TestPromise(%s): got err == %s, want err == nil", test.name, err)
			continue
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
		ArmResource: mustNewArm(types.ActDelete, rescID, "2020-05-01", nil),
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

	want := []byte(`[{"resourceId":""}]`)

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
				Data:             []types.NotificationResource{{}},
			},
			want: envelope.Event{
				Data: types.Data{
					ResourcesContainer: types.RCInline,
					ResourceLocation:   "location",
					PublisherInfo:      "publisher",
					Resources:          []types.NotificationResource{{}},
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

func mustNewArm(act types.Activity, id *arm.ResourceID, apiVersion string, props any) types.ArmResource {
	resc, err := types.NewArmResource(act, id, "2024-01-01", props)
	if err != nil {
		panic(err)
	}
	return resc
}
