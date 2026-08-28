package ipgw

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/netip"
	"net/url"
	"reflect"
	"strings"
	"time"

	securetransport "github.com/UnbalancedCat/ipgw-meta/internal/transport"
)

const (
	statusResponseLimit = int64(256 << 10)
	htmlResponseLimit   = int64(2 << 20)
	scriptResponseLimit = int64(512 << 10)
	apiResponseLimit    = int64(256 << 10)
	protocolCacheTTL    = 7 * 24 * time.Hour
	casEncodedKeyLimit  = 8 << 10
	casDERKeyLimit      = 4 << 10
	casRSAMinBits       = 2048
	casRSAMaxBits       = 8192
)

type endpointSet struct {
	status     *url.URL
	logout     *url.URL
	captive    *url.URL
	casLogin   *url.URL
	service    *url.URL
	activation *url.URL
}

type Client struct {
	roundTripper http.RoundTripper
	observer     Observer
	stateStore   ProtocolStateStore
	bindIP       netip.Addr
	endpoints    endpointSet
	now          func() time.Time
	mutating     contextMutex
	pollInterval time.Duration
	qrLifetime   time.Duration
	verifyDelay  time.Duration
}

type contextMutex struct {
	token chan struct{}
}

func newContextMutex() contextMutex {
	return contextMutex{token: make(chan struct{}, 1)}
}

// Lock remains only as a short migration bridge for the current callers;
// mutating public methods use LockContext so cancellation also covers time
// spent waiting behind another mutation.
func (m *contextMutex) Lock() { m.token <- struct{}{} }

func (m *contextMutex) LockContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case m.token <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-m.token
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *contextMutex) Unlock() { <-m.token }

func validatePublicCall(client *Client, ctx context.Context) error {
	if client == nil || client.roundTripper == nil || client.now == nil || client.endpoints.status == nil || client.mutating.token == nil {
		return newError(CodeInvalidArgument, "client must be created with NewClient", false, nil)
	}
	if ctx == nil {
		return newError(CodeInvalidArgument, "context must not be nil", false, nil)
	}
	return nil
}

func NewClient(options ...Option) (*Client, error) {
	settings := clientOptions{}
	for _, option := range options {
		if option == nil {
			return nil, newError(CodeInvalidArgument, "client option must not be nil", false, nil)
		}
		if err := option(&settings); err != nil {
			return nil, newError(CodeInvalidArgument, err.Error(), false, err)
		}
	}
	roundTripper := settings.roundTripper
	if roundTripper != nil && isNilInterface(roundTripper) {
		return nil, newError(CodeInvalidArgument, "round tripper must not be a typed nil", false, nil)
	}
	if candidate, ok := roundTripper.(*http.Transport); ok {
		if candidate.TLSClientConfig != nil && candidate.TLSClientConfig.InsecureSkipVerify {
			return nil, newError(CodeInvalidArgument, "TLS certificate verification must not be disabled", false, nil)
		}
		// Clone both the transport and its TLS config. This preserves the rule
		// that caller-owned transports are not modified and prevents a later
		// caller mutation from weakening this Client's verification policy.
		roundTripper = candidate.Clone()
	} else if roundTripper != nil {
		// An arbitrary implementation cannot be inspected for PKI behaviour.
		// It is therefore a fully trusted extension boundary, intended for
		// controlled instrumentation and synthetic tests.
	}
	if roundTripper == nil {
		roundTripper = securetransport.Default(settings.bindIP)
	}
	endpoints, err := defaultEndpoints()
	if err != nil {
		return nil, newError(CodeInternal, "invalid built-in endpoints", false, err)
	}
	return &Client{
		roundTripper: securetransport.Wrap(roundTripper),
		observer:     settings.observer,
		stateStore:   settings.stateStore,
		bindIP:       settings.bindIP,
		endpoints:    endpoints,
		now:          time.Now,
		mutating:     newContextMutex(),
		pollInterval: 3 * time.Second,
		qrLifetime:   180 * time.Second,
		verifyDelay:  500 * time.Millisecond,
	}, nil
}

func isNilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func defaultEndpoints() (endpointSet, error) {
	parse := func(value string) (*url.URL, error) { return url.Parse(value) }
	status, err := parse("https://ipgw.neu.edu.cn/cgi-bin/rad_user_info")
	if err != nil {
		return endpointSet{}, err
	}
	logout, err := parse("https://ipgw.neu.edu.cn/cgi-bin/srun_portal")
	if err != nil {
		return endpointSet{}, err
	}
	captive, err := parse("http://ipgw.neu.edu.cn/")
	if err != nil {
		return endpointSet{}, err
	}
	casLogin, err := parse("https://pass.neu.edu.cn/tpass/login")
	if err != nil {
		return endpointSet{}, err
	}
	service, err := parse("http://ipgw.neu.edu.cn/srun_portal_sso")
	if err != nil {
		return endpointSet{}, err
	}
	activation, err := parse("https://ipgw.neu.edu.cn/v1/srun_portal_sso")
	if err != nil {
		return endpointSet{}, err
	}
	return endpointSet{status: status, logout: logout, captive: captive, casLogin: casLogin, service: service, activation: activation}, nil
}

func (c *Client) newHTTPClient(redirect func(*http.Request, []*http.Request) error) *http.Client {
	if redirect == nil {
		redirect = rejectRedirect
	}
	jar, _ := cookiejar.New(nil)
	return &http.Client{Transport: c.roundTripper, Jar: jar, CheckRedirect: redirect, Timeout: 20 * time.Second}
}

func rejectRedirect(*http.Request, []*http.Request) error {
	return errRedirectRejected
}

func (c *Client) emit(ctx context.Context, name EventName, operation, phase, outcome string) {
	if c.observer != nil {
		c.observer.Observe(ctx, Event{Name: name, Operation: operation, Phase: phase, Outcome: outcome, At: c.now().UTC()})
	}
}

func (c *Client) networkKey() string {
	// A bind address is not a network identity: the same DHCP or static IPv4
	// can recur on unrelated wired and wireless networks. Until the caller can
	// provide a verified network fingerprint, persistent protocol fallback is
	// deliberately disabled. The state-store option remains an API seam for a
	// future, explicitly versioned fingerprint without weakening v1.
	_ = c
	return ""
}

func requireHTTPS(target *url.URL) error {
	if target == nil || target.Scheme != "https" || target.Host == "" || target.User != nil {
		return newError(CodeProtocolChanged, "protocol attempted a non-HTTPS authenticated request", false, nil)
	}
	return nil
}

func (c *Client) request(ctx context.Context, client *http.Client, method string, target *url.URL, body io.Reader, limit int64) ([]byte, *http.Response, error) {
	var bodyReader io.Reader = http.NoBody
	if body != nil {
		bodyReader = body
	}
	request, err := http.NewRequestWithContext(ctx, method, target.String(), bodyReader)
	if err != nil {
		return nil, nil, wrapError(CodeInvalidArgument, "could not construct request", false, err)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, nil, wrapError(CodeNetwork, "network request failed", true, err)
	}
	defer response.Body.Close()
	data, err := securetransport.ReadAll(response.Body, limit)
	if err != nil {
		if errors.Is(err, securetransport.ErrResponseTooLarge) {
			return nil, response, newError(CodeProtocolChanged, "gateway response exceeded the safe limit", false, err)
		}
		return nil, response, wrapError(CodeNetwork, "could not read gateway response", true, err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, response, newError(CodeNetwork, "gateway returned an unexpected HTTP status", response.StatusCode >= 500, nil)
	}
	return data, response, nil
}

func encryptCredential(username, password, encodedKey string) (string, error) {
	encodedKey = strings.TrimSpace(encodedKey)
	if len(encodedKey) > casEncodedKeyLimit {
		return "", fmt.Errorf("CAS public key exceeds the encoded size limit")
	}
	der, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil {
		return "", fmt.Errorf("CAS public key is not valid base64")
	}
	if len(der) > casDERKeyLimit {
		return "", fmt.Errorf("CAS public key exceeds the DER size limit")
	}
	var publicKey *rsa.PublicKey
	parsed, pkixErr := x509.ParsePKIXPublicKey(der)
	if pkixErr == nil {
		var ok bool
		publicKey, ok = parsed.(*rsa.PublicKey)
		if !ok {
			return "", fmt.Errorf("CAS public key is not RSA")
		}
	} else {
		parsedPKCS1, pkcs1Err := x509.ParsePKCS1PublicKey(der)
		if pkcs1Err != nil {
			return "", fmt.Errorf("CAS public key is neither valid PKIX nor PKCS#1")
		}
		publicKey = parsedPKCS1
	}
	bits := publicKey.N.BitLen()
	if bits < casRSAMinBits || bits > casRSAMaxBits {
		return "", fmt.Errorf("CAS RSA modulus must be between %d and %d bits", casRSAMinBits, casRSAMaxBits)
	}
	ciphertext, err := rsa.EncryptPKCS1v15(rand.Reader, publicKey, []byte(username+password))
	if err != nil {
		return "", fmt.Errorf("encrypt credential: %w", err)
	}
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}
