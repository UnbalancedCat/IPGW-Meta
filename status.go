package ipgw

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/UnbalancedCat/ipgw-meta/internal/srun"
)

func (c *Client) Status(ctx context.Context) (Status, error) {
	if err := validatePublicCall(c, ctx); err != nil {
		return Status{}, err
	}
	c.emit(ctx, EventOperationStarted, "status", "request", "")
	status, err := c.status(ctx, c.newHTTPClient(nil))
	if err != nil {
		c.emit(ctx, EventOperationFinished, "status", "request", "error")
		return Status{}, err
	}
	c.emit(ctx, EventOperationFinished, "status", "request", string(status.Session))
	return status, nil
}

func (c *Client) status(ctx context.Context, client *http.Client) (Status, error) {
	if err := requireHTTPS(c.endpoints.status); err != nil {
		return Status{}, err
	}
	data, _, err := c.request(ctx, client, http.MethodGet, c.endpoints.status, nil, statusResponseLimit)
	if err != nil {
		return Status{}, err
	}
	parsed, err := srun.ParseStatus(data)
	if err != nil {
		if errors.Is(err, srun.ErrUnrecognized) {
			protocolErr := newError(CodeProtocolChanged, "gateway status format is not recognized", false, err)
			protocolErr.Details.ProtocolPart = "gateway_status"
			return Status{}, protocolErr
		}
		return Status{}, wrapError(CodeProtocolChanged, "could not parse gateway status", false, err)
	}
	result := Status{Network: NetworkReachable, Session: SessionOffline, ObservedAt: c.now().UTC()}
	if parsed.Online {
		result.Session = SessionOnline
		result.Username = parsed.Username
		result.OnlineIP = parsed.OnlineIP
		if parsed.Summary != nil {
			result.Summary = &OnlineSummary{TrafficBytes: parsed.Summary.TrafficBytes, DurationSeconds: parsed.Summary.DurationSeconds}
			if parsed.Summary.BalanceMinor != nil {
				result.Summary.Balance = &Money{Currency: "CNY", MinorUnits: *parsed.Summary.BalanceMinor}
			}
		}
	}
	return result, nil
}

func (c *Client) verifyStatus(ctx context.Context, client *http.Client, expected SessionState, username string) (Status, error) {
	var last Status
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(c.verifyDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return Status{}, ctx.Err()
			case <-timer.C:
			}
		}
		status, err := c.status(ctx, client)
		if err != nil {
			return Status{}, err
		}
		last = status
		if status.Session == expected && (username == "" || status.Username == username) {
			return status, nil
		}
	}
	if expected == SessionOnline && last.Session == SessionOnline && last.Username != username {
		err := newError(CodeSessionConflict, "gateway activated a different account", false, nil)
		err.Details.ExpectedUser = username
		err.Details.ActualUser = last.Username
		return Status{}, err
	}
	return Status{}, newError(CodeAuthentication, "gateway did not reach the expected session state", true, nil)
}
