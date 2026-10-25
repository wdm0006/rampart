package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
)

const baselineVersion = 1

// baselineFile records, per repo, the rules that were failing when it was
// written. Repos in error or skipped state are never recorded.
type baselineFile struct {
	Version int                 `json:"version"`
	Repos   map[string][]string `json:"repos"`
}

// baselineEval is the outcome of comparing audit results with a baseline.
type baselineEval struct {
	// Baselined[repo][rule] is true for a failing rule present in the baseline.
	Baselined map[string]map[string]bool
	// BaselinedCount is the number of grandfathered rule failures.
	BaselinedCount int
	// NewFailures counts rule failures not in the baseline plus read errors.
	NewFailures int
	// Fixed lists "repo: rule" baseline entries that now pass.
	Fixed []string
}

func buildBaseline(results []RepoAuditResult) baselineFile {
	b := baselineFile{Version: baselineVersion, Repos: map[string][]string{}}
	for _, r := range results {
		if r.Skipped || r.Error != "" {
			continue
		}
		var rules []string
		for _, d := range r.Diffs {
			if !d.Pass {
				rules = append(rules, d.Rule)
			}
		}
		if len(rules) > 0 {
			sort.Strings(rules)
			b.Repos[r.Repo] = rules
		}
	}
	return b
}

func writeBaseline(path string, b baselineFile) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(b); err != nil {
		return err
	}
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("writing baseline %s: %w", path, err)
	}
	return nil
}

// loadBaseline is strict: a missing, unreadable or malformed file is an error,
// never an empty baseline.
func loadBaseline(path string) (baselineFile, error) {
	var b baselineFile
	data, err := os.ReadFile(path)
	if err != nil {
		return b, fmt.Errorf("reading baseline %s: %w", path, err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		return baselineFile{}, fmt.Errorf("invalid baseline %s: %w", path, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return baselineFile{}, fmt.Errorf("invalid baseline %s: unexpected trailing content", path)
	}
	if b.Version != baselineVersion {
		return baselineFile{}, fmt.Errorf("invalid baseline %s: unsupported version %d (want %d)", path, b.Version, baselineVersion)
	}
	if b.Repos == nil {
		return baselineFile{}, fmt.Errorf("invalid baseline %s: missing \"repos\"", path)
	}
	return b, nil
}

// evaluateBaseline classifies every failure in results. Read errors are always
// new failures, even for repos present in the baseline.
func evaluateBaseline(results []RepoAuditResult, b baselineFile) baselineEval {
	ev := baselineEval{Baselined: map[string]map[string]bool{}}
	for _, r := range results {
		if r.Skipped {
			continue
		}
		if r.Error != "" {
			ev.NewFailures++
			continue
		}
		known := map[string]bool{}
		for _, rule := range b.Repos[r.Repo] {
			known[rule] = true
		}
		failing := map[string]bool{}
		for _, d := range r.Diffs {
			if d.Pass {
				continue
			}
			failing[d.Rule] = true
			if known[d.Rule] {
				if ev.Baselined[r.Repo] == nil {
					ev.Baselined[r.Repo] = map[string]bool{}
				}
				ev.Baselined[r.Repo][d.Rule] = true
				ev.BaselinedCount++
			} else {
				ev.NewFailures++
			}
		}
		for _, rule := range b.Repos[r.Repo] {
			if !failing[rule] {
				ev.Fixed = append(ev.Fixed, r.Repo+": "+rule)
			}
		}
	}
	sort.Strings(ev.Fixed)
	return ev
}

func (ev *baselineEval) isBaselined(repo, rule string) bool {
	return ev != nil && ev.Baselined[repo][rule]
}

// readErrorCount counts non-skipped repos that failed to read.
func readErrorCount(results []RepoAuditResult) int {
	n := 0
	for _, r := range results {
		if !r.Skipped && r.Error != "" {
			n++
		}
	}
	return n
}

// printBaselineText prints the baseline section of the text output.
func printBaselineText(w io.Writer, ev baselineEval) {
	fmt.Fprintf(w, "Baseline: %d baselined, %d new failures\n", ev.BaselinedCount, ev.NewFailures)
	for _, f := range ev.Fixed {
		fmt.Fprintf(w, "  fixed - remove from baseline: %s\n", f)
	}
}
