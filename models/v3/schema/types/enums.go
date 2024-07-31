package types

// This file contains the enums used by the various types in schema 3.0.
// The enums have been converted to uint8 to save space in memory and allow
// faster validation. The enums are marshaled to JSON as strings using MarshalJSON()
// methods. These use unsafe to prevent allocations.

import (
	"unsafe"

	"github.com/go-json-experiment/json"
)

//go:generate stringer -type=ResourcesContainer -linecomment

// ResourcesContainer details if the resources are inline or stored in a blob.
type ResourcesContainer uint8

const (
	// RCUnknown is the default value, which tells the server to use inline.
	RCUnknown ResourcesContainer = 0 // ""
	// RCInline is the value to use when the resources are inline.
	RCInline ResourcesContainer = 1 // "inline"
	// RCBlob is the value to use when the resources are stored in a blob.
	RCBlob ResourcesContainer = 2 // "blob"
)

// MarshalJSON marshals the value to its JSON string. This uses an
// unsafe conversion to avoid allocations. Do not change the []byte that is returned.
func (r ResourcesContainer) MarshalJSON() ([]byte, error) {
	s := r.String()
	return unsafe.Slice(unsafe.StringData(s), len(s)), nil
}

// Runtime check on startup to ensure that the enums can be marshaled to JSON.
// This can break if the line comment for the enum is incorrect.
func init() {
	for i := 0; i < len(_ResourcesContainer_index)-1; i++ {
		v := ResourcesContainer(i)
		_, err := json.Marshal(struct{ ResourcesContainer ResourcesContainer }{ResourcesContainer: v})
		if err != nil {
			panic(err)
		}
	}
}

//go:generate stringer -type=Activity -linecomment

// Activity is the type of activity that is being performed on ARN (not the
// resource itself).
type Activity uint8

const (
	// ActUnknown indicates that the activity was not provided. This is a bug.
	ActUnknown Activity = 0 //
	// ActWrite indicates that the resource is being created or updated.
	// in ARN.
	ActWrite Activity = 1 // write
	// ActDelete indicates that the resource is being deleted in ARN.
	ActDelete Activity = 2 // delete
	// ActSnapshot indicates that the resource is being snapshotted. A snapshot
	// is when no update has been seen, but we want to ensure that the
	// resource is still present.
	ActSnapshot Activity = 3 // snapshot
)

// MarshalJSON marshals the value to its JSON string. This uses an
// unsafe conversion to avoid allocations. Do not change the []byte that is returned.
func (a Activity) MarshalJSON() ([]byte, error) {
	s := a.String()
	b := make([]byte, len(s)+2)
	b[0] = '"'
	copy(b[1:], s)
	b[len(b)-1] = '"'
	return b, nil
}

// Runtime check on startup to ensure that the enums can be marshaled to JSON.
// This can break if the line comment for the enum is incorrect.
func init() {
	for i := 0; i < len(_Activity_index)-1; i++ {
		v := Activity(i)
		_, err := json.Marshal(struct{ Activity Activity }{Activity: v})
		if err != nil {
			panic(err)
		}
	}
}

//go:generate stringer -type=ChangeAction -linecomment

// ChangeAction is the type of change that is being performed on a resource,
// not on ARN (which is what is indicated by Activity).
type ChangeAction uint8

const (
	// CAUknownn indicates that the change action was not provided. This
	// is a bug.
	CAUnknown ChangeAction = 0 // ""
	// CACreate indicates that the resource is being created at source.
	CACreate ChangeAction = 1 // "Create"
	// CADelete indicates that the resource is being deleted at source.
	CADelete ChangeAction = 2 // "Delete"
	// CAMove indicates that the resource is being moved at source.
	CAMove ChangeAction = 3 // "Move"
	// CAUpdate indicates that the resource is being updated at source.
	CAUpdate ChangeAction = 4 // "Update"
)

// MarshalJSON marshals the value to its JSON string. This uses an
// unsafe conversion to avoid allocations. Do not change the []byte that is returned.
func (c ChangeAction) MarshalJSON() ([]byte, error) {
	s := c.String()
	return unsafe.Slice(unsafe.StringData(s), len(s)), nil
}

// Runtime check on startup to ensure that the enums can be marshaled to JSON.
// This can break if the line comment for the enum is incorrect.
func init() {
	for i := 0; i < len(_ChangeAction_index)-1; i++ {
		v := ChangeAction(i)
		_, err := json.Marshal(struct{ ChangeAction ChangeAction }{ChangeAction: v})
		if err != nil {
			panic(err)
		}
	}
}

//go:generate stringer -type=DataBoundary -linecomment

// DataBoundary is the boundary of the data that is being processed.
type DataBoundary uint8

const (
	DBUnknown DataBoundary = 0 // ""
	// DataBoundary is the whole world.
	DBGlobal DataBoundary = 1 // "global"
	// DataBoundary is select locations in the EU.
	DBEU DataBoundary = 2 // "eu"
)

// MarshalJSON marshals the value to its JSON string. This uses an
// unsafe conversion to avoid allocations. Do not change the []byte that is returned.
func (d DataBoundary) MarshalJSON() ([]byte, error) {
	s := d.String()
	return unsafe.Slice(unsafe.StringData(s), len(s)), nil
}

// Runtime check on startup to ensure that the enums can be marshaled to JSON.
// This can break if the line comment for the enum is incorrect.
func init() {
	for i := 0; i < len(_DataBoundary_index)-1; i++ {
		v := DataBoundary(i)
		_, err := json.Marshal(struct{ DataBoundary DataBoundary }{DataBoundary: v})
		if err != nil {
			panic(err)
		}
	}
}
