/*
Package types provides the types used in the ARN service on the wire.

The basics are that you send an Event to the ARN service. That Event may carry the data inline or detail
where to find the data in blob storage.

Any field with omitzero tag is optional, however depending other fields being set might be required.

EventMeta is the metadata of the event. This is inlined during Marshaling.

ArmResource is where you store the resource data from your service. You may need to have an
agreed on schema with the ARN service. This object must serialize out a field called "id" that
is the resource ID. During delete events, all object properties other than id will be missing.
*/
package types

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	jsonv2 "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
)

const (
	// StatusCode is the HTTP status code of the operation. As a producer, this is always "OK".
	StatusCode = "OK"
)

// Data represents the data of the event. THIS IS NOT USED DIRECTLY, BUT INSTEAD IS CREATED BY msgs.Notification.
// THIS IS PUBLIC TO ALLOW FOR MARSHALING. NOT ALL FIELDS ARE CURRENTLY EXPOSED.
// There are two ways to send the data:
// 1. Inline: The resources being are included in the Resources field.
// 2. Blob: The resources are stored in a blob and the information about the blob is included in ResourcesBlobInfo.
// The ResourcesContainer field is used to determine if the resources are inline or in a blob.
// Any field with omitzero tag is optional, however depending on the ResourcesContainer field, some fields might be required.
// Field-aligned.
type Data struct {
	// AdditionalBatchProperties can contain the sdkversion, batchsize, subscription partition tag etc.
	AdditionalBatchProperties AdditionalBatchProperties `json:"additionalBatchProperties"`
	// HomeTenantID is the Tenant ID of the tenant from which the resources in this notification are managed.
	HomeTenantID string `json:"homeTenantId,omitzero"`
	// ResourceHomeTenantID is the Tenant ID of the tenant in which the resources in this notification are located.
	ResourceHomeTenantID string `json:"resourceHomeTenantId,omitzero"`
	// ResourceLocation is the location of the resources in this notification. This is the normalized ARM location enum
	// like "eastus".
	ResourceLocation string `json:"resourceLocation"`
	// FrontdoorLocation is the ARM region that emitted the notification. Omitted for notifications not emitted by ARM.
	FrontdoorLocation string `json:"frontdoorLocation,omitzero"`
	// PublisherInfo is the Namespace of the publisher sending the data of this notification, for example Microsoft.Resources is be the publisherInfo for ARM.
	PublisherInfo string `json:"publisherInfo"`
	// Sign is set by ARN, do not populate as a publisher.
	Sign string `json:"-"`
	// RoutingType is set by ARN, do not populate as a publisher.
	RoutingType string `json:"-"`
	// APIVersion is the APIVersion in the format of "yyyy-MM-dd" followed by an optional suffix
	// such as "-preview" or "-privatepreview".
	// This is optional, however if it is not set here it must be set on every NotificationResource in Resources.
	// If it is set here, each NotificationResource must either match it or leave its own APIVersion empty.
	APIVersion string `json:"apiVersion,omitzero"`
	// DataBoundary is the boundary for the resources included in the notification.
	// Optional. DBUnknown omits the field from the wire format.
	DataBoundary DataBoundary `json:"dataBoundary,omitzero"`
	// Data is where the serialized resources are stored, as a JSON serialization of the Resources field.
	// Do not populate this: the SDK sets it, and Validate() rejects a caller-set value on the blob path.
	Data json.RawMessage `json:"resources"`
	// ResourcesBlobInfo is the information about the storage blob used to store the payload of resources included in this notification.
	// Populated only when a blob is used, in which case ResourcesContainer is set to Blob.
	ResourcesBlobInfo ResourcesBlobInfo `json:"resourcesBlobInfo,omitzero"`
	// Resources is required for inline payload, only null if payload is in blob. While it
	// is not directly emitted as JSON, we serialize this and store it in the Data field.
	Resources []NotificationResource `json:"-"`
	// ResourcesContainer details if the resources are inline or in a blob.
	// This is either RCInline or RCBlob.
	ResourcesContainer ResourcesContainer `json:"resourcesContainer,omitzero"`
}

