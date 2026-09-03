package ipgw

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/UnbalancedCat/ipgw-meta/internal/cas"
	"github.com/UnbalancedCat/ipgw-meta/internal/srun"
	securetransport "github.com/UnbalancedCat/ipgw-meta/internal/transport"
)

var (
	acIDPattern     = regexp.MustCompile(`^[0-9]{1,10}$`)
	acIDBodyPattern = regexp.MustCompile(`(?:[?&]|\b)ac_id=([0-9]{1,10})`)
)

const (
	maxDynamicScriptCandidates = 8
	maxACIDBodyCandidates      = 32
	maxACIDDiscoveryResponses  = 2
)

func javascriptUTF16Length(value string) int {
	return len(utf16.Encode([]rune(value)))
}

func detectAuthenticationChallenge(data []byte) (cas.Challenge, error) {
	challenge, err := cas.DetectChallenge(data)
	if err != nil {
		return cas.ChallengeNone, newError(CodeProtocolChanged, "authentication challenge response is not recognized", false, err)
	}
	return challenge, nil
}

type ticketCapture struct {
	ticket string
	acID   string
}

func (c *Client) Login(ctx context.Context, request LoginRequest) (result LoginResult, resultErr error) {
	if err := validatePublicCall(c, ctx); err != nil {
		return LoginResult{}, err
	}
	c.emit(ctx, EventOperationStarted, "login", "preflight", "")
	defer func() {
		c.emit(ctx, EventOperationFinished, "login", "complete", observerOutcome(resultErr))
	}()
	if err := c.mutating.LockContext(ctx); err != nil {
		return LoginResult{}, wrapError(CodeNetwork, "login was canceled while waiting for another session change", true, err)
	}
	defer c.mutating.Unlock()

	request.ExpectedUsername = strings.TrimSpace(request.ExpectedUsername)
	if request.ExpectedUsername == "" {
		return LoginResult{}, newError(CodeInvalidArgument, "expected username is required", false, nil)
	}
	if request.Method == "" {
		request.Method = AuthMethodPassword
	}
	if request.Switch == "" {
		request.Switch = SwitchRefuse
	}
	if request.Method != AuthMethodPassword && request.Method != AuthMethodTerminalQR {
		return LoginResult{}, newError(CodeUnsupported, "authentication method is not supported", false, nil)
	}
	if request.Switch != SwitchRefuse && request.Switch != SwitchLogoutExisting {
		return LoginResult{}, newError(CodeInvalidArgument, "invalid switch policy", false, nil)
	}

	client := c.newHTTPClient(nil)
	status, err := c.status(ctx, client)
	if err != nil {
		return LoginResult{}, err
	}
	if status.Session == SessionOnline {
		if status.Username == request.ExpectedUsername {
			return LoginResult{Outcome: LoginAlreadyOnline, Status: status}, nil
		}
		if request.Switch == SwitchRefuse {
			conflict := newError(CodeSessionConflict, "another account is already online", false, nil)
			conflict.Details.ExpectedUser = request.ExpectedUsername
			conflict.Details.ActualUser = status.Username
			return LoginResult{}, conflict
		}
		if _, err := c.logoutOnline(ctx, client, status.Username); err != nil {
			return LoginResult{}, err
		}
	}

	acID, err := c.discoverACID(ctx)
	if err != nil {
		return LoginResult{}, err
	}
	serviceURL := cloneURL(c.endpoints.service)
	serviceQuery := serviceURL.Query()
	serviceQuery.Set("ac_id", acID)
	serviceURL.RawQuery = serviceQuery.Encode()
	loginURL := cloneURL(c.endpoints.casLogin)
	loginQuery := loginURL.Query()
	loginQuery.Set("service", serviceURL.String())
	loginURL.RawQuery = loginQuery.Encode()

	capture := &ticketCapture{}
	client = c.newHTTPClient(c.casRedirectPolicy(serviceURL, capture))
	pageData, response, err := c.doCAS(ctx, client, http.MethodGet, loginURL, nil, capture)
	if err != nil {
		return LoginResult{}, err
	}
	if capture.ticket != "" {
		return c.finishActivation(ctx, client, request.Method, request.ExpectedUsername, acID, capture.ticket)
	}
	page, err := cas.ParsePage(response.Request.URL, pageData)
	if err != nil {
		return LoginResult{}, wrapError(CodeProtocolChanged, "CAS login page could not be parsed", false, err)
	}
	if page.Challenge != cas.ChallengeNone {
		return LoginResult{}, c.challengeError(ctx, request.Method, page.Challenge)
	}

	switch request.Method {
	case AuthMethodPassword:
		return c.passwordLogin(ctx, client, request, loginURL, serviceURL, acID, page, pageData)
	case AuthMethodTerminalQR:
		return c.qrLogin(ctx, client, request, serviceURL, acID, page)
	default:
		return LoginResult{}, newError(CodeUnsupported, "authentication method is not supported", false, nil)
	}
}

