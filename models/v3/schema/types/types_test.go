package types

import (
	stdjson "encoding/json"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/go-json-experiment/json"
)

const (
	rscPrefix     = "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/test/providers/Microsoft.Test/testResources/"
	otherPrefix   = "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/test/providers/Microsoft.Other/otherResources/"
	testAPIVer    = "2024-01-01"
	testTenant    = "11111111-1111-1111-1111-111111111111"
	testRscTenant = "22222222-2222-2222-2222-222222222222"
)

// validResource returns a NotificationResource that passes validation. Callers break exactly one
// field of it (or of the Data holding it) to produce an error case.
func validResource(t *testing.T, prefix, name string) NotificationResource {
	t.Helper()

	id, err := arm.ParseResourceID(prefix + name)
	if err != nil {
		t.Fatalf("validResource(%s): could not parse resource ID: %s", name, err)
	}
	a, err := NewArmResource(ActWrite, id, testAPIVer, map[string]any{"prop": name})
	if err != nil {
		t.Fatalf("validResource(%s): NewArmResource(): %s", name, err)
	}

	return NotificationResource{
		ResourceID:               id.String(),
		APIVersion:               testAPIVer,
		StatusCode:               StatusCode,
		ResourceSystemProperties: ResourceSystemProperties{ChangeAction: CACreate},
		ArmResource:              a,
	}
}

