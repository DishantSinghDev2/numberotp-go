package numberotp

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type recorded struct {
	Method, Path, Query string
	Header              http.Header
	Body                map[string]any
}

// server answers from handlers in order (the last one repeats) and records requests.
func server(t *testing.T, handlers ...func(w http.ResponseWriter, r *http.Request)) (*httptest.Server, *[]recorded) {
	t.Helper()
	var mu sync.Mutex
	calls := []recorded{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		i := len(calls)
		b, _ := io.ReadAll(r.Body)
		body := map[string]any{}
		_ = json.Unmarshal(b, &body)
		calls = append(calls, recorded{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Clone(), body})
		mu.Unlock()
		if i >= len(handlers) {
			i = len(handlers) - 1
		}
		handlers[i](w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func reply(status int, body any) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}
}

func newTestClient(srv *httptest.Server, opts ...Option) *Client {
	return NewClient(append([]Option{WithBaseURL(srv.URL + "/v1"), WithAPIKey("notp_test")}, opts...)...)
}

var activationJSON = map[string]any{
	"id": "123", "phone_number": "447700900123", "service": "wa", "service_name": "WhatsApp", "country": "16",
	"status": "received", "otp": "482913", "full_sms": "Your code is 482913", "sms": []any{}, "cost": 0.42,
	"pool": "pool1", "expires_at": "x", "created_at": "x",
}

func TestAuthHeaderAndQuery(t *testing.T) {
	srv, calls := server(t, reply(200, map[string]any{"success": true, "data": []any{}}))
	c := newTestClient(srv)
	if _, err := c.Activations.List(context.Background(), &ListActivationsParams{Status: String("waiting"), Limit: Int(5)}); err != nil {
		t.Fatal(err)
	}
	got := (*calls)[0]
	if got.Path != "/v1/user/activations" || got.Query != "limit=5&status=waiting" {
		t.Fatalf("unexpected request %s?%s", got.Path, got.Query)
	}
	if got.Header.Get("Authorization") != "Bearer notp_test" {
		t.Fatalf("missing auth header")
	}
}

func TestPublicEndpointsSendNoCredentials(t *testing.T) {
	srv, calls := server(t, reply(200, map[string]any{"success": true, "data": []any{}}))
	c := newTestClient(srv)
	if _, err := c.Services.Search(context.Background(), &SearchServicesParams{Q: "discord.com"}); err != nil {
		t.Fatal(err)
	}
	if h := (*calls)[0].Header.Get("Authorization"); h != "" {
		t.Fatalf("sent credentials to a public endpoint: %q", h)
	}
}

func TestPurchaseRetryReusesIdempotencyKey(t *testing.T) {
	srv, calls := server(t,
		reply(503, map[string]any{"error": "busy"}),
		reply(200, map[string]any{"success": true, "data": map[string]any{"id": "1", "phoneNumber": "1"}, "balance": 1}),
	)
	c := newTestClient(srv)
	res, err := c.Activations.Create(context.Background(), CreateActivationParams{Service: "wa", Country: "16"})
	if err != nil || res.Data.ID != "1" {
		t.Fatalf("create: %v %+v", err, res)
	}
	if len(*calls) != 2 {
		t.Fatalf("want 2 attempts, got %d", len(*calls))
	}
	k := (*calls)[0].Header.Get("Idempotency-Key")
	if k == "" || (*calls)[1].Header.Get("Idempotency-Key") != k {
		t.Fatalf("idempotency key not reused: %q vs %q", k, (*calls)[1].Header.Get("Idempotency-Key"))
	}
	if (*calls)[0].Body["service"] != "wa" || (*calls)[0].Body["pool"] != nil {
		t.Fatalf("unexpected body %+v", (*calls)[0].Body)
	}
}

func TestNonIdempotentPostNotRetried(t *testing.T) {
	srv, calls := server(t, reply(500, map[string]any{"error": "boom"}))
	c := newTestClient(srv)
	_, err := c.Activations.Finish(context.Background(), "123")
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Status != 500 || len(*calls) != 1 {
		t.Fatalf("want one 500 attempt, got %v (%d calls)", err, len(*calls))
	}
}

func TestTypedErrorCode(t *testing.T) {
	srv, _ := server(t, reply(402, map[string]any{"error": "Insufficient balance. Please add funds.", "code": "INSUFFICIENT_BALANCE"}))
	c := newTestClient(srv)
	_, err := c.Activations.Create(context.Background(), CreateActivationParams{Service: "wa", Country: "16"})
	var apiErr *Error
	if !errors.As(err, &apiErr) || !apiErr.IsInsufficientBalance() || apiErr.Code != "INSUFFICIENT_BALANCE" {
		t.Fatalf("got %v", err)
	}
}

func TestRefreshesExpiredAccessToken(t *testing.T) {
	var saved TokenResponse
	srv, calls := server(t,
		reply(401, map[string]any{"error": "Unauthorized"}),
		reply(200, map[string]any{"access_token": "new", "refresh_token": "notp_rt_new"}),
		func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer new" {
				t.Errorf("retry used %q", r.Header.Get("Authorization"))
			}
			reply(200, map[string]any{"success": true, "data": map[string]any{"balance": 3}})(w, r)
		},
	)
	c := NewClient(WithBaseURL(srv.URL+"/v1"), WithTokens("old", "notp_rt_old"), WithOnTokens(func(t TokenResponse) { saved = t }))
	me, err := c.Account.Get(context.Background())
	if err != nil || me.Data.Balance != 3 {
		t.Fatalf("me: %v %+v", err, me)
	}
	if (*calls)[1].Path != "/v1/oauth/token" || (*calls)[1].Body["refresh_token"] != "notp_rt_old" || (*calls)[1].Header.Get("Authorization") != "" {
		t.Fatalf("bad refresh call %+v", (*calls)[1])
	}
	if saved.RefreshToken != "notp_rt_new" || c.RefreshToken() != "notp_rt_new" {
		t.Fatalf("tokens not rotated")
	}
}

