// Package wrapperapi is the protocol between shelf and the supervisor in the
// shelf-wrapper image: the control API's routes, requests and answers. Both
// sides import it, so the protocol lives in one place. It imports nothing of
// shelf's, because the supervisor is a separate static binary.
package wrapperapi

import (
	"regexp"
	"time"
)

// Routes of the control API.
const (
	PathState   = "/v1/state"
	PathLogin   = "/v1/login"
	PathCode    = "/v1/code"
	PathRestart = "/v1/restart"
	PathLog     = "/v1/log"
)

// State is what the wrapper is doing.
type State string

const (
	// Starting: service mode was started and doesn't answer yet.
	Starting State = "starting"
	// Running: service mode answers and its /status lists a region.
	Running State = "running"
	// SignedOut: service mode answers, but /status lists no region.
	SignedOut State = "signed_out"
	// SigningIn: a sign-in runs and hasn't asked for a code, or has its code.
	SigningIn State = "signing_in"
	// AwaitingCode: wrapper-lite asked for the 2FA code.
	AwaitingCode State = "awaiting_code"
	// Error: the last sign-in failed, or service mode exited by itself.
	Error State = "error"
)

// Status is the answer of GET /v1/state.
type Status struct {
	State State `json:"state"`
	// Storefront is the account's region, such as "ru", while running.
	Storefront string    `json:"storefront"`
	Since      time.Time `json:"since"`
	// Error says why the state is error.
	Error string `json:"error"`
	// CodeDeadline is when wrapper-lite stops waiting for the code, while
	// awaiting_code.
	CodeDeadline *time.Time `json:"code_deadline"`
}

// Busy reports whether a sign-in runs.
func (s Status) Busy() bool { return s.State == SigningIn || s.State == AwaitingCode }

// LoginRequest is the body of POST /v1/login.
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// CodeRequest is the body of POST /v1/code.
type CodeRequest struct {
	Code string `json:"code"`
}

// CodePattern is a valid 2FA code: six digits.
var CodePattern = regexp.MustCompile(`^[0-9]{6}$`)

// LogLine is one line of wrapper-lite's output, or one of the supervisor's
// own notes about it.
type LogLine struct {
	// N counts lines from 1 since the supervisor started.
	N    int64     `json:"n"`
	Time time.Time `json:"time"`
	Text string    `json:"text"`
}

// LogPage is the answer of GET /v1/log?after=N: the kept lines after N.
type LogPage struct {
	Lines []LogLine `json:"lines"`
}

// ErrorBody is every answer that isn't 2xx.
type ErrorBody struct {
	Error string `json:"error"`
}
