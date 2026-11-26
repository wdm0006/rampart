package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
)

// repoChange is the rule-level movement of one repo since the previous run.
type repoChange struct {
	NewlyFailing []string
	NewlyFixed   []string
}

// compareEval is the outcome of comparing audit results with a previous run.
type compareEval struct {
	Changes      map[string]repoChange
	NewRepos     []string
	RemovedRepos []string
}

// jsonCompare is the additive top-level JSON section for --compare-to.
type jsonCompare struct {
	NewRepos     []string `json:"new_repos"`
	RemovedRepos []string `json:"removed_repos"`
}

// loadPreviousRun is strict: a missing, unreadable or malformed file, or one
// that is not an audit --format json document, is an error.
func loadPreviousRun(path string) (jsonAudit, error) {
	var a jsonAudit
	data, err := os.ReadFile(path)
	if err != nil {
		return a, fmt.Errorf("reading compare file %s: %w", path, err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&a); err != nil {
		return jsonAudit{}, fmt.Errorf("invalid compare file %s: %w", path, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return jsonAudit{}, fmt.Errorf("invalid compare file %s: unexpected trailing content", path)
	}
	if a.Repos == nil {
		return jsonAudit{}, fmt.Errorf("invalid compare file %s: missing \"repos\" (not an audit --format json document?)", path)
	}
	seen := map[string]bool{}
	for _, r := range a.Repos {
		switch r.Status {
		case "compliant", "non_compliant", "skipped", "error":
		default:
			return jsonAudit{}, fmt.Errorf("invalid compare file %s: repo %q has unknown status %q", path, r.Repo, r.Status)
		}
		if r.Repo == "" || seen[r.Repo] {
			return jsonAudit{}, fmt.Errorf("invalid compare file %s: empty or duplicate repo name %q", path, r.Repo)
		}
		seen[r.Repo] = true
	}
	return a, nil
}

// ruleStates maps rule -> failing for a repo whose state was actually read.
// ok is false for error/skipped repos, whose state is unknown.
func ruleStates(status string, diffs map[string]bool) (map[string]bool, bool) {
	if status != "compliant" && status != "non_compliant" {
		return nil, false
	}
	return diffs, true
}

// evaluateCompare diffs results against a previous run. Rules are only
// reported as newly failing or newly fixed when the repo was readable in both
// runs, so an unreadable previous state is never read as "was failing".
// A repo new to the fleet reports all its failing rules as newly failing.
func evaluateCompare(results []RepoAuditResult, prev jsonAudit) compareEval {
	ev := compareEval{Changes: map[string]repoChange{}}
	prevRepos := map[string]jsonRepo{}
	for _, r := range prev.Repos {
		prevRepos[r.Repo] = r
	}
	current := map[string]bool{}
	for _, r := range results {
		current[r.Repo] = true
		curStates, curOK := ruleStates(repoStatus(r), diffStates(resultDiffStates(r)))
		if !curOK {
			continue
		}
		p, existed := prevRepos[r.Repo]
		var prevStates map[string]bool
		prevOK := false
		if existed {
			prevStates, prevOK = ruleStates(p.Status, diffStates(jsonDiffStates(p)))
		} else {
			ev.NewRepos = append(ev.NewRepos, r.Repo)
			prevStates, prevOK = map[string]bool{}, true
		}
		if !prevOK {
			continue
		}
		var ch repoChange
		for rule, failing := range curStates {
			was, had := prevStates[rule]
			if failing && (!had || !was) {
				ch.NewlyFailing = append(ch.NewlyFailing, rule)
			}
			if !failing && had && was {
				ch.NewlyFixed = append(ch.NewlyFixed, rule)
			}
		}
		sort.Strings(ch.NewlyFailing)
		sort.Strings(ch.NewlyFixed)
		if len(ch.NewlyFailing) > 0 || len(ch.NewlyFixed) > 0 {
			ev.Changes[r.Repo] = ch
		}
	}
	for _, p := range prev.Repos {
		if !current[p.Repo] {
			ev.RemovedRepos = append(ev.RemovedRepos, p.Repo)
		}
	}
	return ev
}

type ruleState struct {
	rule    string
	failing bool
}

func diffStates(in []ruleState) map[string]bool {
	m := map[string]bool{}
	for _, s := range in {
		m[s.rule] = s.failing
	}
	return m
}

func resultDiffStates(r RepoAuditResult) []ruleState {
	var out []ruleState
	for _, d := range r.Diffs {
		out = append(out, ruleState{d.Rule, !d.Pass})
	}
	return out
}

func jsonDiffStates(r jsonRepo) []ruleState {
	var out []ruleState
	for _, d := range r.Diffs {
		out = append(out, ruleState{d.Rule, !d.Pass})
	}
	return out
}

func (ev *compareEval) hasChanges() bool {
	return ev != nil && (len(ev.Changes) > 0 || len(ev.NewRepos) > 0 || len(ev.RemovedRepos) > 0)
}

// printCompareText prints the compare section of the text output.
func printCompareText(w io.Writer, ev compareEval, results []RepoAuditResult) {
	fmt.Fprintf(w, "Compared with previous run: %d new repos, %d removed repos\n", len(ev.NewRepos), len(ev.RemovedRepos))
	for _, line := range compareLines(ev, results) {
		fmt.Fprintf(w, "  %s\n", line)
	}
}

// compareLines lists changes in discovery order, then removed repos.
func compareLines(ev compareEval, results []RepoAuditResult) []string {
	var lines []string
	newSet := map[string]bool{}
	for _, n := range ev.NewRepos {
		newSet[n] = true
	}
	for _, r := range results {
		ch, ok := ev.Changes[r.Repo]
		if !ok {
			if newSet[r.Repo] {
				lines = append(lines, "new repo: "+r.Repo)
			}
			continue
		}
		prefix := ""
		if newSet[r.Repo] {
			prefix = "new repo, "
		}
		for _, rule := range ch.NewlyFailing {
			lines = append(lines, fmt.Sprintf("%snewly failing: %s: %s", prefix, r.Repo, rule))
		}
		for _, rule := range ch.NewlyFixed {
			lines = append(lines, fmt.Sprintf("newly fixed: %s: %s", r.Repo, rule))
		}
	}
	for _, n := range ev.RemovedRepos {
		lines = append(lines, "removed repo: "+n)
	}
	return lines
}
