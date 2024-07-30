// Package version provides the schema version of the SDK.
package version

// Schema is the schema version of the API.
// The Go SDK only supports from schema version 3.
type Schema string

const (
	// V3 is the schema version 3.
	V3 Schema = "3.0"
)

// API is the API version of the SDK.
type API string

// API2020 is the API version 2020-05-01.
const API2020 API = "2020-05-01"
