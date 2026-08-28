// Package srun parses the untrusted Srun gateway status and business-response
// wire formats. It does not own HTTP state or public SDK types.
package srun

import (
	"errors"
	"net/netip"
)

// ErrUnrecognized reports a response that cannot be classified safely. It is
// intentionally content-free so raw gateway data cannot leak through errors.
var ErrUnrecognized = errors.New("unrecognized Srun response")

type Summary struct {
	TrafficBytes    int64
	DurationSeconds int64
	BalanceMinor    *int64
}

type Status struct {
	Online   bool
	Username string
	OnlineIP netip.Addr
	Summary  *Summary
}