// Validate validates the data.
// TODO: Add more validation for omitzero fields when they are set.
// TODO: Need to write some tests for this.
func (d Data) Validate() error {
	if d.ResourcesContainer == 0 || d.ResourcesContainer >= ResourcesContainer(len(_ResourcesContainer_index)-1) {
		return fmt.Errorf(".ResourcesContainer(%d) is invalid", d.ResourcesContainer)
	}

	// DataBoundary is optional, so DBUnknown is allowed. Anything past the last defined
	// constant is a bug in the caller.
	if d.DataBoundary >= DataBoundary(len(_DataBoundary_index)-1) {
		return fmt.Errorf(".DataBoundary(%d) is invalid", d.DataBoundary)
	}

	// The arms stay separate because the container decides which payload is legal, and that invariant
	// has nowhere else to live. The per-resource loop below is deliberately outside the switch: how the
	// resources travel says nothing about whether they are well formed, and skipping it for blob let
	// invalid resources ship.
	switch d.ResourcesContainer {
	case RCInline:
		if len(d.Resources) == 0 {
			return errors.New(".Resources is required when ResourcesContainer is Inline")
		}
		if d.ResourcesBlobInfo != (ResourcesBlobInfo{}) {
			return errors.New(".ResourcesBlobInfo must not be set when ResourcesContainer is Inline")
		}
	case RCBlob:
		if len(d.Resources) == 0 {
			return errors.New(".Resources is required when ResourcesContainer is Blob")
		}
		// Data is the field that actually serializes (as "resources"); Resources is json:"-" and is the
		// in-memory source for both containers. Guarding Data is what keeps the wire payload and the
		// container tag consistent.
		if len(d.Data) != 0 {
			return errors.New(".Data must not be set when ResourcesContainer is Blob")
		}
		// ResourcesBlobInfo is not checked here: msgs uploads the blob after this runs and validates
		// the URI and size it gets back at that point.
	default:
		// The guard above only bounds the value against the generated index, which widens on its own
		// whenever a constant is added. This bounds the dispatch, so a new container cannot slip
		// through unvalidated.
		return fmt.Errorf("bug: .ResourcesContainer(%d) is defined but Data.Validate() has no case", d.ResourcesContainer)
	}

	rscAPIVersion := ""
	var rscType [2]string

	for i, r := range d.Resources {
		if err := r.Validate(); err != nil {
			return fmt.Errorf(".Resources[%d]%w", i, err)
		}

		if r.ArmResource.arm == nil {
			return fmt.Errorf(".Resources[%d].ArmResource is empty or was not created with NewArmResource()", i)
		}

		// All ARMResource.Properties must be of the same type. This either gets the type on the
		// first iteration or validates that the type is the same on subsequent iterations.
		if i == 0 {
			rscType[0] = r.ArmResource.arm.ResourceType.Namespace
			rscType[1] = r.ArmResource.arm.ResourceType.Type
		} else {
			compare := [2]string{r.ArmResource.arm.ResourceType.Namespace, r.ArmResource.arm.ResourceType.Type}
			if rscType != compare {
				return errors.New("all NotificationResource.ArmResource.Properties must be of the same type")
			}
		}

		// If APIVersion is not set on Data, it must be set on all resources.
		if d.APIVersion == "" {
			if r.APIVersion == "" {
				return errors.New("NotificationResource.APIVersion is required when not set on Data")
			}
		} else {
			// If it is set on Data, it must match on all resources or they must be empty.
			if d.APIVersion != r.APIVersion && r.APIVersion != "" {
				return errors.New("NotificationResource.APIVersion must match Data.APIVersion if set")
			}
		}

		// Tenant IDs must be identical at both levels. This is stricter than the ARN V3 spec's
		// "if present at both levels, the values should be the same": empty is compared as a value,
		// so a resource may not leave a tenant ID unset when Data sets it, or vice versa.
		if d.HomeTenantID != r.HomeTenantID {
			return fmt.Errorf(".Resources[%d].HomeTenantID %q must match Data.HomeTenantID %q", i, r.HomeTenantID, d.HomeTenantID)
		}

		if d.ResourceHomeTenantID != r.ResourceHomeTenantID {
			got, want := r.ResourceHomeTenantID, d.ResourceHomeTenantID
			return fmt.Errorf(".Resources[%d].ResourceHomeTenantID %q must match Data's %q", i, got, want)
		}

		// Note: the two checks above already require every resource's tenant IDs to equal Data's,
		// which transitively requires all resources to agree with each other.

		// The effective version is the resource's own when set, otherwise Data's. It is never empty here:
		// the branch above already errors when both are unset. Everything below compares effective values,
		// so a resource may match Data's version or defer to it by leaving its own empty -- which is what
		// the field docs promise. Comparing raw values rejected that mix.
		effAPIVersion := r.APIVersion
		if effAPIVersion == "" {
			effAPIVersion = d.APIVersion
		}
		if i == 0 {
			rscAPIVersion = effAPIVersion
		}
		if rscAPIVersion != effAPIVersion {
			return errors.New("all resources must resolve to the same APIVersion")
		}

		// An empty ArmResource.APIVersion inherits the effective version rather than conflicting with it,
		// which keeps NewArmResource(..., "", ...) valid instead of failing later at send time.
		armVer := r.ArmResource.APIVersion
		if armVer != "" && armVer != effAPIVersion {
			return fmt.Errorf(".Resources[%d].ArmResource.APIVersion %q != effective %q", i, armVer, effAPIVersion)
		}
	}

	return nil
}

