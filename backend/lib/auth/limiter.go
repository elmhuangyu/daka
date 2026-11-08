package auth

import "time"

// FailureLimiter throttles brute-force attempts on login / reauth. It is
// satisfied by ratelimit.AuthFailureLimiter; prefix distinguishes the endpoint.
type FailureLimiter interface {
	// Check reports whether an attempt may proceed; when locked out,
	// retryAfter is the remaining lockout.
	Check(ip, prefix string) (allowed bool, retryAfter time.Duration)
	// Failure records a failed attempt.
	Failure(ip, prefix string)
	// Success clears the failure state.
	Success(ip, prefix string)
}

// NopLimiter allows everything.
type NopLimiter struct{}

// Check implements FailureLimiter.
func (NopLimiter) Check(string, string) (bool, time.Duration) { return true, 0 }

// Failure implements FailureLimiter.
func (NopLimiter) Failure(string, string) {}

// Success implements FailureLimiter.
func (NopLimiter) Success(string, string) {}
