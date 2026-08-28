// Package dashboard reserves the internal boundary for post-v1 session,
// usage, billing, and recharge work.
//
// Version 1 intentionally provides no Dashboard adapter. Future work must map
// Dashboard responses into internal domain models before exposing stable SDK
// types; HTML and wire-response details must not cross this package boundary.
package dashboard