// validData returns a Data that passes validation. Each test case mutates exactly one field.
func validData(t *testing.T) Data {
	t.Helper()

	return Data{
		ResourcesContainer: RCInline,
		ResourceLocation:   "eastus",
		PublisherInfo:      "Microsoft.Test",
		Resources:          []NotificationResource{validResource(t, rscPrefix, "resource1")},
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(t *testing.T, d *Data)
		wantErr bool
	}{
		{
			name: "Success: inline with a single valid resource",
		},
		{
			name: "Success: inline with two resources of the same type",
			mutate: func(t *testing.T, d *Data) {
				d.Resources = append(d.Resources, validResource(t, rscPrefix, "resource2"))
			},
		},
		{
			name:   "Success: blob container with valid resources",
			mutate: func(t *testing.T, d *Data) { d.ResourcesContainer = RCBlob },
		},
		{
			name: "Error: blob container is validated the same as inline",
			mutate: func(t *testing.T, d *Data) {
				d.ResourcesContainer = RCBlob
				d.Resources[0].StatusCode = "BadRequest"
			},
			wantErr: true,
		},
		{
			name: "Error: blob container carrying the inline wire payload",
			mutate: func(t *testing.T, d *Data) {
				d.ResourcesContainer = RCBlob
				d.Data = []byte(`[{"resourceId":"leaked"}]`)
			},
			wantErr: true,
		},
		{
			name: "Error: inline container with a populated ResourcesBlobInfo",
			mutate: func(t *testing.T, d *Data) {
				d.ResourcesBlobInfo = ResourcesBlobInfo{BlobURI: "https://blob/x", BlobSize: 10}
			},
			wantErr: true,
		},
		{
			// Precondition: Data supplies the version. The resource's conflicting version is the wrong input.
			name: "Error: resource APIVersion conflicts with the one Data supplies",
			mutate: func(t *testing.T, d *Data) {
				d.APIVersion = testAPIVer
				d.Resources[0].APIVersion = "2023-01-01"
			},
			wantErr: true,
		},
		{
			name: "Success: ArmResource APIVersion empty inherits the effective version",
			mutate: func(t *testing.T, d *Data) {
				d.Resources[0].ArmResource.APIVersion = ""
			},
		},
		{
			name:    "Error: blob container with no resources",
			mutate:  func(t *testing.T, d *Data) { d.ResourcesContainer = RCBlob; d.Resources = nil },
			wantErr: true,
		},
		{
			name:   "Success: APIVersion set on Data and matching the resources",
			mutate: func(t *testing.T, d *Data) { d.APIVersion = testAPIVer },
		},
		{
			name: "Success: APIVersion set on Data and left empty on the resource",
			mutate: func(t *testing.T, d *Data) {
				d.APIVersion = testAPIVer
				d.Resources[0].APIVersion = ""
			},
		},
		{
			// The first two mutations are the precondition -- Data supplies the version, the resource
			// defers to it. Only ArmResource.APIVersion is the wrong input under test.
			name: "Error: ArmResource APIVersion differs from the one Data supplies",
			mutate: func(t *testing.T, d *Data) {
				d.APIVersion = testAPIVer
				d.Resources[0].APIVersion = ""
				d.Resources[0].ArmResource.APIVersion = "2023-01-01"
			},
			wantErr: true,
		},
		{
			name: "Success: tenant IDs set and matching at both levels",
			mutate: func(t *testing.T, d *Data) {
				d.HomeTenantID = testTenant
				d.ResourceHomeTenantID = testRscTenant
				d.Resources[0].HomeTenantID = testTenant
				d.Resources[0].ResourceHomeTenantID = testRscTenant
			},
		},
		{
			name:   "Success: DataBoundary set to global",
			mutate: func(t *testing.T, d *Data) { d.DataBoundary = DBGlobal },
		},
		{
			name:   "Success: DataBoundary set to eu",
			mutate: func(t *testing.T, d *Data) { d.DataBoundary = DBEU },
		},
		{
			name:    "Error: ResourcesContainer is unset",
			mutate:  func(t *testing.T, d *Data) { d.ResourcesContainer = RCUnknown },
			wantErr: true,
		},
		{
			name:    "Error: ResourcesContainer is out of range",
			mutate:  func(t *testing.T, d *Data) { d.ResourcesContainer = ResourcesContainer(3) },
			wantErr: true,
		},
		{
			name:    "Error: DataBoundary is out of range",
			mutate:  func(t *testing.T, d *Data) { d.DataBoundary = DataBoundary(3) },
			wantErr: true,
		},
		{
			name:    "Error: inline container with no resources",
			mutate:  func(t *testing.T, d *Data) { d.Resources = nil },
			wantErr: true,
		},
		{
			name:    "Error: resource has no ResourceID",
			mutate:  func(t *testing.T, d *Data) { d.Resources[0].ResourceID = "" },
			wantErr: true,
		},
		{
			name:   "Success: resource StatusCode is empty and defaults on the wire",
			mutate: func(t *testing.T, d *Data) { d.Resources[0].StatusCode = "" },
		},
		{
			name:    "Error: resource StatusCode is not OK",
			mutate:  func(t *testing.T, d *Data) { d.Resources[0].StatusCode = "BadRequest" },
			wantErr: true,
		},
		{
			name:    "Error: resource ChangeAction is unset",
			mutate:  func(t *testing.T, d *Data) { d.Resources[0].ResourceSystemProperties.ChangeAction = CAUnknown },
			wantErr: true,
		},
		{
			name:    "Error: resource ChangeAction is out of range",
			mutate:  func(t *testing.T, d *Data) { d.Resources[0].ResourceSystemProperties.ChangeAction = ChangeAction(5) },
			wantErr: true,
		},
		{
			name:    "Error: ArmResource was not built by NewArmResource",
			mutate:  func(t *testing.T, d *Data) { d.Resources[0].ArmResource.arm = nil },
			wantErr: true,
		},
		{
			name:    "Error: ArmResource has no ID",
			mutate:  func(t *testing.T, d *Data) { d.Resources[0].ArmResource.ID = "" },
			wantErr: true,
		},
		{
			name: "Error: second resource has a different resource type",
			mutate: func(t *testing.T, d *Data) {
				d.Resources = append(d.Resources, validResource(t, otherPrefix, "resource2"))
			},
			wantErr: true,
		},
		{
			name:    "Error: resource APIVersion is empty when Data APIVersion is empty",
			mutate:  func(t *testing.T, d *Data) { d.Resources[0].APIVersion = "" },
			wantErr: true,
		},
		{
			name:    "Error: resource APIVersion does not match its ArmResource APIVersion",
			mutate:  func(t *testing.T, d *Data) { d.Resources[0].APIVersion = "2023-01-01" },
			wantErr: true,
		},
		{
			name: "Error: second resource has a different APIVersion",
			mutate: func(t *testing.T, d *Data) {
				second := validResource(t, rscPrefix, "resource2")
				second.APIVersion = "2023-01-01"
				second.ArmResource.APIVersion = "2023-01-01" // Kept consistent so only the inter-resource rule breaks.
				d.Resources = append(d.Resources, second)
			},
			wantErr: true,
		},
		{
			name:    "Error: Data sets HomeTenantID but the resource does not",
			mutate:  func(t *testing.T, d *Data) { d.HomeTenantID = testTenant },
			wantErr: true,
		},
		{
			name:    "Error: resource sets HomeTenantID but Data does not",
			mutate:  func(t *testing.T, d *Data) { d.Resources[0].HomeTenantID = testTenant },
			wantErr: true,
		},
		{
			name:    "Error: Data sets ResourceHomeTenantID but the resource does not",
			mutate:  func(t *testing.T, d *Data) { d.ResourceHomeTenantID = testRscTenant },
			wantErr: true,
		},
		{
			name:    "Error: resource sets ResourceHomeTenantID but Data does not",
			mutate:  func(t *testing.T, d *Data) { d.Resources[0].ResourceHomeTenantID = testRscTenant },
			wantErr: true,
		},
	}

	for _, test := range tests {
		d := validData(t)
		if test.mutate != nil {
			test.mutate(t, &d)
		}

		err := d.Validate()
		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestValidate(%s): got err == nil, want err != nil", test.name)
			continue
		case err != nil && !test.wantErr:
			t.Errorf("TestValidate(%s): got err == %s, want err == nil", test.name, err)
			continue
		case err != nil:
			continue
		}
	}
}

