package github

import (
	"errors"
	"os/exec"
	"testing"
	"time"
)

func TestIsRateLimited(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		want   bool
	}{
		{"primary rate limit 403", "gh: API rate limit exceeded for user ID 1234. (HTTP 403)", true},
		{"secondary rate limit 403", "gh: You have exceeded a secondary rate limit. Please wait a few minutes before you try again. (HTTP 403)", true},
		{"429", "gh: Too Many Requests (HTTP 429)", true},
		{"permission 403", "gh: Resource not accessible by integration (HTTP 403)", false},
		{"admin 403", "gh: Must have admin rights to Repository. (HTTP 403)", false},
		{"404", "gh: Not Found (HTTP 404)", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRateLimited(tt.stderr); got != tt.want {
				t.Errorf("isRateLimited() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClassifiersPreferRateLimit(t *testing.T) {
	stderr := "gh: API rate limit exceeded (HTTP 403)"
	if _, _, err := classifyProtectionResult(stderr); !errors.Is(err, ErrRateLimited) {
		t.Errorf("classifyProtectionResult err = %v, want ErrRateLimited", err)
	}
	if _, err := classifyBranchRulesError(stderr); !errors.Is(err, ErrRateLimited) {
		t.Errorf("classifyBranchRulesError err = %v, want ErrRateLimited", err)
	}
}

func testPolicy(sleeps *[]time.Duration) retryPolicy {
	return retryPolicy{maxRetries: 3, base: time.Second, max: 4 * time.Second,
		sleep: func(d time.Duration) { *sleeps = append(*sleeps, d) }}
}

func limitedErr() error {
	return &exec.ExitError{Stderr: []byte("gh: API rate limit exceeded (HTTP 403)")}
}

func TestRetrySucceedsAfterRateLimits(t *testing.T) {
	var sleeps []time.Duration
	calls := 0
	run := testPolicy(&sleeps).wrap(func(string, ...string) ([]byte, error) {
		calls++
		if calls <= 2 {
			return nil, limitedErr()
		}
		return []byte("[]"), nil
	})
	out, err := run("gh", "api", "x")
	if err != nil || string(out) != "[]" {
		t.Fatalf("got %q, %v; want success", out, err)
	}
	if calls != 3 || len(sleeps) != 2 {
		t.Errorf("calls=%d sleeps=%d, want 3 and 2", calls, len(sleeps))
	}
}

func TestRetryExhaustionIsBounded(t *testing.T) {
	var sleeps []time.Duration
	calls := 0
	run := testPolicy(&sleeps).wrap(func(string, ...string) ([]byte, error) {
		calls++
		return nil, limitedErr()
	})
	_, err := run("gh", "api", "x")
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
	if calls != 4 {
		t.Errorf("calls = %d, want 4 (1 + 3 retries)", calls)
	}
	for _, d := range sleeps {
		if d > 4*time.Second {
			t.Errorf("sleep %v exceeds cap", d)
		}
	}
}

func TestRetryDoesNotRetryOtherErrors(t *testing.T) {
	var sleeps []time.Duration
	calls := 0
	run := testPolicy(&sleeps).wrap(func(string, ...string) ([]byte, error) {
		calls++
		return nil, &exec.ExitError{Stderr: []byte("gh: Must have admin rights (HTTP 403)")}
	})
	if _, err := run("gh", "api", "x"); err == nil || errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want original permission error", err)
	}
	if calls != 1 || len(sleeps) != 0 {
		t.Errorf("calls=%d sleeps=%d, want 1 and 0", calls, len(sleeps))
	}
}

func TestReadsSurfaceRateLimitNotPermissions(t *testing.T) {
	var sleeps []time.Duration
	run := testPolicy(&sleeps).wrap(func(string, ...string) ([]byte, error) { return nil, limitedErr() })
	if _, err := getBranchRules("o", "r", "main", run); !errors.Is(err, ErrRateLimited) {
		t.Errorf("getBranchRules err = %v", err)
	}
	if _, _, err := getBranchProtection("o", "r", "main", run); !errors.Is(err, ErrRateLimited) {
		t.Errorf("getBranchProtection err = %v", err)
	}
}
