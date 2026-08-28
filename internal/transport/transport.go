// Package transport contains the SDK's bounded and non-mutating HTTP helpers.
package transport

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"time"
)

const UserAgent = "IPGW-Meta/1 (+https://github.com/UnbalancedCat/ipgw-meta)"

var (
	ErrResponseTooLarge    = errors.New("response exceeds configured limit")
	errInvalidRequest      = errors.New("transport request is incomplete")
	errMissingTransport    = errors.New("transport is not configured")
	errInvalidResponseBody = errors.New("response body is not configured")
	errInvalidLimit        = errors.New("response limit must be positive")
)

type userAgentRoundTripper struct{ next http.RoundTripper }

func (t userAgentRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil || request.URL == nil {
		return nil, errInvalidRequest
	}
	if t.next == nil {
		return nil, errMissingTransport
	}
	clone := request.Clone(request.Context())
	clone.Header = request.Header.Clone()
	if clone.Header == nil {
		clone.Header = make(http.Header)
	}
	if clone.Header.Get("User-Agent") == "" {
		clone.Header.Set("User-Agent", UserAgent)
	}
	return t.next.RoundTrip(clone)
}

func Wrap(roundTripper http.RoundTripper) http.RoundTripper {
	if roundTripper == nil {
		roundTripper = http.DefaultTransport
	}
	return userAgentRoundTripper{next: roundTripper}
}

func Default(bindIP netip.Addr) *http.Transport {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	if bindIP.IsValid() {
		dialer.LocalAddr = &net.TCPAddr{IP: net.IP(bindIP.AsSlice())}
	}
	return &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, _ string, address string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp4", address)
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
}

func ReadAll(body io.Reader, limit int64) ([]byte, error) {
	if body == nil {
		return nil, errInvalidResponseBody
	}
	if limit < 1 {
		return nil, errInvalidLimit
	}
	limited := io.LimitReader(body, limit+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, ErrResponseTooLarge
	}
	return data, nil
}