// TestNotificationResourceValidate is type-qualified because this package has five Validate
// methods and they cannot all be named TestValidate.
func TestNotificationResourceValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(n *NotificationResource)
		wantErr bool
	}{
		{
			name: "Success: all required fields set",
		},
		{
			name:    "Error: ResourceID is empty",
			mutate:  func(n *NotificationResource) { n.ResourceID = "" },
			wantErr: true,
		},
		{
			name:   "Success: StatusCode is empty and defaults to OK on the wire",
			mutate: func(n *NotificationResource) { n.StatusCode = "" },
		},
		{
			name:    "Error: StatusCode is neither empty nor OK",
			mutate:  func(n *NotificationResource) { n.StatusCode = "BadRequest" },
			wantErr: true,
		},
		{
			name:    "Error: ArmResource is invalid",
			mutate:  func(n *NotificationResource) { n.ArmResource.ID = "" },
			wantErr: true,
		},
		{
			name:    "Error: ResourceSystemProperties is invalid",
			mutate:  func(n *NotificationResource) { n.ResourceSystemProperties.ChangeAction = CAUnknown },
			wantErr: true,
		},
	}

	for _, test := range tests {
		n := validResource(t, rscPrefix, "resource1")
		if test.mutate != nil {
			test.mutate(&n)
		}

		err := n.Validate()
		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestNotificationResourceValidate(%s): got err == nil, want err != nil", test.name)
			continue
		case err != nil && !test.wantErr:
			t.Errorf("TestNotificationResourceValidate(%s): got err == %s, want err == nil", test.name, err)
			continue
		case err != nil:
			continue
		}
	}
}

func TestArmResourceValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(a *ArmResource)
		wantErr bool
	}{
		{
			name: "Success: write activity with properties",
		},
		{
			name:   "Success: snapshot activity with properties",
			mutate: func(a *ArmResource) { a.act = ActSnapshot },
		},
		{
			name: "Success: delete activity needs no properties",
			mutate: func(a *ArmResource) {
				a.act = ActDelete
				a.Properties = nil
			},
		},
		{
			name:    "Error: ID is empty",
			mutate:  func(a *ArmResource) { a.ID = "" },
			wantErr: true,
		},
		{
			name:    "Error: write activity without properties",
			mutate:  func(a *ArmResource) { a.Properties = nil },
			wantErr: true,
		},
		{
			name:    "Error: activity is unset",
			mutate:  func(a *ArmResource) { a.act = ActUnknown },
			wantErr: true,
		},
	}

	for _, test := range tests {
		a := validResource(t, rscPrefix, "resource1").ArmResource
		if test.mutate != nil {
			test.mutate(&a)
		}

		err := a.Validate()
		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestArmResourceValidate(%s): got err == nil, want err != nil", test.name)
			continue
		case err != nil && !test.wantErr:
			t.Errorf("TestArmResourceValidate(%s): got err == %s, want err == nil", test.name, err)
			continue
		case err != nil:
			continue
		}
	}
}

func TestResourceSystemPropertiesValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		changeAction ChangeAction
		wantErr      bool
	}{
		{name: "Success: ChangeAction is Create", changeAction: CACreate},
		{name: "Success: ChangeAction is Update", changeAction: CAUpdate},
		{name: "Error: ChangeAction is unset", changeAction: CAUnknown, wantErr: true},
		{name: "Error: ChangeAction is out of range", changeAction: ChangeAction(5), wantErr: true},
	}

	for _, test := range tests {
		err := ResourceSystemProperties{ChangeAction: test.changeAction}.Validate()
		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestResourceSystemPropertiesValidate(%s): got err == nil, want err != nil", test.name)
			continue
		case err != nil && !test.wantErr:
			t.Errorf("TestResourceSystemPropertiesValidate(%s): got err == %s, want err == nil", test.name, err)
			continue
		case err != nil:
			continue
		}
	}
}

func TestResourcesBlobInfoValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		blobURI string
		size    int64
		wantErr bool
	}{
		{name: "Success: URI and size set", blobURI: "https://blob/x", size: 10},
		{name: "Error: BlobURI is empty", blobURI: "", size: 10, wantErr: true},
		{name: "Error: BlobSize is zero", blobURI: "https://blob/x", size: 0, wantErr: true},
		{name: "Error: BlobSize is negative", blobURI: "https://blob/x", size: -1, wantErr: true},
	}

	for _, test := range tests {
		r := ResourcesBlobInfo{BlobURI: test.blobURI, BlobSize: test.size}

		err := r.Validate()
		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestResourcesBlobInfoValidate(%s): got err == nil, want err != nil", test.name)
			continue
		case err != nil && !test.wantErr:
			t.Errorf("TestResourcesBlobInfoValidate(%s): got err == %s, want err == nil", test.name, err)
			continue
		case err != nil:
			continue
		}
	}
}

// TestDataBoundaryMarshal pins the wire format of the DataBoundary enum, which is now the type of
// Data.DataBoundary. The enum's MarshalJSON returns the -linecomment text verbatim, so the JSON
// quotes come from the constant's line comment rather than from the marshaler.
func TestDataBoundaryMarshal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		boundary DataBoundary
		want     string
		omitted  bool
	}{
		{name: "Success: global marshals to the global string", boundary: DBGlobal, want: `"dataBoundary":"global"`},
		{name: "Success: eu marshals to the eu string", boundary: DBEU, want: `"dataBoundary":"eu"`},
		{name: "Success: unknown is omitted entirely", boundary: DBUnknown, omitted: true},
	}

	for _, test := range tests {
		b, err := json.Marshal(Data{DataBoundary: test.boundary})
		if err != nil {
			t.Errorf("TestDataBoundaryMarshal(%s): got err == %s, want err == nil", test.name, err)
			continue
		}

		got := string(b)
		if test.omitted {
			if strings.Contains(got, "dataBoundary") {
				t.Errorf("TestDataBoundaryMarshal(%s): got %s, want no dataBoundary field", test.name, got)
			}
			continue
		}
		if !strings.Contains(got, test.want) {
			t.Errorf("TestDataBoundaryMarshal(%s): got %s, want it to contain %s", test.name, got, test.want)
		}
	}
}