// AdditionalBatchProperties is the additional properties that can be set on a batch of notifications.
type AdditionalBatchProperties struct {
	// Others is a map of additional properties that are provided by the user. These should not
	// include keys that are already defined in the struct. These entries are inlined into the
	// object when serialized.
	Others map[string]any `json:",inline"`
	// BatchCorrelationID is a unique identifier for the batch of notifications. This can be used by
	// ARN or ARG in traces. This is a GUID. Optional.
	BatchCorrelationID string `json:"batchCorrelationId"`
	// SDKVersion is the version of the SDK that is sending the notification.
	// This is automaticallly set by the SDK.
	SDKVersion string `json:"sdkVersion"`
	// BatchSize is the number of resources in the batch. These may be inline or in a blob. This is
	// automatically set by the SDK.
	BatchSize uint16 `json:"batchSize"`
}

// ResourcesBlobInfo is the information about the storage blob used to store the payload of resources
// included in this notification.
type ResourcesBlobInfo struct {
	// BlobURI is the the Blob uri with SAS (shared access signature) for the reader to
	// be able to have access to download the data and parse into NotificationResourceData objects.
	BlobURI string `json:"blobUri"`
	// BlobSize is the size in bytes of the blob payload content.
	BlobSize int64 `json:"blobSize"`
}

// Validate validates the ResourcesBlobInfo.
func (r *ResourcesBlobInfo) Validate() error {
	if r.BlobURI == "" {
		return errors.New(".ResourcesBlobInfo.BlobURI is required")
	}

	switch {
	case r.BlobSize == 0:
		return errors.New(".ResourcesBlobInfo.BlobSize is required")
	case r.BlobSize < 0:
		return fmt.Errorf(".ResourcesBlobInfo.BlobSize(%d) must be positive", r.BlobSize)
	}
	return nil
}

// Status is the status of the operation on a resource as reported to ARN. As a producer the
// SDK always reports StatusCode ("OK"), so the zero value serializes as StatusCode rather than
// being omitted and callers never need to set this.
type Status string

// Compile-time checks. MarshalJSONTo is the only thing that puts statusCode on the wire, so if a
// dependency bump changed either interface the field would silently stop being emitted.
var (
	_ jsonv2.MarshalerTo = Status("")
	_ jsonv2.Marshaler   = Status("")
)

// MarshalJSONTo implements jsonv2.MarshalerTo. An empty Status is written as "OK". Doing the
// defaulting at serialization time means the wire format is correct no matter when the value is
// marshaled, and nothing has to write back into the caller's resource slice to achieve it.
func (s Status) MarshalJSONTo(enc *jsontext.Encoder) error {
	return enc.WriteToken(jsontext.String(string(s.orDefault())))
}

