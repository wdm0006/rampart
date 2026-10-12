package cli

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wdm0006/rampart/internal/config"
	"github.com/wdm0006/rampart/internal/github"
)

func stubReads(t *testing.T, protection func(repo string) (config.Rules, bool, error)) {
	t.Helper()
	origP, origR := getBranchProtection, getBranchRules
	t.Cleanup(func() { getBranchProtection, getBranchRules = origP, origR })
	getBranchProtection = func(_, repo, _ string) (config.Rules, bool, error) { return protection(repo) }
	getBranchRules = func(_, _, _ string) (config.Rules, error) { return config.Rules{}, nil }
}

func testRepos(n int) []github.Repo {
	repos := make([]github.Repo, n)
	for i := range repos {
		repos[i] = github.Repo{Name: fmt.Sprintf("repo-%02d", i), DefaultBranch: "main"}
	}
	return repos
}

func TestAuditAllPreservesOrderWhenReadsFinishOutOfOrder(t *testing.T) {
	const n = 12
	stubReads(t, func(repo string) (config.Rules, bool, error) {
		var i int
		fmt.Sscanf(repo, "repo-%d", &i)
		// earlier repos finish last
		time.Sleep(time.Duration(n-i) * 5 * time.Millisecond)
		return config.NoProtectionRules(), true, nil
	})
	repos := testRepos(n)
	for _, c := range []int{1, 2, 4, n, 50} {
		results := auditAll("acme", repos, config.Config{Branch: "default"}, nil, c)
		if len(results) != n {
			t.Fatalf("concurrency %d: len = %d", c, len(results))
		}
		for i, r := range results {
			if r.Repo != repos[i].Name {
				t.Fatalf("concurrency %d: results[%d] = %s, want %s", c, i, r.Repo, repos[i].Name)
			}
		}
	}
}

func TestAuditAllBoundsInFlightReads(t *testing.T) {
	for _, limit := range []int{1, 3} {
		var inFlight, peak int32
		var mu sync.Mutex
		stubReads(t, func(string) (config.Rules, bool, error) {
			cur := atomic.AddInt32(&inFlight, 1)
			mu.Lock()
			if cur > peak {
				peak = cur
			}
			mu.Unlock()
			time.Sleep(20 * time.Millisecond)
			atomic.AddInt32(&inFlight, -1)
			return config.NoProtectionRules(), true, nil
		})
		auditAll("acme", testRepos(12), config.Config{Branch: "default"}, nil, limit)
		if int(peak) > limit {
			t.Errorf("limit %d: peak in-flight = %d", limit, peak)
		}
		if limit > 1 && peak < 2 {
			t.Errorf("limit %d: reads never overlapped (peak %d)", limit, peak)
		}
	}
}

func TestAuditAllClassification(t *testing.T) {
	stubReads(t, func(repo string) (config.Rules, bool, error) {
		switch repo {
		case "repo-01":
			return config.Rules{}, false, nil
		case "repo-02":
			return config.Rules{}, false, fmt.Errorf("boom")
		}
		return config.NoProtectionRules(), true, nil
	})
	repos := testRepos(4)
	for _, c := range []int{1, 4} {
		res := auditAll("acme", repos, config.Config{Branch: "default"}, map[string]bool{"repo-03": true}, c)
		if res[1].Error != "insufficient permissions" || !res[1].Skipped {
			t.Errorf("c=%d permissions: %+v", c, res[1])
		}
		if res[2].Error != "boom" || res[2].Skipped {
			t.Errorf("c=%d error: %+v", c, res[2])
		}
		if res[3].Error != "excluded" || !res[3].Skipped {
			t.Errorf("c=%d excluded: %+v", c, res[3])
		}
		if res[0].Error != "" || res[0].Skipped || res[0].Branch != "main" {
			t.Errorf("c=%d ok: %+v", c, res[0])
		}
	}
}

func TestValidateConcurrency(t *testing.T) {
	for _, n := range []int{-1, 0} {
		if validateConcurrency(n) == nil {
			t.Errorf("validateConcurrency(%d) = nil", n)
		}
	}
	for _, n := range []int{1, 4, 100} {
		if err := validateConcurrency(n); err != nil {
			t.Errorf("validateConcurrency(%d) = %v", n, err)
		}
	}
}
