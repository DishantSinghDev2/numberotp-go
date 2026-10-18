// Package numberotp is the official Go client for the NumberOTP API —
// virtual numbers for SMS / OTP verification.
//
//	client := numberotp.NewClient() // reads NOTP_API_KEY
//	n, err := client.Activations.Create(ctx, numberotp.CreateActivationParams{Service: "wa", Country: "16"})
//	act, err := client.WaitForCode(ctx, n.Data.ID, 20*time.Minute)
//	fmt.Println(act.Code())
package numberotp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"iter"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Client talks to the NumberOTP API. Its fields are the API resources.
type Client struct {
	services
	t *transport
}

// NewClient builds a client. With no options it uses $NOTP_API_KEY.
func NewClient(opts ...Option) *Client {
	cfg := config{
		baseURL:    DefaultBaseURL,
		timeout:    30 * time.Second,
		maxRetries: 2,
	}
	if v := os.Getenv("NOTP_BASE_URL"); v != "" {
		cfg.baseURL = v
	}
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.apiKey == "" && cfg.accessToken == "" {
		cfg.apiKey = os.Getenv("NOTP_API_KEY")
	}
	if cfg.httpClient == nil {
		cfg.httpClient = &http.Client{}
	}
	t := newTransport(cfg)
	return &Client{services: newServices(t), t: t}
}

// SetTokens switches the client to a signed-in user's tokens.
func (c *Client) SetTokens(tok TokenResponse) { c.t.setTokens(tok) }

// RefreshToken returns the current refresh token (to persist securely).
func (c *Client) RefreshToken() string { _, r := c.t.tokens(); return r }

// ── Waiting for codes ────────────────────────────────────────────────────────

// WaitForCode waits until the number's code arrives and returns the whole
// activation. It returns *ActivationClosedError if the number expired or was
// canceled first (it's refunded), or ErrWaitTimeout when timeout passes.
func (c *Client) WaitForCode(ctx context.Context, id string, timeout time.Duration) (*Activation, error) {
	if timeout <= 0 {
		timeout = 20 * time.Minute
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		round := time.Until(deadline) + 5*time.Second
		if round > 70*time.Second {
			round = 70 * time.Second
		}
		_, err := c.Activations.WaitOnce(ctx, id, WithRequestTimeout(round), WithRequestRetries(0))
		if err == nil {
			res, err := c.Activations.Get(ctx, id)
			if err != nil {
				return nil, err
			}
			return &res.Data, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var apiErr *Error
		if errors.As(err, &apiErr) {
			status, _ := apiErr.Body["status"].(string)
			switch {
			case apiErr.Status == 408 || status == "waiting" || apiErr.Code == "TIMEOUT":
				continue
			case apiErr.Status == 400 && (status == "expired" || status == "canceled" || status == "finished"):
				return nil, &ActivationClosedError{ActivationStatus: status}
			}
		}
		return nil, err
	}
	return nil, ErrWaitTimeout
}

// CreateAndWait buys a number, calls onNumber with it (may be nil), then waits for its code.
func (c *Client) CreateAndWait(ctx context.Context, params CreateActivationParams, timeout time.Duration, onNumber func(*CreateActivationResult)) (*Activation, error) {
	created, err := c.Activations.Create(ctx, params)
	if err != nil {
		return nil, err
	}
	if onNumber != nil {
		onNumber(created)
	}
	return c.WaitForCode(ctx, created.Data.ID, timeout)
}

// ── Live events ──────────────────────────────────────────────────────────────

// StreamEvents yields live account events until ctx is done. It reconnects by
// itself and resumes after the last event, so none are skipped. Pass since=""
// to start from now.
//
//	for ev, err := range client.StreamEvents(ctx, "") {
//		if err != nil { return err }
//		if ev.Type == "activation.code_received" { … }
//	}
func (c *Client) StreamEvents(ctx context.Context, since string) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		cursor := since
		failures := 0
		for ctx.Err() == nil {
			q := url.Values{}
			if cursor != "" {
				q.Set("since", cursor)
			}
			resp, err := c.t.send(ctx, apiRequest{method: http.MethodGet, path: "/events", query: q, accept: "text/event-stream"},
				WithRequestTimeout(75*time.Second), WithRequestRetries(0))
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				failures++
				if failures > 5 {
					yield(Event{}, err)
					return
				}
				if sleep(ctx, time.Duration(1<<failures)*time.Second) != nil {
					return
				}
				continue
			}
			if resp.StatusCode >= 400 {
				body := map[string]any{}
				_ = json.NewDecoder(resp.Body).Decode(&body)
				resp.Body.Close()
				yield(Event{}, errorFromBody(resp.StatusCode, body))
				return
			}
			failures = 0
			stop := false
			readSSE(resp.Body, func(id, data string) bool {
				var ev Event
				if err := json.Unmarshal([]byte(data), &ev); err != nil {
					return true
				}
				ev.StreamID = id
				if id != "" {
					cursor = id
				}
				if !yield(ev, nil) {
					stop = true
					return false
				}
				return true
			})
			resp.Body.Close()
			if stop {
				return
			}
		}
	}
}

