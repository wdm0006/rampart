package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func prevRun(repos ...jsonRepo) jsonAudit {
	return jsonAudit{Owner: "o", Repos: repos}
}

func prevRepo(name, status string, diffs ...jsonDiff) jsonRepo {
	return jsonRepo{Repo: name, Status: status, Diffs: diffs}
}

func pd(rule string, pass bool) jsonDiff { return jsonDiff{Rule: rule, Pass: pass} }

func TestEvaluateCompare(t *testing.T) {
	prev := prevRun(
		prevRepo("drifted", "compliant", pd("a", true), pd("b", true)),
		prevRepo("stable", "non_compliant", pd("a", false), pd("b", true)),
		prevRepo("fixed", "non_compliant", pd("a", false), pd("b", false)),
		prevRepo("was-err", "error"),
		prevRepo("now-err", "non_compliant", pd("a", false)),
		prevRepo("was-skip", "skipped"),
		prevRepo("gone", "non_compliant", pd("a", false)),
	)
	results := []RepoAuditResult{
		failing("drifted", "b"),
		failing("stable", "a"),
		passing("fixed", "a", "b"),
		failing("was-err", "a"),
		{Repo: "now-err", Error: "boom"},
		passing("was-skip", "a"),
		failing("fresh", "a", "b"),
		passing("fresh-ok", "a"),
	}
	ev := evaluateCompare(results, prev)
	wantChanges := map[string]repoChange{
		"drifted": {NewlyFailing: []string{"b"}},
		"fixed":   {NewlyFixed: []string{"a", "b"}},
		"fresh":   {NewlyFailing: []string{"a", "b"}},
	}
	if !reflect.DeepEqual(ev.Changes, wantChanges) {
		t.Errorf("changes = %+v, want %+v", ev.Changes, wantChanges)
	}
	if want := []string{"fresh", "fresh-ok"}; !reflect.DeepEqual(ev.NewRepos, want) {
		t.Errorf("new repos = %v, want %v", ev.NewRepos, want)
	}
	if want := []string{"gone"}; !reflect.DeepEqual(ev.RemovedRepos, want) {
		t.Errorf("removed repos = %v, want %v", ev.RemovedRepos, want)
	}
}

func TestEvaluateCompareNeverFixedFromUnreadable(t *testing.T) {
	prev := prevRun(prevRepo("r", "error"), prevRepo("s", "skipped"))
	ev := evaluateCompare([]RepoAuditResult{passing("r", "a"), passing("s", "a")}, prev)
	if len(ev.Changes) != 0 {
		t.Errorf("unreadable previous state produced changes: %+v", ev.Changes)
	}
}

func TestEvaluateCompareRuleDroppedFromPolicyIsNotFixed(t *testing.T) {
	prev := prevRun(prevRepo("r", "non_compliant", pd("a", false)))
	ev := evaluateCompare([]RepoAuditResult{passing("r", "b")}, prev)
	if len(ev.Changes) != 0 {
		t.Errorf("rule absent from current diffs reported: %+v", ev.Changes)
	}
}

func TestBuildAuditJSONCompare(t *testing.T) {
	prev := prevRun(prevRepo("a", "compliant", pd("x", true)), prevRepo("gone", "compliant"))
	results := []RepoAuditResult{failing("a", "x"), passing("b", "x")}
	ev := evaluateCompare(results, prev)
	out := buildAuditJSON("o", "c", "main", results, nil, &ev)
	if !reflect.DeepEqual(out.Repos[0].NewlyFailing, []string{"x"}) || out.Repos[0].NewlyFixed != nil {
		t.Errorf("repo a = %+v", out.Repos[0])
	}
	if out.Repos[1].NewlyFailing != nil || out.Repos[1].NewlyFixed != nil {
		t.Errorf("repo b = %+v", out.Repos[1])
	}
	if out.Compare == nil || !reflect.DeepEqual(out.Compare.NewRepos, []string{"b"}) || !reflect.DeepEqual(out.Compare.RemovedRepos, []string{"gone"}) {
		t.Errorf("compare = %+v", out.Compare)
	}
	plain := buildAuditJSON("o", "c", "main", results, nil, nil)
	if plain.Compare != nil || plain.Repos[0].NewlyFailing != nil {
		t.Errorf("compare fields leaked without --compare-to: %+v", plain)
	}
}

func TestCompareRoundTrip(t *testing.T) {
	results := []RepoAuditResult{failing("a", "x"), {Repo: "e", Error: "boom"}}
	path := filepath.Join(t.TempDir(), "audit.json")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	ev := compareEval{Changes: map[string]repoChange{"a": {NewlyFailing: []string{"x"}}}}
	if err := writeAuditJSON(f, buildAuditJSON("o", "c", "main", results, nil, &ev)); err != nil {
		t.Fatal(err)
	}
	f.Close()
	got, err := loadPreviousRun(path)
	if err != nil {
		t.Fatalf("own output rejected: %v", err)
	}
	if len(got.Repos) != 2 || got.Repos[1].Status != "error" {
		t.Errorf("loaded = %+v", got)
	}
}

func TestLoadPreviousRunRejects(t *testing.T) {
	cases := map[string]string{
		"empty":         ``,
		"not json":      `nope`,
		"baseline file": `{"version":1,"repos":{"a":["x"]}}`,
		"unknown field": `{"owner":"o","repos":[],"bogus":1}`,
		"no repos":      `{"owner":"o"}`,
		"bad status":    `{"repos":[{"repo":"a","status":"weird"}]}`,
		"duplicate":     `{"repos":[{"repo":"a","status":"error"},{"repo":"a","status":"error"}]}`,
		"trailing":      `{"repos":[]} {"repos":[]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "p.json")
			os.WriteFile(p, []byte(body), 0644)
			if _, err := loadPreviousRun(p); err == nil {
				t.Errorf("accepted %s", body)
			}
		})
	}
	if _, err := loadPreviousRun(filepath.Join(t.TempDir(), "missing.json")); err == nil || !strings.Contains(err.Error(), "compare file") {
		t.Errorf("missing file err = %v", err)
	}
}

func TestStepSummaryCompare(t *testing.T) {
	results := []RepoAuditResult{failing("a", "x")}
	ev := evaluateCompare(results, prevRun(prevRepo("a", "compliant", pd("x", true))))
	s := renderStepSummaryFull("o", results, nil, &ev)
	if !strings.Contains(s, "newly failing: a: x") || !strings.Contains(s, "0 new repos, 0 removed repos") {
		t.Errorf("summary = %s", s)
	}
	if strings.Contains(renderStepSummaryFull("o", results, nil, nil), "previous run") {
		t.Error("compare section without --compare-to")
	}
}
