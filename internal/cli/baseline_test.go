package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/wdm0006/rampart/internal/config"
)

func failing(repo string, rules ...string) RepoAuditResult {
	r := RepoAuditResult{Repo: repo, Branch: "main"}
	for _, rule := range rules {
		r.Diffs = append(r.Diffs, config.RuleDiff{Rule: rule, Pass: false, Want: "true", Got: "false"})
	}
	return r
}

func passing(repo string, rules ...string) RepoAuditResult {
	r := RepoAuditResult{Repo: repo, Branch: "main", Compliant: true}
	for _, rule := range rules {
		r.Diffs = append(r.Diffs, config.RuleDiff{Rule: rule, Pass: true, Want: "true", Got: "true"})
	}
	return r
}

func TestBuildBaseline(t *testing.T) {
	results := []RepoAuditResult{
		failing("a", "required_approvals", "enforce_admins"),
		passing("ok", "enforce_admins"),
		{Repo: "err", Error: "boom"},
		{Repo: "skip", Skipped: true, Error: "excluded"},
		{Repo: "mixed", Diffs: []config.RuleDiff{{Rule: "x", Pass: true}, {Rule: "y", Pass: false}}},
	}
	want := map[string][]string{
		"a":     {"enforce_admins", "required_approvals"},
		"mixed": {"y"},
	}
	got := buildBaseline(results)
	if got.Version != 1 || !reflect.DeepEqual(got.Repos, want) {
		t.Fatalf("got %+v, want repos %v", got, want)
	}
}

func TestEvaluateBaseline(t *testing.T) {
	base := baselineFile{Version: 1, Repos: map[string][]string{
		"drifted": {"enforce_admins", "required_approvals"},
		"errored": {"enforce_admins"},
		"gone":    {"enforce_admins"},
	}}
	tests := []struct {
		name      string
		results   []RepoAuditResult
		baselined int
		newFail   int
		fixed     []string
	}{
		{"unchanged state", []RepoAuditResult{failing("drifted", "enforce_admins", "required_approvals")}, 2, 0, nil},
		{"new rule on baselined repo", []RepoAuditResult{failing("drifted", "enforce_admins", "required_approvals", "allow_deletions")}, 2, 1, nil},
		{"new failing repo", []RepoAuditResult{failing("fresh", "enforce_admins")}, 0, 1, nil},
		{"read error on baselined repo", []RepoAuditResult{{Repo: "errored", Error: "boom"}}, 0, 1, nil},
		{"fixed rule listed", []RepoAuditResult{failing("drifted", "required_approvals")}, 1, 0, []string{"drifted: enforce_admins"}},
		{"fully compliant repo lists all as fixed", []RepoAuditResult{passing("drifted", "enforce_admins", "required_approvals")}, 0, 0,
			[]string{"drifted: enforce_admins", "drifted: required_approvals"}},
		{"skipped repo neither fixed nor failing", []RepoAuditResult{{Repo: "drifted", Skipped: true, Error: "excluded"}}, 0, 0, nil},
		{"repo absent from run is not fixed", []RepoAuditResult{passing("other", "x")}, 0, 0, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := evaluateBaseline(tt.results, base)
			if ev.BaselinedCount != tt.baselined || ev.NewFailures != tt.newFail || !reflect.DeepEqual(ev.Fixed, tt.fixed) {
				t.Errorf("got baselined=%d new=%d fixed=%v; want %d %d %v",
					ev.BaselinedCount, ev.NewFailures, ev.Fixed, tt.baselined, tt.newFail, tt.fixed)
			}
		})
	}
}

func TestEvaluateBaselinePerRuleMarks(t *testing.T) {
	base := baselineFile{Version: 1, Repos: map[string][]string{"r": {"a"}}}
	ev := evaluateBaseline([]RepoAuditResult{failing("r", "a", "b")}, base)
	if !ev.isBaselined("r", "a") || ev.isBaselined("r", "b") || ev.isBaselined("other", "a") {
		t.Errorf("unexpected marks: %v", ev.Baselined)
	}
	var none *baselineEval
	if none.isBaselined("r", "a") {
		t.Error("nil eval must never baseline")
	}
}

func TestBaselineRoundTripUnchanged(t *testing.T) {
	results := []RepoAuditResult{failing("a", "x", "y"), passing("b", "x"), {Repo: "e", Error: "boom"}}
	path := filepath.Join(t.TempDir(), "baseline.json")
	if err := writeBaseline(path, buildBaseline(results)); err != nil {
		t.Fatal(err)
	}
	base, err := loadBaseline(path)
	if err != nil {
		t.Fatal(err)
	}
	ev := evaluateBaseline(results, base)
	// the read error is not in the file, so it is the only new failure
	if ev.BaselinedCount != 2 || ev.NewFailures != 1 || len(ev.Fixed) != 0 {
		t.Errorf("got %+v", ev)
	}
	if _, ok := base.Repos["e"]; ok {
		t.Error("errored repo must not be baselined")
	}
}

