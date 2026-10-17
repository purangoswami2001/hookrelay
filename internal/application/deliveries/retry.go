package deliveries

import (
	"math"
	"math/rand"
	"time"
)

const (
	DefaultMaxAttempts = 8
	BaseRetryDelay     = 2 * time.Second
	MaxRetryDelay      = 30 * time.Minute
)

// RetryDelay calculates exponential backoff with jitter
func RetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}

	exponent := math.Min(float64(attempt-1), 20)
	delay := time.Duration(float64(BaseRetryDelay) * math.Pow(2, exponent))

	if delay > MaxRetryDelay {
		delay = MaxRetryDelay
	}

	// equal jitter: half deterministic + half randomized
	jitter := time.Duration(rand.Int63n(int64(delay/2) + 1))
	delay = delay/2 + jitter

	if delay > MaxRetryDelay {
		return MaxRetryDelay
	}

	return delay
}

// ShouldRetry checks if status code or error is transient
func ShouldRetry(statusCode int, requestErr error) bool {
	if requestErr != nil {
		return true // network drops, dial timeouts, resets
	}

	switch {
	case statusCode >= 200 && statusCode < 300:
		return false
	case statusCode == 408 || statusCode == 429:
		return true // timeout or rate limited
	case statusCode >= 500 && statusCode <= 599:
		return true // receiver server errors
	default:
		return false // 4xx client errors are fatal
	}
}

// retryAfter := parseRetryAfter(respHeader)