func (c *Client) passwordLogin(ctx context.Context, client *http.Client, request LoginRequest, loginURL, serviceURL *url.URL, acID string, page cas.Page, pageData []byte) (LoginResult, error) {
	if isNilInterface(request.Credentials) {
		return LoginResult{}, newError(CodeConfig, "password login requires a credential provider", false, nil)
	}
	lt, ltOK := singleFormValue(page.Hidden, "lt")
	execution, executionOK := singleFormValue(page.Hidden, "execution")
	if page.Action == nil || !ltOK || !executionOK {
		return LoginResult{}, newError(CodeProtocolChanged, "CAS login form is missing required fields", false, nil)
	}
	if err := validateSameHTTPSOrigin(page.Action, c.endpoints.casLogin); err != nil {
		return LoginResult{}, err
	}
	publicKey := page.PublicKey
	if publicKey == "" {
		var candidates []*url.URL
		for _, script := range page.Scripts {
			if err := validateSameHTTPSOrigin(script, c.endpoints.casLogin); err != nil {
				continue
			}
			lowerPath := strings.ToLower(script.Path)
			if !strings.Contains(lowerPath, "login") && !strings.Contains(lowerPath, "rsa") {
				continue
			}
			candidates = append(candidates, script)
		}
		if len(candidates) > maxDynamicScriptCandidates {
			return LoginResult{}, newError(CodeProtocolChanged, "CAS advertised too many dynamic login scripts", false, nil)
		}
		for _, script := range candidates {
			data, _, err := c.request(ctx, client, http.MethodGet, script, nil, scriptResponseLimit)
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return LoginResult{}, err
				}
				continue
			}
			if publicKey = cas.ExtractPublicKey(data); publicKey != "" {
				break
			}
		}
	}
	if publicKey == "" {
		challenge, challengeErr := detectAuthenticationChallenge(pageData)
		if challengeErr != nil {
			return LoginResult{}, challengeErr
		}
		if challenge != cas.ChallengeNone {
			return LoginResult{}, c.challengeError(ctx, request.Method, challenge)
		}
		return LoginResult{}, newError(CodeProtocolChanged, "CAS public key was not discovered", false, nil)
	}
	credential, err := request.Credentials.Credential(ctx, CredentialRequest{Username: request.ExpectedUsername, Purpose: CredentialPurposeLogin})
	if err != nil {
		return LoginResult{}, wrapError(CodeConfig, "credential provider failed", false, err)
	}
	if credential.Password == "" {
		return LoginResult{}, newError(CodeConfig, "credential provider returned an empty password", false, nil)
	}
	encrypted, err := encryptCredential(request.ExpectedUsername, credential.Password, publicKey)
	if err != nil {
		return LoginResult{}, wrapError(CodeProtocolChanged, "CAS public key or encryption contract changed", false, err)
	}

	form := make(url.Values, 6)
	form.Set("rsa", encrypted)
	form.Set("ul", strconv.Itoa(javascriptUTF16Length(request.ExpectedUsername)))
	form.Set("pl", strconv.Itoa(javascriptUTF16Length(credential.Password)))
	form.Set("lt", lt)
	form.Set("execution", execution)
	form.Set("_eventId", "submit")
	capture := &ticketCapture{}
	postClient := c.newHTTPClient(c.casRedirectPolicy(serviceURL, capture))
	postClient.Jar = client.Jar
	postRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, page.Action.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return LoginResult{}, wrapError(CodeInvalidArgument, "could not construct CAS request", false, err)
	}
	postRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	data, _, err := c.doCASRequest(postClient, postRequest, capture)
	if err != nil {
		return LoginResult{}, err
	}
	if capture.ticket == "" {
		challenge, challengeErr := detectAuthenticationChallenge(data)
		if challengeErr != nil {
			return LoginResult{}, challengeErr
		}
		if challenge != cas.ChallengeNone {
			return LoginResult{}, c.challengeError(ctx, request.Method, challenge)
		}
		authenticationFailure, failureErr := cas.DetectAuthenticationFailure(data)
		if failureErr != nil {
			return LoginResult{}, newError(CodeProtocolChanged, "CAS authentication response is not recognized", false, failureErr)
		}
		if authenticationFailure {
			return LoginResult{}, newError(CodeAuthentication, "CAS rejected the supplied credentials", false, nil)
		}
		return LoginResult{}, newError(CodeProtocolChanged, "CAS did not return a service ticket or a recognized challenge", false, nil)
	}
	return c.finishActivation(ctx, postClient, request.Method, request.ExpectedUsername, acID, capture.ticket)
}

