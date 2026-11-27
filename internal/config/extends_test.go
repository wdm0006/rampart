package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writePolicy(t *testing.T, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadExtendsMerge(t *testing.T) {
	baseRules := Rules{RequirePullRequest: true, RequiredApprovals: 3, EnforceAdmins: true, RequiredChecks: []string{"build"}, Restrictions: &Restrictions{Users: []string{"alice"}}}
	tests := []struct {
		name, child string
		branch      string
		stricter    bool
		rules       Rules
	}{
		{"omitted", "", "release", true, baseRules},
		{"scalars", "branch: main\nallow_stricter_rules: false\n", "main", false, baseRules},
		{"empty branch", "branch: ''\n", "default", true, baseRules},
		{"false and zero", "rules:\n  require_pull_request: false\n  required_approvals: 0\n", "release", true, Rules{EnforceAdmins: true, RequiredChecks: []string{"build"}, Restrictions: baseRules.Restrictions}},
		{"empty checks", "rules:\n  required_checks: []\n", "release", true, Rules{RequirePullRequest: true, RequiredApprovals: 3, EnforceAdmins: true, RequiredChecks: []string{}, Restrictions: baseRules.Restrictions}},
		{"null restrictions", "rules:\n  restrictions: null\n", "release", true, Rules{RequirePullRequest: true, RequiredApprovals: 3, EnforceAdmins: true, RequiredChecks: []string{"build"}}},
		{"empty restrictions", "rules:\n  restrictions: {}\n", "release", true, Rules{RequirePullRequest: true, RequiredApprovals: 3, EnforceAdmins: true, RequiredChecks: []string{"build"}, Restrictions: &Restrictions{}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writePolicy(t, filepath.Join(dir, "base.yaml"), "branch: release\nallow_stricter_rules: true\nrules:\n  require_pull_request: true\n  required_approvals: 3\n  enforce_admins: true\n  required_checks: [build]\n  restrictions:\n    users: [alice]\n")
			child := filepath.Join(dir, "nested", "child.yaml")
			writePolicy(t, child, "extends: ../base.yaml\n"+tt.child)
			got, err := Load(child)
			if err != nil {
				t.Fatal(err)
			}
			want := Config{Branch: tt.branch, AllowStricterRules: tt.stricter, Rules: tt.rules}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("got %#v, want %#v", got, want)
			}
		})
	}
}

func TestLoadExtendsOrderingAndAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base.yaml")
	writePolicy(t, base, "rules:\n  enforce_admins: true\noverrides:\n  - repos: ['*']\n    rules:\n      required_approvals: 2\n      require_pull_request: true\n")
	middle := filepath.Join(dir, "middle.yaml")
	writePolicy(t, middle, "extends: base.yaml\noverrides:\n  - repos: ['*']\n    rules:\n      required_approvals: 3\n")
	child := filepath.Join(dir, "child.yaml")
	writePolicy(t, child, fmt.Sprintf("extends: %q\noverrides:\n  - repos: ['*']\n    rules:\n      required_approvals: 4\n", middle))
	cfg, err := Load(child)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Overrides) != 3 {
		t.Fatalf("got %d overrides", len(cfg.Overrides))
	}
	for i, o := range cfg.Overrides {
		if *o.Rules.RequiredApprovals != i+2 {
			t.Errorf("override %d: %#v", i, o)
		}
	}
	want := Rules{EnforceAdmins: true, RequirePullRequest: true, RequiredApprovals: 4, RequiredChecks: []string{}}
	if got := cfg.RulesForRepo("widget"); !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v want %#v", got, want)
	}
}

func TestLoadExtendsErrors(t *testing.T) {
	for _, tt := range []struct{ name, base, diagnostic string }{
		{"missing", "", "missing.yaml"},
		{"unknown top key", "typo: true\n", "typo"},
		{"unknown rule", "rules:\n  typo: true\n", "typo"},
		{"unknown restriction", "rules:\n  restrictions:\n    typo: []\n", "typo"},
		{"unknown override", "overrides:\n  - repos: ['*']\n    rules:\n      typo: true\n", "typo"},
		{"bad glob", "overrides:\n  - repos: ['[']\n", "invalid repo pattern"},
		{"multiple documents", "branch: main\n---\n", "multiple YAML documents"},
		{"cycle", "extends: child.yaml\n", "cycle"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			base := "base.yaml"
			if tt.name == "missing" {
				base = "missing.yaml"
			} else {
				writePolicy(t, filepath.Join(dir, base), tt.base)
			}
			child := filepath.Join(dir, "child.yaml")
			writePolicy(t, child, "extends: "+base+"\n")
			_, err := Load(child)
			if err == nil {
				t.Fatal("expected error")
			}
			for _, part := range []string{base, "child.yaml", tt.diagnostic} {
				if !strings.Contains(err.Error(), part) {
					t.Errorf("%q missing %q", err, part)
				}
			}
		})
	}
}

func TestLoadExtendsDepth(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < maxPolicyDepth; i++ {
		writePolicy(t, filepath.Join(dir, fmt.Sprintf("%d.yaml", i)), fmt.Sprintf("extends: %d.yaml\n", i+1))
	}
	leaf := filepath.Join(dir, fmt.Sprintf("%d.yaml", maxPolicyDepth))
	writePolicy(t, leaf, "branch: release\n")
	cfg, err := Load(filepath.Join(dir, "1.yaml"))
	if err != nil || cfg.Branch != "release" {
		t.Fatalf("boundary: %#v, %v", cfg, err)
	}
	_, err = Load(filepath.Join(dir, "0.yaml"))
	if err == nil {
		t.Fatal("expected depth error")
	}
	for _, part := range []string{"depth exceeds 5", "0.yaml", "5.yaml"} {
		if !strings.Contains(err.Error(), part) {
			t.Errorf("%q missing %q", err, part)
		}
	}
}

func TestLoadExtendsSymlinkCycle(t *testing.T) {
	dir := t.TempDir()
	child := filepath.Join(dir, "child.yaml")
	writePolicy(t, child, "extends: alias.yaml\n")
	if err := os.Symlink(child, filepath.Join(dir, "alias.yaml")); err != nil {
		t.Skip(err)
	}
	_, err := Load(child)
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("got %v", err)
	}
}

func TestLoadExtendsRejectsURL(t *testing.T) {
	child := filepath.Join(t.TempDir(), "child.yaml")
	writePolicy(t, child, "extends: https://example.com/base.yaml\n")
	_, err := Load(child)
	if err == nil || !strings.Contains(err.Error(), "local file path") || !strings.Contains(err.Error(), child) {
		t.Fatalf("got %v", err)
	}
}