// MarshalJSON implements jsonv2.Marshaler so the default also applies under encoding/json. The SDK
// itself marshals with jsonv2, which prefers MarshalJSONTo, but NotificationResource is a public
// type and a caller using the standard library would otherwise emit an empty statusCode that ARN
// rejects. Every enum in enums.go carries the same v1 method for the same reason.
func (s Status) MarshalJSON() ([]byte, error) {
	// Delegate the encoding rather than concatenating quotes: a Status carrying a quote, backslash or
	// control byte must be escaped, and hand-rolling that silently corrupted the value.
	return json.Marshal(string(s.orDefault()))
}

// orDefault applies the producer rule that an unset Status means "OK".
func (s Status) orDefault() Status {
	if s == "" {
		return StatusCode
	}
	return s
}

// NotificationResource is the resource payload.
// Field-aligned.
type NotificationResource struct {
	// OperationalInfo is operational information for this resource.
	OperationalInfo OperationalInfo `json:"operationalInfo,omitzero"`
	// ResourceEventTime is the time of the resource event.
	ResourceEventTime time.Time `json:"resourceEventTime,omitzero" format:"RFC3339"`
	// AdditionalResourceProperties is a dictionary of additional resource metadata.
	AdditionalResourceProperties map[string]string `json:"additionalResourceProperties,omitzero"`
	// ArmResource is the ARM resource. This is where your specific resource data is stored.
	// While it says Arm, it can be other resource types.
	// For delete events all object properties other than id will be missing.
	ArmResource ArmResource `json:"armResource,omitzero"`
	// ResourceID is the ARM resource ID.
	// This is in the form of "="/subscriptions/{subId}/resourceGroups/{rgName}/providers/{providerNamespace}/{resourceType}/{resourceName}".
	ResourceID string `json:"resourceId"`
	// APIVersion is the version of the resource schema used to encode the resource payload in armResource.
	// APIVersion in the format of "yyyy-MM-dd" follwed by an optional string like "-preview", "-privatepreview", etc.
	// When not specified here it must be specified in NotificationDataV3.
	APIVersion string `json:"apiVersion,omitzero"`
	// SourceResourceID has the resource ID of the source resource for the move event.
	SourceResourceID string `json:"sourceResourceId,omitzero"`
	// CorrelationID is the correlation identifier associated with the operation that resulted in the activity
	// reflected in the notification. This is normally a GUID.
	CorrelationID string `json:"correlationId,omitzero"`
	// StatusCode is the HTTP status code of the operation. As a producer this is always "OK".
	// Leave it unset: the zero value serializes as the StatusCode constant. No omitzero here, or
	// the zero value would be dropped before Status.MarshalJSONTo could default it.
	StatusCode Status `json:"statusCode"`
	// HomeTenantID is the tenant ID of the home tenant of the resource.
	// This is optional except for provider scoped resources, but it must always be identical to
	// Data.HomeTenantID: set it on both or neither, never on only one.
	HomeTenantID string `json:"homeTenantId,omitzero"`
	// ResourceHomeTenantID is the tenant id in which the resources in this notification exist.
	// This is optional, but it must always be identical to Data.ResourceHomeTenantID: set it on
	// both or neither, never on only one.
	ResourceHomeTenantID string `json:"resourceHomeTenantId,omitzero"`
	// ResourceSystemProperties provides details about the change action, who created and modified the resource, and when.
	ResourceSystemProperties ResourceSystemProperties `json:"resourceSystemProperties,omitzero"`
}

// Validate validates the NotificationResource.
func (n NotificationResource) Validate() error {
	if n.ResourceID == "" {
		return errors.New(".ResourceID is required")
	}
	// Empty is legal and means "serialize as StatusCode". As a producer we never report
	// anything else, so any other value is a caller bug. See Status.MarshalJSONTo.
	if n.StatusCode != "" && n.StatusCode != StatusCode {
		return fmt.Errorf(".StatusCode must be empty or %q, got %q", StatusCode, n.StatusCode)
	}

	if n.ArmResource != (ArmResource{}) {
		if err := n.ArmResource.Validate(); err != nil {
			return fmt.Errorf(".ArmResource: %w", err)
		}
	}

	if err := n.ResourceSystemProperties.Validate(); err != nil {
		return fmt.Errorf(".ResourceSystemProperties: %w", err)
	}

	return nil
}