func TestLoadBaselineErrors(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name, content, msg string
		missing            bool
	}{
		{name: "missing file", missing: true, msg: "reading baseline"},
		{name: "empty file", content: "", msg: "invalid baseline"},
		{name: "not json", content: "nope", msg: "invalid baseline"},
		{name: "unknown field", content: `{"version":1,"repos":{},"extra":1}`, msg: "invalid baseline"},
		{name: "wrong version", content: `{"version":2,"repos":{}}`, msg: "unsupported version"},
		{name: "no version", content: `{"repos":{}}`, msg: "unsupported version"},
		{name: "no repos", content: `{"version":1}`, msg: "missing"},
		{name: "wrong type", content: `{"version":1,"repos":{"a":"x"}}`, msg: "invalid baseline"},
		{name: "trailing content", content: `{"version":1,"repos":{}} {}`, msg: "trailing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(dir, strings.ReplaceAll(tt.name, " ", "_"))
			if !tt.missing {
				if err := os.WriteFile(path, []byte(tt.content), 0644); err != nil {
					t.Fatal(err)
				}
			}
			_, err := loadBaseline(path)
			if err == nil || !strings.Contains(err.Error(), tt.msg) {
				t.Errorf("got %v, want error containing %q", err, tt.msg)
			}
		})
	}
}

func TestLoadBaselineEmptyReposValid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "b.json")
	os.WriteFile(path, []byte(`{"version":1,"repos":{}}`), 0644)
	if _, err := loadBaseline(path); err != nil {
		t.Errorf("empty repos map is a valid baseline: %v", err)
	}
}

func TestReadErrorCount(t *testing.T) {
	got := readErrorCount([]RepoAuditResult{{Repo: "e", Error: "x"}, {Repo: "s", Skipped: true, Error: "excluded"}, passing("p")})
	if got != 1 {
		t.Errorf("got %d, want 1", got)
	}
}

func TestAuditJSONBaselined(t *testing.T) {
	results := []RepoAuditResult{failing("r", "a", "b")}
	base := baselineFile{Version: 1, Repos: map[string][]string{"r": {"a", "gone"}}}
	ev := evaluateBaseline(results, base)
	out := buildAuditJSON("o", "c", "main", results, &ev)
	d := out.Repos[0].Diffs
	if !d[0].Baselined || d[1].Baselined {
		t.Errorf("baselined flags: %+v", d)
	}
	if !reflect.DeepEqual(out.BaselineFixed, []string{"r: gone"}) {
		t.Errorf("fixed: %v", out.BaselineFixed)
	}
	plain := buildAuditJSON("o", "c", "main", results, nil)
	if plain.Repos[0].Diffs[0].Baselined || plain.BaselineFixed != nil {
		t.Error("no baseline: nothing baselined")
	}
}

func TestStepSummaryBaseline(t *testing.T) {
	results := []RepoAuditResult{failing("r", "a", "b")}
	ev := evaluateBaseline(results, baselineFile{Version: 1, Repos: map[string][]string{"r": {"a", "gone"}}})
	out := renderStepSummaryBaseline("o", results, &ev)
	for _, want := range []string{"Baseline: 1 baselined, 1 new failures", "fixed - remove from baseline: r: gone", "| r | a (baselined) |", "| r | b |"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestAuditFailing(t *testing.T) {
	base := baselineFile{Version: 1, Repos: map[string][]string{"a": {"x"}, "e": {"x"}}}
	tests := []struct {
		name     string
		results  []RepoAuditResult
		baseline bool
		writing  bool
		want     bool
	}{
		{"default: drift fails", []RepoAuditResult{failing("a", "x")}, false, false, true},
		{"default: compliant passes", []RepoAuditResult{passing("a", "x")}, false, false, false},
		{"baseline: grandfathered passes", []RepoAuditResult{failing("a", "x")}, true, false, false},
		{"baseline: new rule fails", []RepoAuditResult{failing("a", "x", "y")}, true, false, true},
		{"baseline: new repo fails", []RepoAuditResult{failing("n", "x")}, true, false, true},
		{"baseline: read error on baselined repo fails", []RepoAuditResult{{Repo: "e", Error: "boom"}}, true, false, true},
		{"write: drift alone passes", []RepoAuditResult{failing("a", "x")}, false, true, false},
		{"write: read error fails", []RepoAuditResult{{Repo: "e", Error: "boom"}}, false, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ev *baselineEval
			if tt.baseline {
				e := evaluateBaseline(tt.results, base)
				ev = &e
			}
			if got := auditFailing(tt.results, ev, tt.writing); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