// TestStatusMarshal pins the producer rule that an unset StatusCode serializes as StatusCode.
// This is what puts statusCode on the wire; nothing writes the field into the caller's resources.
func TestStatusMarshal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status Status
		want   string
	}{
		{name: "Success: empty defaults to OK", status: "", want: `"statusCode":"OK"`},
		{name: "Success: OK is preserved", status: StatusCode, want: `"statusCode":"OK"`},
		{name: "Success: an explicit value is preserved", status: "BadRequest", want: `"statusCode":"BadRequest"`},
	}

	// Both encoders are exercised: the SDK marshals with jsonv2 (which prefers MarshalJSONTo), while
	// the MarshalJSON shim exists solely for callers using the standard library. They must agree.
	marshalers := []struct {
		name string
		fn   func(any) ([]byte, error)
	}{
		{name: "jsonv2", fn: func(v any) ([]byte, error) { return json.Marshal(v) }},
		{name: "encoding/json", fn: stdjson.Marshal},
	}

	for _, test := range tests {
		for _, m := range marshalers {
			b, err := m.fn(NotificationResource{StatusCode: test.status})
			if err != nil {
				t.Errorf("TestStatusMarshal(%s/%s): got err == %s, want err == nil", test.name, m.name, err)
				continue
			}
			if !strings.Contains(string(b), test.want) {
				t.Errorf("TestStatusMarshal(%s/%s): got %s, want it to contain %s", test.name, m.name, b, test.want)
			}
		}
	}
}

// TestStatusMarshalEscapes pins that a Status needing JSON escaping is escaped rather than concatenated
// into the output -- hand-rolling the quoting silently turned a backslash into a backspace. Each encoder
// gets its own expectation: encoding/json HTML-escapes by default and jsontext does not, so asserting
// that the two agree would be asserting a contract the code does not hold.
func TestStatusMarshalEscapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status Status
		wantV1 string
		wantV2 string
	}{
		{
			name:   "Success: a double quote is escaped",
			status: `a"b`,
			wantV1: `"statusCode":"a\"b"`,
			wantV2: `"statusCode":"a\"b"`,
		},
		{
			name:   "Success: a backslash is escaped",
			status: `a\b`,
			wantV1: `"statusCode":"a\\b"`,
			wantV2: `"statusCode":"a\\b"`,
		},
		{
			name:   "Success: a newline is escaped",
			status: "a\nb",
			wantV1: `"statusCode":"a\nb"`,
			wantV2: `"statusCode":"a\nb"`,
		},
		{
			name:   "Success: encoding/json HTML-escapes where jsonv2 does not",
			status: "a<b",
			wantV1: `"statusCode":"a\u003cb"`,
			wantV2: `"statusCode":"a<b"`,
		},
	}

	for _, test := range tests {
		v1, err := stdjson.Marshal(NotificationResource{StatusCode: test.status})
		if err != nil {
			t.Errorf("TestStatusMarshalEscapes(%s): encoding/json: got err == %s, want err == nil", test.name, err)
			continue
		}
		if !strings.Contains(string(v1), test.wantV1) {
			t.Errorf("TestStatusMarshalEscapes(%s): encoding/json: got %s, want it to contain %s", test.name, v1, test.wantV1)
		}

		v2, err := json.Marshal(NotificationResource{StatusCode: test.status})
		if err != nil {
			t.Errorf("TestStatusMarshalEscapes(%s): jsonv2: got err == %s, want err == nil", test.name, err)
			continue
		}
		if !strings.Contains(string(v2), test.wantV2) {
			t.Errorf("TestStatusMarshalEscapes(%s): jsonv2: got %s, want it to contain %s", test.name, v2, test.wantV2)
		}
	}
}

// TestMarshalDoesNotMutate proves the defaulting is confined to serialization: marshaling a
// resource must not write StatusCode back into the value the caller still holds.
func TestMarshalDoesNotMutate(t *testing.T) {
	t.Parallel()

	rs := []NotificationResource{{ResourceID: "a"}, {ResourceID: "b"}}
	if _, err := json.Marshal(rs); err != nil {
		t.Fatalf("TestMarshalDoesNotMutate: got err == %s, want err == nil", err)
	}
	for i, r := range rs {
		if r.StatusCode != "" {
			t.Errorf("TestMarshalDoesNotMutate: rs[%d].StatusCode: got %q, want %q", i, r.StatusCode, "")
		}
	}
}
