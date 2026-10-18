package numberotp

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	mrand "math/rand/v2"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Version of this SDK.
const Version = "0.1.1"

// DefaultBaseURL is the production API.
const DefaultBaseURL = "https://api.numberotp.com/v1"

type apiRequest struct {
	method     string
	path       string
	query      url.Values
	body       any
	idempotent bool
	noAuth     bool
	accept     string
}

type transport struct {
	cfg     config
	mu      sync.RWMutex
	refresh sync.Mutex
	access  string
	rtoken  string
	apiKey  string
}

func newTransport(cfg config) *transport {
	return &transport{cfg: cfg, access: cfg.accessToken, rtoken: cfg.refreshToken, apiKey: cfg.apiKey}
}

func (t *transport) credential() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.access != "" {
		return t.access
	}
	return t.apiKey
}

func (t *transport) setTokens(tok TokenResponse) {
	t.mu.Lock()
	t.access = tok.AccessToken
	if tok.RefreshToken != "" {
		t.rtoken = tok.RefreshToken
	}
	t.apiKey = ""
	t.mu.Unlock()
	if t.cfg.onTokens != nil {
		t.cfg.onTokens(tok)
	}
}

func (t *transport) tokens() (access, refresh string) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.access, t.rtoken
}

func (t *transport) clearTokens() {
	t.mu.Lock()
	t.access, t.rtoken = "", ""
	t.mu.Unlock()
}

var retryStatuses = map[int]bool{408: true, 429: true, 500: true, 502: true, 503: true, 504: true}

// do sends the request and decodes a JSON success body into out.
func (t *transport) do(ctx context.Context, req apiRequest, out any, opts ...RequestOption) error {
	resp, err := t.send(ctx, req, opts...)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return &Error{Status: resp.StatusCode, Code: "CONNECTION_ERROR", Message: err.Error()}
	}
	if resp.StatusCode >= 400 {
		body := map[string]any{}
		_ = json.Unmarshal(data, &body)
		return errorFromBody(resp.StatusCode, body)
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return &Error{Status: resp.StatusCode, Code: "DECODE_ERROR", Message: "could not decode response: " + err.Error()}
	}
	return nil
}

// send performs the request with retries, timeout and token refresh, and
// returns the raw response (any status). The caller closes the body.
func (t *transport) send(ctx context.Context, req apiRequest, opts ...RequestOption) (*http.Response, error) {
	rc := requestConfig{timeout: t.cfg.timeout}
	for _, o := range opts {
		o(&rc)
	}
	maxRetries := t.cfg.maxRetries
	if rc.maxRetries != nil {
		maxRetries = *rc.maxRetries
	}

	u := strings.TrimRight(t.cfg.baseURL, "/") + req.path
	if len(req.query) > 0 {
		u += "?" + req.query.Encode()
	}
	var payload []byte
	if req.body != nil {
		b, err := json.Marshal(req.body)
		if err != nil {
			return nil, fmt.Errorf("numberotp: encode request: %w", err)
		}
		payload = b
	}
	idemKey := ""
	if req.idempotent {
		idemKey = rc.idempotencyKey
		if idemKey == "" {
			idemKey = randomKey() // one key for every attempt — that's what makes retries safe
		}
	}
	canRetry := req.method == http.MethodGet || req.method == http.MethodDelete || idemKey != ""
	refreshed := false

	for attempt := 0; ; attempt++ {
		attemptCtx, cancel := ctx, context.CancelFunc(func() {})
		if rc.timeout > 0 {
			attemptCtx, cancel = context.WithTimeout(ctx, rc.timeout)
		}
		var body io.Reader
		if payload != nil {
			body = bytes.NewReader(payload)
		}
		hreq, err := http.NewRequestWithContext(attemptCtx, req.method, u, body)
		if err != nil {
			cancel()
			return nil, err
		}
		accept := req.accept
		if accept == "" {
			accept = "application/json"
		}
		hreq.Header.Set("Accept", accept)
		hreq.Header.Set("User-Agent", "numberotp-go/"+Version)
		hreq.Header.Set("X-NumberOTP-Client", "sdk-go/"+Version)
		for k, vs := range t.cfg.headers {
			hreq.Header[k] = vs
		}
		for k, vs := range rc.headers {
			hreq.Header[k] = vs
		}
		if payload != nil {
			hreq.Header.Set("Content-Type", "application/json")
		}
		if cred := t.credential(); cred != "" && !req.noAuth {
			hreq.Header.Set("Authorization", "Bearer "+cred)
		}
		if idemKey != "" {
			hreq.Header.Set("Idempotency-Key", idemKey)
		}

		resp, err := t.cfg.httpClient.Do(hreq)
		if err != nil {
			cancel()
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if canRetry && attempt < maxRetries {
				if serr := sleep(ctx, backoff(attempt)); serr != nil {
					return nil, serr
				}
				continue
			}
			code := "CONNECTION_ERROR"
			if errors.Is(err, context.DeadlineExceeded) {
				code = "TIMEOUT"
			}
			return nil, &Error{Status: 0, Code: code, Message: "could not reach NumberOTP: " + err.Error()}
		}
		// Keep the attempt's timeout alive while the caller reads the body.
		resp.Body = &cancelOnClose{ReadCloser: resp.Body, cancel: cancel}

		access, refresh := t.tokens()
		if resp.StatusCode == 401 && !refreshed && !req.noAuth && access != "" && refresh != "" {
			refreshed = true
			resp.Body.Close()
			if err := t.refreshAccessToken(ctx); err != nil {
				return nil, err
			}
			continue
		}
		if retryStatuses[resp.StatusCode] && (canRetry || resp.StatusCode == 429) && attempt < maxRetries {
			delay := retryAfter(resp)
			if delay == 0 {
				delay = backoff(attempt)
			}
			resp.Body.Close()
			if serr := sleep(ctx, delay); serr != nil {
				return nil, serr
			}
			continue
		}
		return resp, nil
	}
}