// ArmResource is the generic resource (even though it is named ArmResource).
// In the case of delete events, all object properties other than ID and Location will be missing.
// Properties is where you store your custom resource data that describes the resource
// in the format agreed to with the ARN service.
// Use NewArmResource to create a new ArmResource.
// Field-aligned.
type ArmResource struct {
	// Properties is the properties of the resource. This must serialize to a JSON dictionary that
	// stores the properties of the resource. This is where your specific resource data is stored.
	// This can be nil if the Activity that is being performed is a delete.
	// If we were storing AKS node data for this event, this would be the node data.
	Properties any `json:"properties,omitzero"`

	arm *arm.ResourceID `json:"-"`
	// Name is the name of the resource. This is the last segment of the resource ID.
	Name string `json:"name,omitzero"`
	// Type
	Type string `json:"type,omitzero"`
	// ID is the resource ID.
	ID string `json:"id"`
	// Location is the location of the resource, like "eastus".
	Location string `json:"location,omitzero"`
	// APIVersion is the API version of the resource data schema. This is in the format of "yyyy-MM-dd"
	// followed by an optional string like "-preview", "-privatepreview", etc.
	APIVersion string `json:"apiVersion,omitzero"`

	act Activity `json:"-"`
}

// NewArmResource creates a new ArmResource. act is the activity that is being performed on the resource.
// id is the resource ID. apiVer is the API version of the resource data schema. props is the properties of the resource.
// See ArmResource for more details.
func NewArmResource(act Activity, id *arm.ResourceID, apiVersion string, props any) (ArmResource, error) {
	if id == nil {
		return ArmResource{}, errors.New("resourceID is required")
	}

	r := ArmResource{
		ID:         id.String(),
		Name:       id.Name,
		Type:       id.ResourceType.String(),
		Location:   id.Location,
		APIVersion: apiVersion,
		Properties: props,

		arm: id,
		act: act,
	}

	if err := r.Validate(); err != nil {
		return ArmResource{}, err
	}
	return r, nil
}

// ResourceID returns an arm.ResourceID object representing the resource.
func (a ArmResource) ResourceID() *arm.ResourceID {
	return a.arm
}

// Activity returns the activity that is being performed on the resource.
func (a ArmResource) Activity() Activity {
	return a.act
}

// Validate validates the ArmResource. act is the activity that is being performed on the resource.
func (a ArmResource) Validate() error {
	if a.ID == "" {
		return errors.New(".ID is required")
	}

	switch a.act {
	case ActWrite, ActSnapshot:
		if a.Properties == nil {
			return errors.New(".Properties is required")
		}
	case ActDelete:
		return nil
	default:
		return fmt.Errorf("unknown activity %q", a.act)
	}

	return nil
}

// ResourceSystemProperties provides details about the change action, who created and modified the resource, and when.
// This is field-aligned.
type ResourceSystemProperties struct {
	// CreatedTime is the create time of the resource.
	CreatedTime time.Time `json:"createdTime,omitzero" format:"RFC3339"`
	// Modified time of the resource.
	ModifiedTime time.Time `json:"modifiedTime,omitzero" format:"RFC3339"`
	// CreatedBy is the entity that created this resource, can be object id, alias, display name etc.
	CreatedBy string `json:"createdBy"`
	// ModifiedBy is the entity that last modified this resource, can be object id, alias, display name etc.
	ModifiedBy string `json:"modifiedBy"`
	// ChangeAction is the type of event action for this resource event, currently supported ones are Create, Update, Delete, Move.
	ChangeAction ChangeAction `json:"changeAction"`
}

// Validate validates the ResourceSystemProperties.
func (r ResourceSystemProperties) Validate() error {
	if r.ChangeAction == 0 || r.ChangeAction >= ChangeAction(len(_ChangeAction_index)-1) {
		return fmt.Errorf(".ChangedAction(%d) is invalid", r.ChangeAction)
	}
	return nil
}

// OperationalInfo is operational information for this resource. It is defined in the schema
// but has no definition.
type OperationalInfo struct{}