func (c *Client) qrLogin(ctx context.Context, client *http.Client, request LoginRequest, serviceURL *url.URL, acID string, page cas.Page) (LoginResult, error) {
	if isNilInterface(request.Interactions) {
		return LoginResult{}, interactionError(request.Method, InteractionDetails{
			Challenge:      ChallengeQRApproval,
			Capability:     []CapabilityStatus{CapabilityObservedAnonymous, CapabilitySyntheticCovered, CapabilityLiveUnverified},
			SessionBinding: interactionSessionCASSession, ResumeMode: interactionResumeRetryInTTY, TTYRequired: true,
			HelpID: interactionHelpQR,
		}, "terminal QR login requires an interactive QR presenter")
	}
	if page.QRUUID == "" {
		if !page.QRSupported {
			return LoginResult{}, newError(CodeProtocolChanged, "CAS did not advertise the supported QR protocol", false, nil)
		}
		generated, err := randomUUID()
		if err != nil {
			return LoginResult{}, wrapError(CodeInternal, "could not create an ephemeral QR identifier", false, err)
		}
		page.QRUUID = generated
	}
	qrURL := c.endpoints.casLogin.ResolveReference(&url.URL{Path: "qyQrLogin"})
	qrQuery := qrURL.Query()
	qrQuery.Set("uuid", page.QRUUID)
	qrQuery.Set("service", serviceURL.String())
	qrURL.RawQuery = qrQuery.Encode()
	if err := validateSameHTTPSOrigin(qrURL, c.endpoints.casLogin); err != nil {
		return LoginResult{}, err
	}
	prompt := QRCodePrompt{Payload: qrURL.String(), ExpiresAt: c.now().Add(c.qrLifetime).UTC(), PollInterval: c.pollInterval}
	if err := request.Interactions.PromptQRCode(ctx, prompt); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return LoginResult{}, err
		}
		return LoginResult{}, interactionError(request.Method, InteractionDetails{
			Challenge: ChallengeQRApproval, Capability: []CapabilityStatus{CapabilityLiveUnverified},
			SessionBinding: interactionSessionCASSession, ResumeMode: interactionResumeRetryInTTY, TTYRequired: true,
			HelpID: interactionHelpQR,
		}, "the caller could not present the QR code")
	}
	pollBase := c.endpoints.casLogin.ResolveReference(&url.URL{Path: "checkQRCodeScan"})
	deadline := prompt.ExpiresAt
	for {
		if !c.now().Before(deadline) {
			return LoginResult{}, terminalQRExpiredError(request.Method)
		}
		pollURL := cloneURL(pollBase)
		pollQuery := pollURL.Query()
		pollQuery.Set("uuid", page.QRUUID)
		nonce, nonceErr := randomUUID()
		if nonceErr != nil {
			return LoginResult{}, wrapError(CodeInternal, "could not create a QR polling cache buster", false, nonceErr)
		}
		pollQuery.Set("random", nonce)
		pollURL.RawQuery = pollQuery.Encode()
		data, _, err := c.request(ctx, client, http.MethodGet, pollURL, nil, apiResponseLimit)
		if err != nil {
			return LoginResult{}, err
		}
		poll, err := cas.ParsePoll(data)
		if err != nil {
			return LoginResult{}, newError(CodeProtocolChanged, "QR polling response is not recognized", false, err)
		}
		switch poll.State {
		case cas.PollConfirmed:
			return c.finishQRConfirmation(ctx, client, request, serviceURL, acID, poll.RedirectURL)
		case cas.PollChallenge:
			return LoginResult{}, c.challengeError(ctx, request.Method, poll.Challenge)
		case cas.PollExpired:
			return LoginResult{}, terminalQRExpiredError(request.Method)
		case cas.PollPending:
			timer := time.NewTimer(c.pollInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return LoginResult{}, ctx.Err()
			case <-timer.C:
			}
		}
	}
}

