package transport

import (
	"errors"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestWrapRejectsIncompleteRequestsWithoutLeakingURLData(t *testing.T) {
	wrapped := Wrap(roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("underlying transport must not receive an incomplete request")
		return nil, nil
	}))
	if _, err := wrapped.RoundTrip(nil); !errors.Is(err, errInvalidRequest) {
		t.Fatalf("nil request error = %v", err)
	}

	const canary = "TRANSPORT-QUERY-CANARY"
	request := &http.Request{Header: http.Header{"X-Source": []string{"https://example.invalid/path?token=" + canary}}}
	_, err := wrapped.RoundTrip(request)
	if !errors.Is(err, errInvalidRequest) {
		t.Fatalf("nil URL error = %v", err)
	}
	if strings.Contains(err.Error(), canary) || strings.Contains(err.Error(), "example.invalid") {
		t.Fatalf("invalid-request error leaked request data: %v", err)
	}
}

func TestWrapClonesRequestAndAddsUserAgentOnlyWhenMissing(t *testing.T) {
	tests := []struct {
		name     string
		header   http.Header
		wantUA   string
		original http.Header
	}{
		{name: "nil header", header: nil, wantUA: UserAgent, original: nil},
		{name: "missing user agent", header: http.Header{"X-Test": []string{"original"}}, wantUA: UserAgent, original: http.Header{"X-Test": []string{"original"}}},
		{name: "custom user agent", header: http.Header{"User-Agent": []string{"custom-agent"}, "X-Test": []string{"original"}}, wantUA: "custom-agent", original: http.Header{"User-Agent": []string{"custom-agent"}, "X-Test": []string{"original"}}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			target, err := url.Parse("https://example.invalid/path?opaque=query")
			if err != nil {
				t.Fatal(err)
			}
			request := &http.Request{Method: http.MethodGet, URL: target, Header: testCase.header}
			wrapped := Wrap(roundTripFunc(func(received *http.Request) (*http.Response, error) {
				if received == request {
					t.Fatal("underlying transport received the caller-owned request")
				}
				if received.Header.Get("User-Agent") != testCase.wantUA {
					t.Fatalf("User-Agent = %q", received.Header.Get("User-Agent"))
				}
				received.Header.Set("X-Test", "mutated")
				received.URL.RawQuery = "changed=true"
				return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: http.NoBody, Request: received}, nil
			}))
			if _, err := wrapped.RoundTrip(request); err != nil {
				t.Fatalf("RoundTrip() error = %v", err)
			}
			if request.URL.RawQuery != "opaque=query" {
				t.Fatalf("caller URL was modified: %q", request.URL.RawQuery)
			}
			if testCase.original == nil {
				if request.Header != nil {
					t.Fatalf("nil caller Header was replaced: %#v", request.Header)
				}
			} else if got := request.Header.Get("X-Test"); got != "original" {
				t.Fatalf("caller Header was modified: %q", got)
			}
			if testCase.name == "missing user agent" && request.Header.Get("User-Agent") != "" {
				t.Fatalf("User-Agent was added to caller Header: %#v", request.Header)
			}
		})
	}
}

func TestWrapAndDefaultConfiguration(t *testing.T) {
	wrapped, ok := Wrap(nil).(userAgentRoundTripper)
	if !ok || wrapped.next != http.DefaultTransport {
		t.Fatalf("Wrap(nil) = %#v", wrapped)
	}
	request, err := http.NewRequest(http.MethodGet, "https://example.invalid", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (userAgentRoundTripper{}).RoundTrip(request); !errors.Is(err, errMissingTransport) {
		t.Fatalf("missing transport error = %v", err)
	}

	configured := Default(netip.Addr{})
	if configured == nil || configured.Proxy != nil || configured.DialContext == nil {
		t.Fatalf("Default() configuration = %#v", configured)
	}
	if !configured.ForceAttemptHTTP2 || configured.TLSHandshakeTimeout <= 0 || configured.ResponseHeaderTimeout <= 0 || configured.IdleConnTimeout <= 0 {
		t.Fatalf("Default() timeout/HTTP2 configuration is incomplete")
	}
	if configured.TLSClientConfig != nil && configured.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("Default() disabled TLS certificate verification")
	}
}

func TestReadAllLimitsResponses(t *testing.T) {
	data, err := ReadAll(strings.NewReader("1234"), 4)
	if err != nil || string(data) != "1234" {
		t.Fatalf("exact-limit ReadAll() = %q, %v", data, err)
	}
	if _, err := ReadAll(strings.NewReader("12345"), 4); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("over-limit ReadAll() error = %v", err)
	}
	for _, limit := range []int64{0, -1} {
		if _, err := ReadAll(strings.NewReader("x"), limit); !errors.Is(err, errInvalidLimit) {
			t.Fatalf("ReadAll(limit=%d) error = %v", limit, err)
		}
	}
	if _, err := ReadAll(nil, 1); !errors.Is(err, errInvalidResponseBody) {
		t.Fatalf("ReadAll(nil) error = %v", err)
	}
}

func TestReadAllPreservesReaderErrors(t *testing.T) {
	want := errors.New("synthetic read failure")
	reader := errorReader{err: want}
	if _, err := ReadAll(reader, 8); !errors.Is(err, want) {
		t.Fatalf("ReadAll() error = %v", err)
	}
}

type errorReader struct {
	err error
}

func (r errorReader) Read([]byte) (int, error) {
	return 0, r.err
}

var _ io.Reader = errorReader{}