func (t *transport) refreshAccessToken(ctx context.Context) error {
	t.refresh.Lock()
	defer t.refresh.Unlock()
	_, rt := t.tokens()
	if rt == "" {
		return &Error{Status: 401, Code: "NO_REFRESH_TOKEN", Message: "no refresh token to renew the session with"}
	}
	var tok TokenResponse
	err := t.do(ctx, apiRequest{
		method: http.MethodPost, path: "/oauth/token", noAuth: true,
		body: map[string]string{"grant_type": "refresh_token", "refresh_token": rt},
	}, &tok, WithRequestRetries(1))
	var apiErr *Error
	if errors.As(err, &apiErr) && apiErr.Status >= 400 && apiErr.Status < 500 && apiErr.Status != 401 && apiErr.Status != 429 {
		// A refused refresh token (revoked, expired, rotated away) means the session is over.
		code := apiErr.Code
		if code == "" {
			code = "invalid_grant"
		}
		return &Error{Status: 401, Code: code, Message: apiErr.Message, Body: apiErr.Body}
	}
	if err != nil {
		return err
	}
	t.setTokens(tok)
	return nil
}

type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelOnClose) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}

func backoff(attempt int) time.Duration {
	base := math.Min(8000, 500*math.Pow(2, float64(attempt)))
	return time.Duration(base/2+mrand.Float64()*base/2) * time.Millisecond
}

func retryAfter(resp *http.Response) time.Duration {
	v := resp.Header.Get("Retry-After")
	if v == "" {
		return 0
	}
	if secs, err := strconv.ParseFloat(v, 64); err == nil {
		return time.Duration(math.Min(60, math.Max(0, secs)) * float64(time.Second))
	}
	if at, err := http.ParseTime(v); err == nil {
		if d := time.Until(at); d > 0 && d < time.Minute {
			return d
		}
	}
	return 0
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func randomKey() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ── helpers used by generated.go ─────────────────────────────────────────────

func fmtValue(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case int:
		return strconv.Itoa(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	default:
		return fmt.Sprint(x)
	}
}

func isZero(v any) bool {
	return v == nil || reflect.ValueOf(v).IsZero()
}

// mergeExtra encodes params and adds the Extra fields (for open-ended bodies like /oauth/token).
func mergeExtra(params any, extra map[string]string) map[string]any {
	out := map[string]any{}
	if b, err := json.Marshal(params); err == nil {
		_ = json.Unmarshal(b, &out)
	}
	for k, v := range extra {
		if v != "" {
			out[k] = v
		}
	}
	return out
}
