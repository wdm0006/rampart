package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wdm0006/rampart/internal/config"
)

func TestValidateFormat(t *testing.T) {
	for _, f := range []string{"text", "json"} {
		if err := validateFormat(f); err != nil {
			t.Errorf("%q: unexpected error %v", f, err)
		}
	}
	for _, f := range []string{"", "yaml", "JSON"} {
		if err := validateFormat(f); err == nil || !strings.Contains(err.Error(), "invalid --format") {
			t.Errorf("%q: want invalid format error, got %v", f, err)
		}
	}
}

func TestAuditJSONExact(t *testing.T) {
	results := []RepoAuditResult{
		{Repo: "ok", Branch: "main", Compliant: true, Diffs: []config.RuleDiff{{Rule: "require_pull_request", Pass: true, Want: "true", Got: "true"}}},
		{Repo: "drift", Branch: "main", Diffs: []config.RuleDiff{
			{Rule: "required_approvals", Pass: false, Want: "2", Got: "1"},
			{Rule: "required_checks", Pass: true, Want: "[b a]", Got: "[b a]"},
		}},
		{Repo: "skip", Skipped: true, Error: "excluded"},
		{Repo: "bad", Branch: "main", Error: "boom"},
	}
	var buf bytes.Buffer
	if err := writeAuditJSON(&buf, buildAuditJSON("org", "rampart.yaml", "default", results)); err != nil {
		t.Fatal(err)
	}
	want := `{
  "owner": "org",
  "config": "rampart.yaml",
  "branch": "default",
  "summary": {
    "compliant": 1,
    "non_compliant": 2,
    "skipped": 1,
    "total": 4
  },
  "repos": [
    {
      "repo": "ok",
      "branch": "main",
      "status": "compliant",
      "error": "",
      "diffs": [
        {
          "rule": "require_pull_request",
          "pass": true,
          "want": "true",
          "got": "true"
        }
      ]
    },
    {
      "repo": "drift",
      "branch": "main",
      "status": "non_compliant",
      "error": "",
      "diffs": [
        {
          "rule": "required_approvals",
          "pass": false,
          "want": "2",
          "got": "1"
        },
        {
          "rule": "required_checks",
          "pass": true,
          "want": "[b a]",
          "got": "[b a]"
        }
      ]
    },
    {
      "repo": "skip",
      "branch": "",
      "status": "skipped",
      "error": "excluded",
      "diffs": []
    },
    {
      "repo": "bad",
      "branch": "main",
      "status": "error",
      "error": "boom",
      "diffs": []
    }
  ]
}
`
	if buf.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", buf.String(), want)
	}
	var back jsonAudit
	if err := json.Unmarshal(buf.Bytes(), &back); err != nil {
		t.Fatal(err)
	}
}

func TestAuditJSONSummaryMatchesTerminalRule(t *testing.T) {
	results := []RepoAuditResult{
		{Repo: "a", Compliant: true}, {Repo: "b"}, {Repo: "c", Error: "x"}, {Repo: "d", Skipped: true},
	}
	s := buildAuditJSON("o", "c", "b", results).Summary
	want := jsonSummary{Compliant: 1, NonCompliant: 2, Skipped: 1, Total: 4}
	if s != want {
		t.Fatalf("got %+v want %+v", s, want)
	}
}
