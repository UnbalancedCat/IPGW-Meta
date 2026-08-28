package ipgw

import (
	"fmt"
	"net/http"
	"net/netip"
)

type Option func(*clientOptions) error

type clientOptions struct {
	bindIP       netip.Addr
	roundTripper http.RoundTripper
	observer     Observer
	stateStore   ProtocolStateStore
}

func WithBindIP(ip netip.Addr) Option {
	return func(options *clientOptions) error {
		if !ip.IsValid() || !ip.Is4() || ip.IsUnspecified() {
			return fmt.Errorf("bind IP must be a concrete IPv4 address")
		}
		options.bindIP = ip
		return nil
	}
}

func WithRoundTripper(roundTripper http.RoundTripper) Option {
	return func(options *clientOptions) error {
		if isNilInterface(roundTripper) {
			return fmt.Errorf("round tripper must not be nil")
		}
		// An arbitrary RoundTripper is a fully trusted extension boundary: the
		// SDK cannot inspect whether it implements normal system PKI semantics.
		options.roundTripper = roundTripper
		return nil
	}
}

func WithObserver(observer Observer) Option {
	return func(options *clientOptions) error {
		if isNilInterface(observer) {
			return fmt.Errorf("observer must not be nil")
		}
		options.observer = observer
		return nil
	}
}

func WithProtocolStateStore(store ProtocolStateStore) Option {
	return func(options *clientOptions) error {
		if isNilInterface(store) {
			return fmt.Errorf("protocol state store must not be nil")
		}
		options.stateStore = store
		return nil
	}
}