func terminalQRExpiredError(method AuthMethod) *Error {
	err := interactionError(method, InteractionDetails{
		Challenge: ChallengeQRApproval, Capability: []CapabilityStatus{CapabilityLiveUnverified},
		SessionBinding: interactionSessionCASSession, ResumeMode: interactionResumeRestart, TTYRequired: true,
		HelpID: interactionHelpQR,
	}, "terminal QR code expired")
	err.Retryable = true
	return err
}

func (c *Client) finishQRConfirmation(ctx context.Context, client *http.Client, request LoginRequest, serviceURL *url.URL, acID, rawRedirect string) (LoginResult, error) {
	target, parseErr := url.Parse(rawRedirect)
	if parseErr != nil {
		return LoginResult{}, newError(CodeProtocolChanged, "QR confirmation returned an invalid redirect", false, nil)
	}
	if sameServiceTarget(target, serviceURL) {
		capture, err := c.ticketFromRedirect(rawRedirect, serviceURL)
		if err != nil {
			return LoginResult{}, err
		}
		return c.finishActivation(ctx, client, request.Method, request.ExpectedUsername, acID, capture.ticket)
	}
	if !target.IsAbs() {
		target = c.endpoints.casLogin.ResolveReference(target)
	}
	query, queryErr := url.ParseQuery(target.RawQuery)
	if queryErr != nil || query.Get("ticket") != "" {
		return LoginResult{}, newError(CodeProtocolChanged, "QR confirmation returned an unsafe CAS redirect", false, nil)
	}
	if err := validateSameHTTPSOrigin(target, c.endpoints.casLogin); err != nil {
		return LoginResult{}, newError(CodeProtocolChanged, "QR confirmation returned an unexpected redirect", false, nil)
	}
	capture := &ticketCapture{}
	finalClient := c.newHTTPClient(c.casRedirectPolicy(serviceURL, capture))
	finalClient.Jar = client.Jar
	data, response, err := c.doCAS(ctx, finalClient, http.MethodGet, target, nil, capture)
	if err != nil {
		return LoginResult{}, err
	}
	if capture.ticket != "" {
		return c.finishActivation(ctx, finalClient, request.Method, request.ExpectedUsername, acID, capture.ticket)
	}
	base := target
	if response != nil && response.Request != nil && response.Request.URL != nil {
		base = response.Request.URL
	}
	page, pageErr := cas.ParsePage(base, data)
	if pageErr != nil {
		return LoginResult{}, newError(CodeProtocolChanged, "QR confirmation page could not be parsed", false, nil)
	}
	if page.Challenge != cas.ChallengeNone {
		return LoginResult{}, c.challengeError(ctx, request.Method, page.Challenge)
	}
	return LoginResult{}, newError(CodeProtocolChanged, "QR confirmation did not produce a ticket or a recognized challenge", false, nil)
}