// readSSE calls onMessage for each Server-Sent Event until the stream ends or onMessage returns false.
func readSSE(r io.Reader, onMessage func(id, data string) bool) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	var id string
	var data []string
	for sc.Scan() {
		line := strings.TrimSuffix(sc.Text(), "\r")
		if line == "" {
			if len(data) > 0 && !onMessage(id, strings.Join(data, "\n")) {
				return
			}
			id, data = "", nil
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "data":
			data = append(data, value)
		case "id":
			id = value
		}
	}
}

// ── Sign-in ──────────────────────────────────────────────────────────────────

// DeviceLoginOptions configures DeviceLogin.
type DeviceLoginOptions struct {
	// Client is "cli" (default), "extension", "mcp" or "other".
	Client     string
	DeviceName string
	// OnCode shows the person the code and the link to approve it.
	OnCode func(DeviceCode)
}

// DeviceLogin signs in with the device flow (RFC 8628): show a code, the person
// approves it on numberotp.com/device. The client is signed in afterwards.
func (c *Client) DeviceLogin(ctx context.Context, o DeviceLoginOptions) (*TokenResponse, error) {
	if o.Client == "" {
		o.Client = "cli"
	}
	code, err := c.Auth.StartDeviceSignIn(ctx, StartDeviceSignInParams{Client: &o.Client, DeviceName: &o.DeviceName})
	if err != nil {
		return nil, err
	}
	if o.OnCode != nil {
		o.OnCode(*code)
	}
	interval := time.Duration(max(1, code.Interval)) * time.Second
	deadline := time.Now().Add(time.Duration(code.ExpiresIn) * time.Second)
	for time.Now().Before(deadline) {
		if err := sleep(ctx, interval); err != nil {
			return nil, err
		}
		tok, err := c.signIn(ctx, "urn:ietf:params:oauth:grant-type:device_code", map[string]string{
			"device_code": code.DeviceCode, "platform": o.Client, "device_name": o.DeviceName,
		})
		if err == nil {
			return tok, nil
		}
		var apiErr *Error
		if errors.As(err, &apiErr) {
			switch apiErr.Code {
			case "authorization_pending":
				continue
			case "slow_down":
				interval += 5 * time.Second
				continue
			}
		}
		return nil, err
	}
	return nil, &Error{Status: 400, Code: "expired_token", Message: "the sign-in code expired before it was approved"}
}

// SignInWithEmailCode exchanges the 6-digit code from Auth.SendEmailCode — creates the account if it's new.
func (c *Client) SignInWithEmailCode(ctx context.Context, email, code, platform, deviceName string) (*TokenResponse, error) {
	return c.signIn(ctx, "urn:numberotp:params:grant-type:email_code", map[string]string{
		"email": email, "code": code, "platform": platform, "device_name": deviceName,
	})
}

// SignOut revokes the refresh token server-side and forgets the tokens.
func (c *Client) SignOut(ctx context.Context) error {
	if _, rt := c.t.tokens(); rt != "" {
		_, _ = c.Auth.Revoke(ctx, RevokeTokenParams{Token: rt})
	}
	c.t.clearTokens()
	return nil
}

func (c *Client) signIn(ctx context.Context, grant string, fields map[string]string) (*TokenResponse, error) {
	tok, err := c.Auth.Token(ctx, OauthTokenParams{GrantType: grant, Extra: fields})
	if err != nil {
		return nil, err
	}
	c.t.setTokens(*tok)
	return tok, nil
}
