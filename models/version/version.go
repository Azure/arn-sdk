// Package version provides the schema version of the SDK.
package version

import (
	"fmt"
)

// Schema is the schema version of the API.
// The Go SDK only supports from schema version 3.
type Schema string

const (
	// V3 is the schema version 3.
	V3 Schema = "3.0"
)

// SDK contains the version information of the SDK.
var SDK SDKVersion = SDKVersion{
	Version: "0.1.0",
}

// SDKVersion describes the version information of the API.
type SDKVersion struct {
	// Version contains the version of the API, aka v1.2.3 .
	Version string
}

func (s SDKVersion) String() string {
	return "v" + s.Version
}

// AsARNFormat returns the API version in a format that ARN understands.
func (s SDKVersion) AsARNFormat() string {
	if s.Version != "" {
		return fmt.Sprintf("golang@%s", s.Version)
	}
	return "golang@unknown"
}
