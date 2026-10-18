package numberotp

import (
	"errors"
	"fmt"
	"regexp"
)

// Error is returned for every failed API call. Branch on Code (stable, e.g.
// "INSUFFICIENT_BALANCE", "NO_NUMBERS_AVAILABLE", "invalid_grant"), not on
// Message, which is written for people and may change.
//
//	var apiErr *numberotp.Error
//	if errors.As(err, &apiErr) && apiErr.Code == "INSUFFICIENT_BALANCE" { … }
type Error struct {
	// Status is the HTTP status, or 0 when the request never got an answer.
	Status  int
	Code    string
	Message string
	// Body is the decoded error body, when there was one.
	Body map[string]any
}

func (e *Error) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("numberotp: %s (%d %s)", e.Message, e.Status, e.Code)
	}
	return fmt.Sprintf("numberotp: %s (%d)", e.Message, e.Status)
}

// Convenience checks.
func (e *Error) IsInsufficientBalance() bool { return e.Status == 402 }
func (e *Error) IsUnauthorized() bool        { return e.Status == 401 }
func (e *Error) IsNotFound() bool            { return e.Status == 404 }
func (e *Error) IsConflict() bool            { return e.Status == 409 }
func (e *Error) IsRateLimited() bool         { return e.Status == 429 }

var oauthCode = regexp.MustCompile(`^[a-z_]+$`)

func errorFromBody(status int, body map[string]any) *Error {
	e := &Error{Status: status, Body: body}
	if c, ok := body["code"].(string); ok {
		e.Code = c
	} else if c, ok := body["error"].(string); ok && oauthCode.MatchString(c) {
		e.Code = c // OAuth (RFC 6749) errors
	}
	if d, ok := body["error_description"].(string); ok && d != "" {
		e.Message = d
	} else if m, ok := body["error"].(string); ok && m != "" {
		e.Message = m
	} else {
		e.Message = fmt.Sprintf("HTTP %d", status)
	}
	return e
}

// ActivationClosedError: WaitForCode found the number expired or canceled
// before an SMS arrived. It has been refunded.
type ActivationClosedError struct{ ActivationStatus string }

func (e *ActivationClosedError) Error() string {
	return fmt.Sprintf("numberotp: the number is %s and won't receive a code; it has been refunded", e.ActivationStatus)
}

// ErrWaitTimeout: WaitForCode's own timeout passed first. The number may
// still receive a code — wait again, or cancel it for a refund.
var ErrWaitTimeout = errors.New("numberotp: timed out waiting for the code; the number is still open")