func (c *Client) finishActivation(ctx context.Context, client *http.Client, method AuthMethod, expectedUsername, acID, ticket string) (LoginResult, error) {
	activation := cloneURL(c.endpoints.activation)
	if err := requireHTTPS(activation); err != nil {
		return LoginResult{}, err
	}
	if !sameHostname(activation, c.endpoints.service) {
		return LoginResult{}, newError(CodeProtocolChanged, "activation endpoint does not match the service host", false, nil)
	}
	query := activation.Query()
	query.Set("ac_id", acID)
	query.Set("ticket", ticket)
	activation.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, activation.String(), nil)
	if err != nil {
		return LoginResult{}, wrapError(CodeInvalidArgument, "could not construct activation request", false, err)
	}
	request.Header.Set("X-Requested-With", "XMLHttpRequest")
	activationClient := c.newHTTPClient(nil)
	activationClient.Jar = client.Jar
	response, err := activationClient.Do(request)
	if err != nil {
		return LoginResult{}, wrapError(CodeNetwork, "gateway activation request failed", true, err)
	}
	defer response.Body.Close()
	data, err := securetransport.ReadAll(response.Body, apiResponseLimit)
	if err != nil {
		return LoginResult{}, wrapError(CodeProtocolChanged, "gateway activation response is invalid", false, err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return LoginResult{}, newError(CodeNetwork, "gateway activation returned an unexpected HTTP status", response.StatusCode >= 500, nil)
	}
	challenge, challengeErr := detectAuthenticationChallenge(data)
	if challengeErr != nil {
		return LoginResult{}, challengeErr
	}
	if challenge != cas.ChallengeNone {
		return LoginResult{}, c.challengeError(ctx, method, challenge)
	}
	success, parseErr := srun.ParseActivationSuccess(data)
	if parseErr != nil {
		return LoginResult{}, newError(CodeProtocolChanged, "gateway activation response is not recognized", false, parseErr)
	}
	if !success {
		return LoginResult{}, newError(CodeAuthentication, "gateway rejected activation", false, nil)
	}
	status, err := c.verifyStatus(ctx, activationClient, SessionOnline, expectedUsername)
	if err != nil {
		return LoginResult{}, err
	}
	if networkKey := c.networkKey(); c.stateStore != nil && networkKey != "" {
		_ = c.stateStore.Save(ctx, ProtocolState{NetworkKey: networkKey, ACID: acID, VerifiedAt: c.now().UTC()})
	}
	return LoginResult{Outcome: LoginLoggedIn, Status: status}, nil
}

func (c *Client) Logout(ctx context.Context) (result LogoutResult, resultErr error) {
	if err := validatePublicCall(c, ctx); err != nil {
		return LogoutResult{}, err
	}
	c.emit(ctx, EventOperationStarted, "logout", "preflight", "")
	defer func() {
		c.emit(ctx, EventOperationFinished, "logout", "complete", observerOutcome(resultErr))
	}()
	if err := c.mutating.LockContext(ctx); err != nil {
		return LogoutResult{}, wrapError(CodeNetwork, "logout was canceled while waiting for another session change", true, err)
	}
	defer c.mutating.Unlock()
	client := c.newHTTPClient(nil)
	status, err := c.status(ctx, client)
	if err != nil {
		return LogoutResult{}, err
	}
	if status.Session == SessionOffline {
		return LogoutResult{Outcome: LogoutAlreadyOffline, Status: status}, nil
	}
	return c.logoutOnline(ctx, client, status.Username)
}

func (c *Client) logoutOnline(ctx context.Context, client *http.Client, username string) (LogoutResult, error) {
	target := cloneURL(c.endpoints.logout)
	if err := requireHTTPS(target); err != nil {
		return LogoutResult{}, err
	}
	query := target.Query()
	query.Set("action", "logout")
	query.Set("username", username)
	target.RawQuery = query.Encode()
	data, _, err := c.request(ctx, client, http.MethodGet, target, nil, apiResponseLimit)
	if err != nil {
		return LogoutResult{}, err
	}
	success, parseErr := srun.ParseLogoutSuccess(data)
	if parseErr != nil {
		return LogoutResult{}, newError(CodeProtocolChanged, "gateway logout response is not recognized", false, parseErr)
	}
	if !success {
		return LogoutResult{}, newError(CodeAuthentication, "gateway rejected logout", false, nil)
	}
	status, err := c.verifyStatus(ctx, client, SessionOffline, "")
	if err != nil {
		return LogoutResult{}, err
	}
	return LogoutResult{Outcome: LogoutLoggedOut, Status: status}, nil
}

func observerOutcome(err error) string {
	if err == nil {
		return "ok"
	}
	return string(CodeOf(err))
}

