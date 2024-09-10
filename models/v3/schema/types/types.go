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
	"reflect"
	"time"
	"unique"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
)

const (
	// StatusCode is the HTTP status code of the operation. As a producer, this is always "OK".
	StatusCode = "OK"
)

// Data represents the data of the event. There are two ways to send the data:
// 1. Inline: The resources being are included in the Resources field.
// 2. Blob: The resources are stored in a blob and the information about the blob is included in ResourcesBlobInfo.
// The ResourcesContainer field is used to determine if the resources are inline or in a blob.
// Any field with omitzero tag is optional, however depending on the ResourcesContainer field, some fields might be required.
// Field-aligned.
type Data struct {
	// Data is where the serialized resources are stored. Do not populate this as it will be erased.
	// This is a JSON serialized version of the Resources field.
	Data json.RawMessage `json:"resources"`
	// AdditionalBatchProperties can contain the sdkversion, batchsize, subscription partition tag etc.
	AdditionalBatchProperties map[string]any `json:"additionalBatchProperties,omitzero"`
	// ResourcesBlobInfo is the information about the storage blob used to store the payload of resources included in this notification.
	// Populated only when a blob is used, in which case ResourcesContainer is set to Blob.
	ResourcesBlobInfo ResourcesBlobInfo `json:"resourcesBlobInfo,omitzero"`
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
	// APIVersion is the APIVersion in the format of "yyyy-MM-dd" follwed by an optional string like "-preview", "-privatepreview", etc.
	// This is optional, however, if not set here, must be set on
	APIVersion string `json:"apiVersion,omitzero"`
	// Resources is required for inline payload, only null if payload is in blob. While it
	// is not directly emitted as JSON, we serialize this and store it in the Data field.
	Resources []NotificationResource `json:"-"`
	// ResourcesContainer details if the resources are inline or in a blob.
	// This is either RCInline or RCBlob.
	ResourcesContainer ResourcesContainer `json:"resourcesContainer,omitzero"`
	// DataBoundary is the boundary for the resources included in the notification.
	DataBoundary string `json:"dataBoundary,omitzero"`
}

// Validate validates the data.
// TODO: Add more validation for omitzero fields when they are set.
// TODO: Need to write some tests for this.
func (d Data) Validate() error {
	if d.ResourcesContainer == 0 || d.ResourcesContainer >= ResourcesContainer(len(_ResourcesContainer_index)-1) {
		return fmt.Errorf(".ChangedAction(%d) is invalid", d.ResourcesContainer)
	}

	switch d.ResourcesContainer {
	case RCBlob:
		// We don't validate the ResourceBlobInfo here, because this gets called before
		// we upload the blob and get back the URL and size.
	case RCInline:
		if len(d.Resources) == 0 {
			return errors.New(".Resources is required when ResourcesContainer is Inline")
		}

		rscAPIVersion := ""
		var rscType reflect.Type
		for i, r := range d.Resources {
			if err := r.Validate(); err != nil {
				return fmt.Errorf(".Resources[%d]%w", i, err)
			}

			// All ARMResource.Properties must be of the same type. This either gets the type on the
			// first iteration or validates that the type is the same on subsequent iterations.
			// Also makes sure that APIVersion is set on all resources if it is not set on Data and that
			// it matches if it is set on Data.
			if i == 0 {
				rscAPIVersion = r.APIVersion
				rscType = reflect.TypeOf(r.ArmResource.Properties)
			} else {
				if unique.Make(rscType) != unique.Make(reflect.TypeOf(r)) {
					return errors.New("all NotificationResource.ARMResource.Properties must be of the same type")
				}
			}

			// If APIVersion is not set, it must be set on all resources.
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

			if rscAPIVersion != r.APIVersion {
				return errors.New("all resources must have the same APIVersion")
			}

			if rscAPIVersion != "" {
				if r.ArmResource.APIVersion != rscAPIVersion {
					return errors.New("all resources must have the same APIVersion and ArmResource.APIVersion must match")
				} else if r.ArmResource.APIVersion != r.APIVersion {
					return errors.New("all resources must have the same APIVersion and ArmResource.APIVersion must match")
				}
			}
		}
	}

	return nil
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
	if r.BlobSize == 0 {
		return errors.New(".ResourcesBlobInfo.BlobSize is required")
	}
	return nil
}

// NotificationResource is the resource payload.
// Note that HomeTenantID, ResourceHomeTenantID, APIVersion have been removed
// as they are just duplicates of the Data fields that are required to be the same.
// Field-aligned.
type NotificationResource struct {
	// ResourceEventTime is the time of the resource event.
	ResourceEventTime time.Time `json:"resourceEventTime,omitzero" format:"RFC3339"`
	// ArmResource is the ARM resource. This is where your specific resource data is stored.
	// While it says Arm, it can be other resource types.
	// For delete events all object properties other than id will be missing.
	ArmResource ArmResource `json:"armResource,omitzero"`
	// AdditionalResourceProperties is a dictionary of additional resource metadata.
	AdditionalResourceProperties map[string]string `json:"additionalResourceProperties,omitzero"`
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
	// StatusCode is the HTTP status code of the operation. As a producer, this is always "OK" set by StatusCode constant.
	// This is automatically set.
	StatusCode string `json:"statusCode,omitzero"` // "OK" or "BADRequest"
	// HomeTenantID is the tenant ID of the home tenant of the resource.
	// This is optional except for provider scoped resources. It can also be specified
	// at the Data level. If so, these should match.
	HomeTenantID string `json:"homeTenantId,omitzero"`
	// ResourceHomeTenantID is an optiona field with no definition.
	ResourceHomeTenantID string `json:"resourceHomeTenantId,omitzero"`
	// ResourceSystemProperties provides details about the change action, who created and modified the resource, and when.
	ResourceSystemProperties ResourceSystemProperties `json:"resourceSystemProperties,omitzero"`
	// OperationalInfo is operational information for this resource.
	OperationalInfo OperationalInfo `json:"operationalInfo,omitzero"`
}

// Validate validates the NotificationResource.
func (n NotificationResource) Validate() error {
	if n.ResourceID == "" {
		return errors.New(".ResourceID is required")
	}
	if n.StatusCode != StatusCode {
		return errors.New(".StatusCode is required as OK")
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

	arm *arm.ResourceID `json:"-"`
	act Activity        `json:"-"`
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
