package github

import (
	"errors"
	"math/rand"
	"os/exec"
	"strings"
	"time"
)

// ErrRateLimited is returned when a read is still rate limited after retries.
var ErrRateLimited = errors.New("GitHub API rate limit exceeded; retry later or lower --concurrency")

// isRateLimited reports whether gh stderr describes a GitHub primary or
// secondary rate limit (HTTP 403 or 429 with a rate-limit message).
func isRateLimited(stderr string) bool {
	s := strings.ToLower(stderr)
	return strings.Contains(s, "rate limit") ||
		strings.Contains(s, "http 429") ||
		strings.Contains(s, "too many requests")
}

// retryPolicy retries read calls that hit a rate limit. gh does not surface
// Retry-After or x-ratelimit-reset on failure, so the wait is capped
// exponential backoff with jitter.
type retryPolicy struct {
	maxRetries int
	base, max  time.Duration
	sleep      func(time.Duration)
}

func defaultRetryPolicy() retryPolicy {
	return retryPolicy{maxRetries: 3, base: 2 * time.Second, max: 30 * time.Second, sleep: time.Sleep}
}

func (p retryPolicy) backoff(attempt int) time.Duration {
	d := p.base << uint(attempt)
	if d > p.max || d <= 0 {
		d = p.max
	}
	return d/2 + time.Duration(rand.Int63n(int64(d/2)+1))
}

// wrap returns a runner that retries rate-limited calls and returns
// ErrRateLimited once retries are exhausted. Other errors pass through.
func (p retryPolicy) wrap(run commandRunner) commandRunner {
	return func(name string, args ...string) ([]byte, error) {
		for attempt := 0; ; attempt++ {
			out, err := run(name, args...)
			exitErr, ok := err.(*exec.ExitError)
			if err == nil || !ok || !isRateLimited(string(exitErr.Stderr)) {
				return out, err
			}
			if attempt >= p.maxRetries {
				return nil, ErrRateLimited
			}
			p.sleep(p.backoff(attempt))
		}
	}
}
