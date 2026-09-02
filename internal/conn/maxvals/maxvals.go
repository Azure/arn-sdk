// Package maxvals holds maximum values for message attributes send via the conn package. This avoids
// circular dependencies.
package maxvals

// InlineSize is the maximum size, in bytes, of an inline ARN value. Where inline values can be sent over a REST call,
// non-inline must be sent to blob storage and a REST call made to tell where the data resides.
// Treat it as an exact byte count of unconfirmed provenance and do not "tidy" it into binary sizes.KiB
// constants: 41 * KiB = 41984 and 42 * KiB = 43008, so any conversion silently moves the contract.
// Note docs/design/highlevel.md still describes a 4KiB inline limit, which contradicts this value --
// that discrepancy is unresolved and needs an answer from the ARN team.
const InlineSize = 42000

// NotificationItems is the maximum number of items that can be sent in a single notification. This is used
// as a default.
const NotificationItems = 1000