func (c *Client) discoverACID(ctx context.Context) (string, error) {
	redirect := func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client := &http.Client{
		Transport:     c.roundTripper,
		CheckRedirect: redirect,
		Timeout:       20 * time.Second,
	}
	target := *c.endpoints.captive
	for responseIndex := 0; responseIndex < maxACIDDiscoveryResponses; responseIndex++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
		if err != nil {
			return "", newError(CodeInternal, "gateway discovery request could not be created", false, err)
		}
		response, requestErr := client.Do(request)
		if requestErr != nil {
			return "", wrapError(CodeNetwork, "gateway discovery request failed", true, requestErr)
		}
		acID, outcome, next, inspectErr := c.inspectACIDDiscoveryResponse(response, responseIndex == 0)
		closeErr := response.Body.Close()
		if inspectErr != nil {
			return "", inspectErr
		}
		if closeErr != nil {
			return "", wrapError(CodeNetwork, "gateway discovery response failed", true, closeErr)
		}
		if acID != "" {
			c.emit(ctx, EventProtocolDiscovered, "login", "ac_id", outcome)
			return acID, nil
		}
		if next == nil {
			break
		}
		target = *next
	}
	if networkKey := c.networkKey(); c.stateStore != nil && networkKey != "" {
		state, found, loadErr := c.stateStore.Load(ctx, networkKey)
		if loadErr != nil {
			return "", wrapError(CodeConfig, "could not read protocol cache", false, loadErr)
		}
		if found && state.NetworkKey == networkKey && acIDPattern.MatchString(state.ACID) && !state.VerifiedAt.IsZero() && c.now().Sub(state.VerifiedAt) >= 0 && c.now().Sub(state.VerifiedAt) <= protocolCacheTTL {
			c.emit(ctx, EventProtocolDiscovered, "login", "ac_id", "verified_cache")
			return state.ACID, nil
		}
	}
	return "", newError(CodeProtocolChanged, "the active gateway access-controller ID could not be discovered", true, nil)
}

func (c *Client) inspectACIDDiscoveryResponse(response *http.Response, allowFollow bool) (string, string, *url.URL, error) {
	if response == nil || response.Body == nil || response.Request == nil || response.Request.URL == nil {
		return "", "", nil, newError(CodeNetwork, "gateway discovery response failed", true, nil)
	}
	var next *url.URL
	rawLocation := response.Header.Get("Location")
	if rawLocation != "" {
		if !isACIDDiscoveryRedirect(response.StatusCode) {
			return "", "", nil, newError(CodeProtocolChanged, "gateway discovery redirect is not allowed", false, nil)
		}
		if !validRawACIDDiscoveryLocation(rawLocation) {
			return "", "", nil, newError(CodeProtocolChanged, "gateway discovery redirect is not allowed", false, nil)
		}
		location, locationErr := response.Location()
		if locationErr != nil || !validACIDDiscoveryTarget(location, c.endpoints.captive) {
			return "", "", nil, newError(CodeProtocolChanged, "gateway discovery redirect is not allowed", false, locationErr)
		}
		query, queryErr := url.ParseQuery(location.RawQuery)
		if queryErr != nil {
			return "", "", nil, newError(CodeProtocolChanged, "gateway discovery redirect is not allowed", false, queryErr)
		}
		if values, present := query["ac_id"]; present {
			if len(values) != 1 || !acIDPattern.MatchString(values[0]) {
				return "", "", nil, newError(CodeProtocolChanged, "gateway discovery access-controller ID is invalid", false, nil)
			}
			return values[0], "redirect", nil, nil
		}
		if allowFollow && location.RawQuery == "" && !location.ForceQuery {
			next = location
		}
	}

	data, readErr := securetransport.ReadAll(response.Body, statusResponseLimit)
	if readErr != nil {
		if errors.Is(readErr, securetransport.ErrResponseTooLarge) {
			return "", "", nil, newError(CodeProtocolChanged, "gateway discovery response exceeded the safe limit", false, readErr)
		}
		return "", "", nil, wrapError(CodeNetwork, "gateway discovery response failed", true, readErr)
	}
	acID, bodyErr := uniqueACIDFromBody(data)
	if bodyErr != nil {
		return "", "", nil, newError(CodeProtocolChanged, "gateway discovery response is ambiguous", false, bodyErr)
	}
	if acID != "" {
		return acID, "body", nil, nil
	}
	return "", "", next, nil
}

