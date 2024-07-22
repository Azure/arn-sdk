// Package maxvals holds maximum values for message attributes send via the conn package. This avoids
// circular dependencies.
package maxvals

// InlineSize is the maximum size of an inline ARN value. Where inline values can be sent over a REST call,
// non-line must be sent to blob storage and a REST call made to tell where the data resides.
const InlineSize = 42000

// NotificationItems is the maximum number of items that can be sent in a single notification. This is used
// as a default.
const NotificationItems = 1000