func TestWaitForCode(t *testing.T) {
	srv, calls := server(t,
		reply(408, map[string]any{"success": false, "error": "Timeout", "status": "waiting"}),
		reply(200, map[string]any{"success": true, "data": map[string]any{"otp": "482913", "status": "received"}}),
		reply(200, map[string]any{"success": true, "data": activationJSON}),
	)
	c := newTestClient(srv)
	act, err := c.WaitForCode(context.Background(), "123", time.Minute)
	if err != nil || act.Code() != "482913" {
		t.Fatalf("wait: %v %+v", err, act)
	}
	if len(*calls) != 3 || (*calls)[2].Path != "/v1/activations/123" {
		t.Fatalf("unexpected calls %+v", *calls)
	}
}

func TestWaitForCodeClosed(t *testing.T) {
	srv, _ := server(t, reply(400, map[string]any{"success": false, "error": "Activation is expired", "status": "expired"}))
	c := newTestClient(srv)
	_, err := c.WaitForCode(context.Background(), "123", time.Minute)
	var closed *ActivationClosedError
	if !errors.As(err, &closed) || closed.ActivationStatus != "expired" {
		t.Fatalf("got %v", err)
	}
}

func TestStreamEvents(t *testing.T) {
	ev, _ := json.Marshal(map[string]any{"id": "evt_1", "type": "activation.code_received", "created_at": "x", "data": activationJSON})
	srv, _ := server(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "retry: 2000\n: connected\n\nid: 1-0\nevent: activation.code_received\ndata: "+string(ev)+"\n\n")
	})
	c := newTestClient(srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for e, err := range c.StreamEvents(ctx, "") {
		if err != nil {
			t.Fatal(err)
		}
		a, _ := e.Activation()
		if e.Type != "activation.code_received" || e.StreamID != "1-0" || a.Code() != "482913" {
			t.Fatalf("bad event %+v", e)
		}
		return
	}
	t.Fatal("no event")
}

func TestDeviceLogin(t *testing.T) {
	srv, calls := server(t,
		reply(200, map[string]any{"device_code": "dc", "user_code": "BCDF-GHJK", "verification_uri": "u", "verification_uri_complete": "u?c", "expires_in": 600, "interval": 1}),
		reply(400, map[string]any{"error": "authorization_pending", "error_description": "Waiting"}),
		reply(200, map[string]any{"access_token": "at", "refresh_token": "notp_rt_1"}),
	)
	c := NewClient(WithBaseURL(srv.URL + "/v1"))
	var shown DeviceCode
	tok, err := c.DeviceLogin(context.Background(), DeviceLoginOptions{DeviceName: "test", OnCode: func(d DeviceCode) { shown = d }})
	if err != nil || tok.AccessToken != "at" || shown.UserCode != "BCDF-GHJK" {
		t.Fatalf("login: %v %+v", err, tok)
	}
	last := (*calls)[2].Body
	if last["grant_type"] != "urn:ietf:params:oauth:grant-type:device_code" || last["device_code"] != "dc" || last["platform"] != "cli" {
		t.Fatalf("bad token request %+v", last)
	}
}

func TestVerifyWebhook(t *testing.T) {
	secret, body := "whsec_test", []byte(`{"id":"123","otp":"482913"}`)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if !VerifyWebhook(body, sig, secret) {
		t.Fatal("valid signature rejected")
	}
	if VerifyWebhook(append(body, ' '), sig, secret) || VerifyWebhook(body, sig, "other") || VerifyWebhook(body, "", secret) {
		t.Fatal("invalid signature accepted")
	}
}