func uniqueACIDFromBody(data []byte) (string, error) {
	matches := acIDBodyPattern.FindAllSubmatch(data, maxACIDBodyCandidates+1)
	if len(matches) > maxACIDBodyCandidates {
		return "", errors.New("too many access-controller ID candidates")
	}
	unique := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		if len(match) == 2 {
			unique[string(match[1])] = struct{}{}
		}
	}
	if len(unique) > 1 {
		return "", errors.New("conflicting access-controller ID candidates")
	}
	for acID := range unique {
		return acID, nil
	}
	return "", nil
}

func validRawACIDDiscoveryLocation(rawLocation string) bool {
	location, err := url.Parse(rawLocation)
	if err != nil || location.Opaque != "" || location.User != nil || location.Fragment != "" {
		return false
	}
	return location.Path == "" || validACIDDiscoveryPath(location)
}

func validACIDDiscoveryTarget(target, gateway *url.URL) bool {
	if target == nil || gateway == nil || target.Opaque != "" || target.User != nil || target.Fragment != "" ||
		!strings.EqualFold(target.Hostname(), gateway.Hostname()) {
		return false
	}
	switch strings.ToLower(target.Scheme) {
	case "http":
		if target.Port() != "" && target.Port() != "80" {
			return false
		}
	case "https":
		if target.Port() != "" && target.Port() != "443" {
			return false
		}
	default:
		return false
	}
	return validACIDDiscoveryPath(target)
}

func validACIDDiscoveryPath(target *url.URL) bool {
	escapedPath := target.EscapedPath()
	path, pathErr := url.PathUnescape(escapedPath)
	if pathErr != nil || len(escapedPath) > 768 || len(path) < 1 || len(path) > 256 ||
		!strings.HasPrefix(path, "/") || strings.Contains(path, "\\") {
		return false
	}
	for _, character := range path {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func isACIDDiscoveryRedirect(status int) bool {
	switch status {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	default:
		return false
	}
}

func (c *Client) casRedirectPolicy(serviceURL *url.URL, capture *ticketCapture) func(*http.Request, []*http.Request) error {
	return func(request *http.Request, via []*http.Request) error {
		if len(via) > 10 {
			return errRedirectRejected
		}
		target := request.URL
		if sameServiceTarget(target, serviceURL) {
			ticket, acID, err := validatedTicket(target, serviceURL)
			if err != nil {
				return errRedirectRejected
			}
			capture.ticket = ticket
			capture.acID = acID
			return http.ErrUseLastResponse
		}
		if target.Query().Get("ticket") != "" {
			return errRedirectRejected
		}
		if target.Scheme == "https" && sameHost(target, c.endpoints.casLogin) && target.User == nil {
			return nil
		}
		return errRedirectRejected
	}
}

func (c *Client) doCAS(ctx context.Context, client *http.Client, method string, target *url.URL, form url.Values, capture *ticketCapture) ([]byte, *http.Response, error) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	request, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return nil, nil, wrapError(CodeInvalidArgument, "could not construct CAS request", false, err)
	}
	if form != nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	return c.doCASRequest(client, request, capture)
}

func (c *Client) doCASRequest(client *http.Client, request *http.Request, capture *ticketCapture) ([]byte, *http.Response, error) {
	if err := requireHTTPS(request.URL); err != nil {
		return nil, nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, nil, wrapError(CodeNetwork, "CAS request failed", true, err)
	}
	defer response.Body.Close()
	data, err := securetransport.ReadAll(response.Body, htmlResponseLimit)
	if err != nil {
		return nil, response, wrapError(CodeProtocolChanged, "CAS response is invalid", false, err)
	}
	if capture.ticket == "" && (response.StatusCode < 200 || response.StatusCode >= 300) {
		return nil, response, newError(CodeNetwork, "CAS returned an unexpected HTTP status", response.StatusCode >= 500, nil)
	}
	return data, response, nil
}

