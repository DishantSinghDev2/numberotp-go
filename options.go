package numberotp

import (
	"net/http"
	"time"
)

// Option configures a Client.
type Option func(*config)

type config struct {
	apiKey       string
	accessToken  string
	refreshToken string
	onTokens     func(TokenResponse)
	baseURL      string
	httpClient   *http.Client
	timeout      time.Duration
	maxRetries   int
	headers      http.Header
}

// WithAPIKey authenticates with a developer API key (notp_…). Defaults to $NOTP_API_KEY.
func WithAPIKey(key string) Option { return func(c *config) { c.apiKey = key } }

// WithTokens authenticates as a signed-in user. With a refresh token the
// client renews the access token by itself.
func WithTokens(accessToken, refreshToken string) Option {
	return func(c *config) { c.accessToken, c.refreshToken = accessToken, refreshToken }
}

// WithOnTokens is called whenever new tokens are issued — persist them (securely) here.
func WithOnTokens(fn func(TokenResponse)) Option { return func(c *config) { c.onTokens = fn } }

// WithBaseURL overrides https://api.numberotp.com/v1 (reseller white-label domains work too).
func WithBaseURL(u string) Option { return func(c *config) { c.baseURL = u } }

// WithHTTPClient uses your own *http.Client (proxies, tests).
func WithHTTPClient(h *http.Client) Option { return func(c *config) { c.httpClient = h } }

// WithTimeout sets the per-request timeout (default 30s).
func WithTimeout(d time.Duration) Option { return func(c *config) { c.timeout = d } }

// WithMaxRetries sets retries for network errors, 429 and 5xx where safe (default 2).
func WithMaxRetries(n int) Option { return func(c *config) { c.maxRetries = n } }

// WithHeader adds a header to every request.
func WithHeader(key, value string) Option {
	return func(c *config) {
		if c.headers == nil {
			c.headers = http.Header{}
		}
		c.headers.Set(key, value)
	}
}

// RequestOption tunes one call.
type RequestOption func(*requestConfig)

type requestConfig struct {
	timeout        time.Duration
	maxRetries     *int
	idempotencyKey string
	headers        http.Header
}

// WithRequestTimeout overrides the timeout for one call.
func WithRequestTimeout(d time.Duration) RequestOption {
	return func(r *requestConfig) { r.timeout = d }
}

// WithRequestRetries overrides the retry count for one call.
func WithRequestRetries(n int) RequestOption { return func(r *requestConfig) { r.maxRetries = &n } }

// WithIdempotencyKey uses your own key for a buy / rent / top-up call (one is generated otherwise).
func WithIdempotencyKey(k string) RequestOption {
	return func(r *requestConfig) { r.idempotencyKey = k }
}

// WithRequestHeader adds a header to one call.
func WithRequestHeader(key, value string) RequestOption {
	return func(r *requestConfig) {
		if r.headers == nil {
			r.headers = http.Header{}
		}
		r.headers.Set(key, value)
	}
}

// Pointer helpers for optional fields.
func String(v string) *string  { return &v }
func Int(v int) *int           { return &v }
func Float(v float64) *float64 { return &v }
func Bool(v bool) *bool        { return &v }
