package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wdm0006/rampart/internal/config"
)

func TestRenderStepSummary(t *testing.T) {
	results := []RepoAuditResult{
		{Repo: "ok", Compliant: true},
		{Repo: "skip", Skipped: true, Error: "excluded"},
		{Repo: "drift|repo", Diffs: []config.RuleDiff{
			{Rule: "passing", Pass: true},
			{Rule: "checks|names", Want: "a|b\r\nc", Got: "x\ny\rz"},
		}},
		{Repo: "bad", Error: "<failed>|read"},
	}
	want := "\n### Rampart audit: org\n\n1 compliant, 2 non-compliant (including errors), 1 skipped out of 4 repos\n\n| Repo | Rule | Want | Got |\n| --- | --- | --- | --- |\n| drift\\|repo | checks\\|names | a\\|b<br>c | x<br>y<br>z |\n| bad | error |  | &lt;failed&gt;\\|read |\n\n"
	if got := renderStepSummary("org", results); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	for _, results := range [][]RepoAuditResult{nil, {{Repo: "ok", Compliant: true}}} {
		got := renderStepSummary("org", results)
		want := fmt.Sprintf("\n### Rampart audit: org\n\n%d compliant, 0 non-compliant (including errors), 0 skipped out of %d repos\n\n\n", len(results), len(results))
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
}

func TestRenderStepSummaryTruncation(t *testing.T) {
	for _, n := range []int{50, 53} {
		diffs := make([]config.RuleDiff, n)
		want := "\n### Rampart audit: org\n\n0 compliant, 1 non-compliant (including errors), 0 skipped out of 1 repos\n\n| Repo | Rule | Want | Got |\n| --- | --- | --- | --- |\n"
		for i := range diffs {
			diffs[i] = config.RuleDiff{Rule: fmt.Sprintf("rule%d", i), Want: "true", Got: "false"}
			if i < 50 {
				want += fmt.Sprintf("| drift | rule%d | true | false |\n", i)
			}
		}
		if n > 50 {
			want += "\n+3 more rows\n"
		}
		want += "\n"
		if got := renderStepSummary("org", []RepoAuditResult{{Repo: "drift", Diffs: diffs}}); got != want {
			t.Fatalf("n=%d: got %q, want %q", n, got, want)
		}
	}
}

func TestAppendStepSummary(t *testing.T) {
	summaryPath := filepath.Join(t.TempDir(), "summary.md")
	if err := os.WriteFile(summaryPath, []byte("existing\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_STEP_SUMMARY", summaryPath)
	var warnings bytes.Buffer
	appendStepSummary("org", nil, nil, &warnings)
	appendStepSummary("org", nil, nil, &warnings)
	got, err := os.ReadFile(summaryPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "existing\n" + strings.Repeat(renderStepSummary("org", nil), 2)
	if string(got) != want || warnings.Len() != 0 {
		t.Fatalf("got %q, warnings %q", got, warnings.String())
	}
	t.Setenv("GITHUB_STEP_SUMMARY", "")
	appendStepSummary("other", nil, nil, &warnings)
	got, err = os.ReadFile(summaryPath)
	if err != nil || string(got) != want {
		t.Fatalf("unset env changed file: %q, %v", got, err)
	}
	t.Setenv("GITHUB_STEP_SUMMARY", t.TempDir())
	appendStepSummary("org", nil, nil, &warnings)
	if !strings.Contains(warnings.String(), "Warning: could not write GitHub Actions job summary:") {
		t.Fatalf("warnings = %q", warnings.String())
	}
}

func TestAuditStepSummaryCommand(t *testing.T) {
	if os.Getenv("RAMPART_SUMMARY_TEST_HELPER") == "1" {
		getBranchProtection = func(_, _, _ string) (config.Rules, bool, error) {
			if os.Getenv("RAMPART_SUMMARY_TEST_COMPLIANT") == "true" {
				return config.Rules{RequirePullRequest: true, RequiredApprovals: 2}, true, nil
			}
			return config.NoProtectionRules(), true, nil
		}
		getBranchRules = func(_, _, _ string) (config.Rules, error) { return config.Rules{}, nil }
		var args []string
		if err := json.Unmarshal([]byte(os.Getenv("RAMPART_SUMMARY_TEST_ARGS")), &args); err != nil {
			t.Fatal(err)
		}
		rootCmd.SetArgs(args)
		if err := rootCmd.Execute(); err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	dir := t.TempDir()
	cfg := filepath.Join(dir, "policy.yaml")
	if err := os.WriteFile(cfg, []byte("branch: main\nrules:\n  require_pull_request: true\n  required_approvals: 2\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"text", "json"} {
		for _, drift := range []bool{true, false} {
			for _, writable := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/drift=%v/writable=%v", format, drift, writable), func(t *testing.T) {
					summaryPath := filepath.Join(t.TempDir(), "summary.md")
					if !writable {
						summaryPath = t.TempDir()
					}
					command := exec.Command(os.Args[0], "-test.run=^TestAuditStepSummaryCommand$")
					args, err := json.Marshal([]string{"audit", "--owner", "org", "--repo", "repo", "--config", cfg, "--format", format})
					if err != nil {
						t.Fatal(err)
					}
					command.Env = append(os.Environ(), "RAMPART_SUMMARY_TEST_HELPER=1", "GITHUB_STEP_SUMMARY="+summaryPath, "RAMPART_SUMMARY_TEST_ARGS="+string(args), fmt.Sprintf("RAMPART_SUMMARY_TEST_COMPLIANT=%v", !drift))
					var stdout, stderr bytes.Buffer
					command.Stdout, command.Stderr = &stdout, &stderr
					err = command.Run()
					if drift {
						if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
							t.Fatalf("exit = %v", err)
						}
					} else if err != nil {
						t.Fatalf("exit = %v", err)
					}
					if format == "json" && !json.Valid(stdout.Bytes()) {
						t.Fatalf("stdout is not JSON: %s", stdout.String())
					}
					if writable {
						got, err := os.ReadFile(summaryPath)
						want := "1 compliant, 0 non-compliant (including errors), 0 skipped out of 1 repos"
						if drift {
							want = "| repo | required_approvals | 2 | 0 |"
						}
						if err != nil || !strings.Contains(string(got), want) {
							t.Fatalf("summary = %s, %v", got, err)
						}
					} else if !strings.Contains(stderr.String(), "Warning: could not write") {
						t.Fatalf("stderr = %q", stderr.String())
					}
				})
			}
		}
	}
}