func (c *Client) ticketFromRedirect(raw string, serviceURL *url.URL) (ticketCapture, error) {
	target, err := url.Parse(raw)
	if err != nil || !sameServiceTarget(target, serviceURL) {
		// raw may contain a ticket. Do not retain url.ParseError because its
		// Error string embeds the complete input URL.
		return ticketCapture{}, newError(CodeProtocolChanged, "QR confirmation returned an unexpected redirect", false, nil)
	}
	ticket, acID, validationErr := validatedTicket(target, serviceURL)
	if validationErr != nil {
		return ticketCapture{}, validationErr
	}
	return ticketCapture{ticket: ticket, acID: acID}, nil
}

func (c *Client) challengeError(ctx context.Context, method AuthMethod, challenge cas.Challenge) error {
	details := InteractionDetails{
		OriginMethod: method, SessionBinding: interactionSessionCASSession, ResumeMode: interactionResumeOfficialSite,
		HelpID: interactionHelpChallenge,
	}
	switch challenge {
	case cas.ChallengeSMSOTP:
		details.Challenge = ChallengeSMSOTP
		details.Capability = []CapabilityStatus{CapabilityObservedAnonymous, CapabilityDetectedOnly}
		details.HelpID = interactionHelpSMS
	case cas.ChallengeDevice:
		details.Challenge = ChallengeDeviceVerification
		details.Capability = []CapabilityStatus{CapabilityDetectedOnly}
	case cas.ChallengeSetup:
		details.Challenge = ChallengeAccountSetup
		details.Capability = []CapabilityStatus{CapabilityDetectedOnly}
	default:
		details.Challenge = ChallengeUnknown
		details.Capability = []CapabilityStatus{CapabilityUnknown}
	}
	c.emit(ctx, EventInteractionNeeded, "login", "challenge", string(details.Challenge))
	return interactionError(method, details, "login requires an additional interactive verification step")
}

func validateSameHTTPSOrigin(target, expected *url.URL) error {
	if target == nil || target.Scheme != "https" || target.User != nil || !sameHost(target, expected) {
		return newError(CodeProtocolChanged, "CAS resource points outside the trusted HTTPS origin", false, nil)
	}
	return nil
}

func sameHost(left, right *url.URL) bool {
	return left != nil && right != nil && strings.EqualFold(left.Hostname(), right.Hostname()) && normalizedPort(left) == normalizedPort(right)
}

func sameHostname(left, right *url.URL) bool {
	return left != nil && right != nil && strings.EqualFold(left.Hostname(), right.Hostname())
}

func normalizedPort(target *url.URL) string {
	if port := target.Port(); port != "" {
		return port
	}
	if target.Scheme == "https" {
		return "443"
	}
	if target.Scheme == "http" {
		return "80"
	}
	return ""
}

func sameServiceTarget(target, expected *url.URL) bool {
	return target != nil && expected != nil && target.Scheme == expected.Scheme && sameHost(target, expected) && target.EscapedPath() == expected.EscapedPath() && target.User == nil && target.Fragment == ""
}

func validatedTicket(target, expected *url.URL) (string, string, error) {
	query, parseErr := url.ParseQuery(target.RawQuery)
	if parseErr != nil {
		return "", "", newError(CodeProtocolChanged, "CAS service redirect contains an invalid query", false, nil)
	}
	if len(query) != 2 || len(query["ticket"]) != 1 || len(query["ac_id"]) != 1 {
		return "", "", newError(CodeProtocolChanged, "CAS service redirect contains unexpected parameters", false, nil)
	}
	ticket := query.Get("ticket")
	acID := query.Get("ac_id")
	if ticket == "" || len(ticket) > 4096 || strings.ContainsAny(ticket, "\r\n\t ") || acID != expected.Query().Get("ac_id") || !acIDPattern.MatchString(acID) {
		return "", "", newError(CodeProtocolChanged, "CAS service redirect is missing required parameters", false, nil)
	}
	return ticket, acID, nil
}

func randomUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}

func cloneURL(source *url.URL) *url.URL {
	clone := *source
	return &clone
}

func singleFormValue(values url.Values, key string) (string, bool) {
	discovered, ok := values[key]
	if !ok || len(discovered) != 1 || discovered[0] == "" {
		return "", false
	}
	return discovered[0], true
}
